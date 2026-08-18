package expiry

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// certPEM makes a self-signed certificate expiring at the given time, so the
// tests exercise the real x509 path rather than a stubbed notAfter.
func certPEM(t *testing.T, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "example.test"},
		NotBefore:    notAfter.Add(-24 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func testConfig(names ...string) *clientcmdapi.Config {
	cfg := clientcmdapi.NewConfig()
	for _, name := range names {
		cfg.Contexts[name] = &clientcmdapi.Context{Cluster: "c", AuthInfo: "u"}
	}
	return cfg
}

func TestCertNotAfterReadsTheLeaf(t *testing.T) {
	want := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	got, err := certNotAfter(certPEM(t, want))
	if err != nil {
		t.Fatalf("certNotAfter: %v", err)
	}
	if !got.Equal(want) {
		t.Errorf("notAfter = %v, want %v", got, want)
	}
}

// A tls.crt commonly carries intermediates after the leaf, and those outlive
// it. Reading the last block — or the maximum — would report a certificate as
// healthy for years after it stopped working.
func TestCertNotAfterIgnoresTheIntermediate(t *testing.T) {
	leaf := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	ca := time.Now().Add(10 * 365 * 24 * time.Hour)

	chain := append(certPEM(t, leaf), certPEM(t, ca)...)
	got, err := certNotAfter(chain)
	if err != nil {
		t.Fatalf("certNotAfter: %v", err)
	}
	if !got.Equal(leaf) {
		t.Errorf("notAfter = %v, want the leaf's %v", got, leaf)
	}
}

func TestCertNotAfterRejectsGarbage(t *testing.T) {
	if _, err := certNotAfter([]byte("not a certificate")); err == nil {
		t.Error("garbage was accepted as PEM")
	}
}

func TestSweepRunsEveryContext(t *testing.T) {
	now := time.Now()
	s := &Sweeper{
		RestConfig: func(string) (*rest.Config, error) { return &rest.Config{}, nil },
		Fetch: func(context.Context, *rest.Config) ([]Item, []string, error) {
			return []Item{{Namespace: "ns", Kind: KindTLSSecret, Name: "tls", NotAfter: now.Add(48 * time.Hour)}}, nil, nil
		},
	}

	results := s.Run(context.Background(), testConfig("b", "a"), nil)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	// Sorted, so the report is stable across runs of a map-ordered sweep.
	if results[0].Context != "a" || results[1].Context != "b" {
		t.Errorf("results are unsorted: %q, %q", results[0].Context, results[1].Context)
	}
	// The context is stamped on every item, since the fetcher does not know it.
	for _, r := range results {
		if r.Items[0].Context != r.Context {
			t.Errorf("item context = %q, want %q", r.Items[0].Context, r.Context)
		}
	}
}

func TestSweepReportsAFailedContextWithoutLosingTheRest(t *testing.T) {
	s := &Sweeper{
		RestConfig: func(name string) (*rest.Config, error) {
			if name == "broken" {
				return nil, errors.New("no credential")
			}
			return &rest.Config{}, nil
		},
		Fetch: func(context.Context, *rest.Config) ([]Item, []string, error) { return nil, nil, nil },
	}

	results := s.Run(context.Background(), testConfig("broken", "fine"), nil)
	if results[0].Err == "" {
		t.Error("the broken context reported no error")
	}
	if results[1].Err != "" {
		t.Errorf("the healthy context inherited an error: %q", results[1].Err)
	}
}

func TestWithinKeepsOnlyTheWindowSoonestFirst(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	results := []Result{{
		Context: "a",
		Items: []Item{
			{Name: "far", NotAfter: now.AddDate(0, 0, 90)},
			{Name: "soon", NotAfter: now.AddDate(0, 0, 3)},
			{Name: "mid", NotAfter: now.AddDate(0, 0, 20)},
		},
	}}

	got := Within(results, now, 30)
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1", len(got))
	}
	var names []string
	for _, item := range got[0].Items {
		names = append(names, item.Name)
	}
	if fmt.Sprint(names) != "[soon mid]" {
		t.Errorf("items = %v, want [soon mid]", names)
	}
}

