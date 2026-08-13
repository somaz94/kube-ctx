package probe

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/internal/testutil"
)

// stubProber returns a Prober whose live call is scripted.
func stubProber(version string, err error) *Prober {
	return &Prober{
		RestConfig: func(string) (*rest.Config, error) { return &rest.Config{Host: "https://example"}, nil },
		Version: func(context.Context, *rest.Config) (string, error) {
			return version, err
		},
	}
}

func TestRunProbesEveryContextSorted(t *testing.T) {
	cfg := testutil.Config(testutil.Spec{
		Current: "dev",
		Contexts: []testutil.Ctx{
			{Name: "staging"}, {Name: "dev"}, {Name: "prod"},
		},
	})

	got := stubProber("v1.31.4", nil).Run(context.Background(), cfg, nil)

	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}
	if got[0].Context != "dev" || got[1].Context != "prod" || got[2].Context != "staging" {
		t.Errorf("results not sorted: %v", []string{got[0].Context, got[1].Context, got[2].Context})
	}
	for _, r := range got {
		if !r.Reachable || r.ServerVersion != "v1.31.4" {
			t.Errorf("%s: reachable=%v version=%q", r.Context, r.Reachable, r.ServerVersion)
		}
		if !r.Healthy() {
			t.Errorf("%s should be healthy: %+v", r.Context, r)
		}
	}
}

func TestRunSelectedContextsOnly(t *testing.T) {
	cfg := testutil.Config(testutil.Spec{
		Contexts: []testutil.Ctx{{Name: "dev"}, {Name: "prod"}},
	})

	got := stubProber("v1.31.4", nil).Run(context.Background(), cfg, []string{"prod"})
	if len(got) != 1 || got[0].Context != "prod" {
		t.Fatalf("got %+v, want just prod", got)
	}
}

func TestRunUnreachableCluster(t *testing.T) {
	cfg := testutil.Config(testutil.Spec{Contexts: []testutil.Ctx{{Name: "dev"}}})

	got := stubProber("", errors.New("dial tcp 10.0.0.1:6443: i/o timeout\nsecond line"))
	results := got.Run(context.Background(), cfg, nil)

	if results[0].Reachable {
		t.Error("Reachable = true for a failed call")
	}
	if strings.Contains(results[0].Err, "\n") {
		t.Errorf("Err should be one line: %q", results[0].Err)
	}
	if results[0].Healthy() {
		t.Error("an unreachable context is not healthy")
	}
}

func TestRunRestConfigFailure(t *testing.T) {
	cfg := testutil.Config(testutil.Spec{Contexts: []testutil.Ctx{{Name: "dev"}}})
	p := &Prober{
		RestConfig: func(string) (*rest.Config, error) { return nil, errors.New("bad context") },
	}

	got := p.Run(context.Background(), cfg, nil)
	if got[0].Err != "bad context" {
		t.Errorf("Err = %q", got[0].Err)
	}
}

func TestRunDanglingReferences(t *testing.T) {
	cfg := testutil.Config(testutil.Spec{Contexts: []testutil.Ctx{{Name: "dev"}}})
	delete(cfg.Clusters, "dev-cluster")
	delete(cfg.AuthInfos, "dev-user")

	got := stubProber("v1.31.4", nil).Run(context.Background(), cfg, nil)[0]

	if len(got.Issues) != 2 {
		t.Fatalf("issues = %v, want one per dangling reference", got.Issues)
	}
	if got.Reachable {
		t.Error("a context with no server must not be contacted")
	}
	if got.Auth != AuthNone {
		t.Errorf("Auth = %q, want none", got.Auth)
	}
}

func TestRunUnknownContext(t *testing.T) {
	cfg := testutil.Config(testutil.Spec{Contexts: []testutil.Ctx{{Name: "dev"}}})

	got := stubProber("v1.31.4", nil).Run(context.Background(), cfg, []string{"nope"})
	if len(got[0].Issues) == 0 || !strings.Contains(got[0].Issues[0], "not found") {
		t.Errorf("issues = %v", got[0].Issues)
	}
}

func TestRunRespectsConcurrencyLimit(t *testing.T) {
	cfg := testutil.Config(testutil.Spec{
		Contexts: []testutil.Ctx{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}},
	})

	var mu = make(chan struct{}, 1)
	inFlight, peak := 0, 0
	p := &Prober{
		Concurrency: 2,
		RestConfig:  func(string) (*rest.Config, error) { return &rest.Config{Host: "https://example"}, nil },
		Version: func(context.Context, *rest.Config) (string, error) {
			mu <- struct{}{}
			inFlight++
			if inFlight > peak {
				peak = inFlight
			}
			<-mu

			time.Sleep(10 * time.Millisecond)

			mu <- struct{}{}
			inFlight--
			<-mu
			return "v1.31.4", nil
		},
	}

	p.Run(context.Background(), cfg, nil)
	if peak > 2 {
		t.Errorf("peak concurrency = %d, want at most 2", peak)
	}
}

func TestInspectAuthKinds(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("plain-token\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	tests := []struct {
		name     string
		authInfo *clientcmdapi.AuthInfo
		want     AuthKind
		detail   string
	}{
		{"exec plugin", &clientcmdapi.AuthInfo{Exec: &clientcmdapi.ExecConfig{Command: "aws"}}, AuthExec, "aws"},
		{"token", &clientcmdapi.AuthInfo{Token: "abc"}, AuthToken, ""},
		{"token file", &clientcmdapi.AuthInfo{TokenFile: tokenFile}, AuthToken, tokenFile},
		{"basic", &clientcmdapi.AuthInfo{Username: "admin", Password: "x"}, AuthBasic, "admin"},
		{"auth provider", &clientcmdapi.AuthInfo{AuthProvider: &clientcmdapi.AuthProviderConfig{Name: "gcp"}}, AuthExec, "gcp"},
		{"nothing", &clientcmdapi.AuthInfo{}, AuthNone, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, detail, _, err := inspectAuth(tt.authInfo)
			if err != nil {
				t.Fatalf("inspectAuth: %v", err)
			}
			if kind != tt.want {
				t.Errorf("kind = %q, want %q", kind, tt.want)
			}
			if detail != tt.detail {
				t.Errorf("detail = %q, want %q", detail, tt.detail)
			}
		})
	}
}

func TestInspectAuthMissingFiles(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")

	if _, _, _, err := inspectAuth(&clientcmdapi.AuthInfo{ClientCertificate: missing}); err == nil {
		t.Error("expected an error for a missing client certificate")
	}
	if _, _, _, err := inspectAuth(&clientcmdapi.AuthInfo{TokenFile: missing}); err == nil {
		t.Error("expected an error for a missing token file")
	}
}

func TestClientCertExpiry(t *testing.T) {
	notAfter := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	certPEM := makeCert(t, notAfter)

	kind, _, expiry, err := inspectAuth(&clientcmdapi.AuthInfo{ClientCertificateData: certPEM})
	if err != nil {
		t.Fatalf("inspectAuth: %v", err)
	}
	if kind != AuthClientCert {
		t.Errorf("kind = %q, want client-cert", kind)
	}
	if expiry == nil || !expiry.Equal(notAfter.UTC()) {
		t.Errorf("expiry = %v, want %v", expiry, notAfter.UTC())
	}
}

func TestExpiredClientCertBecomesAnIssue(t *testing.T) {
	certPEM := makeCert(t, time.Now().Add(-time.Hour))

	cfg := testutil.Config(testutil.Spec{Contexts: []testutil.Ctx{{Name: "dev"}}})
	cfg.AuthInfos["dev-user"] = &clientcmdapi.AuthInfo{ClientCertificateData: certPEM}

	got := stubProber("v1.31.4", nil).Run(context.Background(), cfg, nil)[0]
	if !got.Expired() {
		t.Fatal("Expired() = false for a certificate that expired an hour ago")
	}
	if len(got.Issues) == 0 || !strings.Contains(got.Issues[0], "expired") {
		t.Errorf("issues = %v", got.Issues)
	}
	if got.Healthy() {
		t.Error("an expired credential is not healthy")
	}
}

func TestMalformedClientCert(t *testing.T) {
	_, _, _, err := inspectAuth(&clientcmdapi.AuthInfo{ClientCertificateData: []byte("not pem")})
	if err == nil {
		t.Error("expected an error for non-PEM certificate data")
	}

	bogusPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")})
	if _, _, _, err := inspectAuth(&clientcmdapi.AuthInfo{ClientCertificateData: bogusPEM}); err == nil {
		t.Error("expected an error for an unparseable certificate")
	}
}

func TestTokenExpiry(t *testing.T) {
	exp := time.Now().Add(time.Hour).Truncate(time.Second)

	tests := []struct {
		name  string
		token string
		want  *time.Time
	}{
		{"jwt with exp", makeJWT(t, exp.Unix()), &exp},
		{"jwt without exp", makeJWT(t, 0), nil},
		{"not a jwt", "opaque-service-account-token", nil},
		{"bad base64", "a.!!!.c", nil},
		{"payload is not json", "a." + base64.RawURLEncoding.EncodeToString([]byte("nope")) + ".c", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenExpiry(tt.token)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("expiry = %v, want nil", got)
			case tt.want != nil && got == nil:
				t.Error("expiry = nil, want a time")
			case tt.want != nil && !got.Equal(*tt.want):
				t.Errorf("expiry = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLiveVersionAgainstFakeAPIServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"major": "1", "minor": "31", "gitVersion": "v1.31.4"})
	}))
	defer srv.Close()

	got, err := LiveVersion(context.Background(), &rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatalf("LiveVersion: %v", err)
	}
	if got != "v1.31.4" {
		t.Errorf("version = %q, want v1.31.4", got)
	}
}

func TestLiveVersionFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := LiveVersion(context.Background(), &rest.Config{Host: srv.URL}); err == nil {
		t.Error("expected an error")
	}

	bad := &rest.Config{
		Host:         srv.URL,
		ExecProvider: &clientcmdapi.ExecConfig{},
		AuthProvider: &clientcmdapi.AuthProviderConfig{},
	}
	if _, err := LiveVersion(context.Background(), bad); err == nil {
		t.Error("expected an error building a client from a conflicting config")
	}
}

func TestTrimError(t *testing.T) {
	long := errors.New(strings.Repeat("x", 300))
	got := trimError(long)
	if n := utf8.RuneCountInString(got); n != 120 {
		t.Errorf("trimmed length = %d runes, want 120", n)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("trimmed error should end with an ellipsis: %q", got)
	}
	if got := trimError(errors.New("first\nsecond")); got != "first" {
		t.Errorf("trimError = %q, want first", got)
	}
}

// makeCert returns a self-signed certificate in PEM form expiring at notAfter.
func makeCert(t *testing.T, notAfter time.Time) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "kube-ctx-test"},
		NotBefore:    notAfter.Add(-48 * time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// makeJWT returns an unsigned JWT carrying the given exp claim; exp of 0 omits
// the claim entirely.
func makeJWT(t *testing.T, exp int64) string {
	t.Helper()
	claims := map[string]any{"sub": "test"}
	if exp != 0 {
		claims["exp"] = exp
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	return fmt.Sprintf("%s.%s.%s",
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`)),
		base64.RawURLEncoding.EncodeToString(payload),
		base64.RawURLEncoding.EncodeToString([]byte("signature")))
}