// An already-expired certificate is the most urgent row on the page. Filtering
// it out as "in the past" would make the report go quiet exactly when the
// outage starts.
func TestWithinKeepsAlreadyExpired(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	results := []Result{{Context: "a", Items: []Item{{Name: "dead", NotAfter: now.AddDate(0, 0, -5)}}}}

	got := Within(results, now, 30)
	if len(got) != 1 || len(got[0].Items) != 1 {
		t.Fatalf("an expired certificate was dropped: %+v", got)
	}
	if !got[0].Items[0].Expired(now) {
		t.Error("Expired() did not report the past as expired")
	}
}

// A context with nothing to say is dropped, but one that could not be fully
// read is kept — "clean" and "could not look" must not render identically.
func TestWithinKeepsUnreadableContexts(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	results := []Result{
		{Context: "clean", Items: []Item{{Name: "far", NotAfter: now.AddDate(1, 0, 0)}}},
		{Context: "partial", Skipped: []string{"secrets"}},
		{Context: "broken", Err: "unreachable"},
	}

	got := Within(results, now, 30)
	if len(got) != 2 {
		t.Fatalf("got %d results, want partial and broken kept: %+v", len(got), got)
	}
	// Input order is preserved; sorting is Run's job, done once.
	if got[0].Context != "partial" || got[1].Context != "broken" {
		t.Errorf("kept %q and %q", got[0].Context, got[1].Context)
	}
}

func TestExpiringDrivesTheExitStatus(t *testing.T) {
	if Expiring([]Result{{Context: "a", Err: "unreachable"}}) {
		t.Error("an unreachable context alone must not report something expiring")
	}
	if !Expiring([]Result{{Context: "a", Items: []Item{{Name: "x"}}}}) {
		t.Error("an expiring certificate did not register")
	}
}

func TestManagedDistinguishesWhoHasToAct(t *testing.T) {
	if (Item{}).Managed() {
		t.Error("an unmanaged secret reported itself as managed")
	}
	if !(Item{Issuer: "letsencrypt"}).Managed() {
		t.Error("a cert-manager Certificate reported itself as unmanaged")
	}
}

// A sweep that could not read a cluster has not established that nothing is
// wrong there. Answering 0 turns "kctx expiry || notify" silent for the one
// case — every cluster unreachable — where it most needs to fire.
func TestUnknownCatchesAFailedContext(t *testing.T) {
	failed := []Result{{Context: "prod", Err: "connection refused"}}

	if Expiring(failed) {
		t.Error("a failed context reported something expiring")
	}
	if !Unknown(failed) {
		t.Fatal("a failed context did not register as unknown; the command would exit 0")
	}
	if Unknown([]Result{{Context: "prod", Items: []Item{{Name: "x"}}}}) {
		t.Error("a healthy context registered as unknown")
	}
}

// &Sweeper{} builds, and the panic would come out of a goroutine with nothing
// to catch it.
func TestSweepWithoutARestConfigFunctionDoesNotPanic(t *testing.T) {
	s := &Sweeper{}
	results := s.Run(context.Background(), testConfig("a"), nil)
	if len(results) != 1 || results[0].Err == "" {
		t.Fatalf("results = %+v, want a reported error", results)
	}
}

// A tls.crt can lead with something that is not the leaf. Treating the first
// block as the certificate drops an otherwise readable secret in silence.
func TestCertNotAfterSkipsANonCertificateBlock(t *testing.T) {
	leaf := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	preamble := []byte("-----BEGIN TRUSTED CERTIFICATE-----\nZ2FyYmFnZQ==\n-----END TRUSTED CERTIFICATE-----\n")

	got, err := certNotAfter(append(preamble, certPEM(t, leaf)...))
	if err != nil {
		t.Fatalf("certNotAfter: %v", err)
	}
	if !got.Equal(leaf) {
		t.Errorf("notAfter = %v, want the leaf's %v", got, leaf)
	}
}

func TestCertNotAfterRejectsPEMWithNoCertificate(t *testing.T) {
	key := []byte("-----BEGIN EC PRIVATE KEY-----\nZ2FyYmFnZQ==\n-----END EC PRIVATE KEY-----\n")
	if _, err := certNotAfter(key); err == nil {
		t.Error("a PEM carrying no certificate was accepted")
	}
}
