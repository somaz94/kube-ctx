package expiry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
)

func tlsSecret(t *testing.T, ns, name string, notAfter time.Time) *corev1.Secret {
	t.Helper()
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{corev1.TLSCertKey: certPEM(t, notAfter)},
	}
}

func TestTLSSecretsReadsEveryCertificate(t *testing.T) {
	soon := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	far := time.Now().Add(365 * 24 * time.Hour).Truncate(time.Second)

	client := fake.NewSimpleClientset(
		tlsSecret(t, "istio", "gw-tls", soon),
		tlsSecret(t, "default", "api-tls", far),
	)

	items, err := tlsSecrets(context.Background(), client)
	if err != nil {
		t.Fatalf("tlsSecrets: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	for _, item := range items {
		if item.Kind != KindTLSSecret {
			t.Errorf("%s: kind = %q", item.Name, item.Kind)
		}
		if item.NotAfter.IsZero() {
			t.Errorf("%s: notAfter was not read", item.Name)
		}
	}
}

// One unreadable secret is not a reason to lose the other ninety.
func TestTLSSecretsSkipsWhatItCannotParse(t *testing.T) {
	good := tlsSecret(t, "ns", "good", time.Now().Add(48*time.Hour))
	garbage := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "garbage"},
		Type:       corev1.SecretTypeTLS,
		Data:       map[string][]byte{corev1.TLSCertKey: []byte("not a certificate")},
	}
	empty := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "empty"},
		Type:       corev1.SecretTypeTLS,
	}

	items, err := tlsSecrets(context.Background(), fake.NewSimpleClientset(good, garbage, empty))
	if err != nil {
		t.Fatalf("tlsSecrets: %v", err)
	}
	if len(items) != 1 || items[0].Name != "good" {
		t.Fatalf("items = %+v, want only the readable one", items)
	}
}

// The Certificate's name is what an operator goes looking for, and it is often
// not the secret's — reporting the secret sends them to a resource they cannot
// edit.
func TestMergeRenamesToTheManagingCertificate(t *testing.T) {
	renewal := time.Now().Add(24 * time.Hour)
	items := []Item{
		{Namespace: "istio", Name: "gw-tls-secret", Kind: KindTLSSecret},
		{Namespace: "default", Name: "hand-rolled", Kind: KindTLSSecret},
	}
	managed := map[string]managedCert{
		"istio/gw-tls-secret": {name: "gateway-cert", issuer: "letsencrypt", renewalTime: &renewal},
	}

	got := merge(items, managed)

	if got[0].Kind != KindCertificate || got[0].Name != "gateway-cert" {
		t.Errorf("managed item = %+v, want the Certificate's identity", got[0])
	}
	if got[0].Issuer != "letsencrypt" || got[0].RenewalTime == nil {
		t.Errorf("managed item lost its renewal information: %+v", got[0])
	}
	if !got[0].Managed() {
		t.Error("a cert-manager Certificate did not report itself as managed")
	}

	// The unmanaged one keeps its own identity and stays somebody's problem.
	if got[1].Kind != KindTLSSecret || got[1].Name != "hand-rolled" || got[1].Managed() {
		t.Errorf("unmanaged item = %+v, want it untouched", got[1])
	}
}

func certificateObject(ns, name, secretName, issuer, renewal string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1",
		"kind":       "Certificate",
		"metadata":   map[string]any{"namespace": ns, "name": name},
		"spec": map[string]any{
			"secretName": secretName,
			"issuerRef":  map[string]any{"name": issuer},
		},
	}}
	if renewal != "" {
		_ = unstructured.SetNestedField(obj.Object, renewal, "status", "renewalTime")
	}
	return obj
}

func dynamicClient(objs ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{certManagerCertificates: "CertificateList"}, objs...)
}

func TestCertManagerOverlayKeysBySecret(t *testing.T) {
	client := dynamicClient(
		certificateObject("istio", "gateway-cert", "gw-tls", "letsencrypt", "2026-06-10T00:00:00Z"),
		// No secretName is nothing this report can attach to.
		certificateObject("ns", "orphan", "", "selfsigned", ""),
	)

	got, err := certManagerOverlay(context.Background(), client)
	if err != nil {
		t.Fatalf("certManagerOverlay: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}

	entry, ok := got["istio/gw-tls"]
	if !ok {
		t.Fatalf("keyed on the wrong thing: %+v", got)
	}
	if entry.name != "gateway-cert" || entry.issuer != "letsencrypt" {
		t.Errorf("entry = %+v", entry)
	}
	if entry.renewalTime == nil || entry.renewalTime.Day() != 10 {
		t.Errorf("renewalTime = %v", entry.renewalTime)
	}
}

// An unparseable renewalTime is not a reason to lose the rest of the entry.
func TestCertManagerOverlayToleratesABadRenewalTime(t *testing.T) {
	client := dynamicClient(certificateObject("ns", "cert", "secret", "issuer", "not-a-timestamp"))

	got, err := certManagerOverlay(context.Background(), client)
	if err != nil {
		t.Fatalf("certManagerOverlay: %v", err)
	}
	entry := got["ns/secret"]
	if entry.name != "cert" || entry.issuer != "issuer" {
		t.Errorf("entry = %+v, want it kept", entry)
	}
	if entry.renewalTime != nil {
		t.Errorf("renewalTime = %v, want nil", entry.renewalTime)
	}
}

func TestItemInReportsTimeLeft(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	item := Item{NotAfter: now.Add(48 * time.Hour)}
	if got := item.In(now); got != 48*time.Hour {
		t.Errorf("In = %v, want 48h", got)
	}
}

// Live's own wiring: an unreachable cluster is an error, not an empty answer
// that would render as "nothing expires here".
func TestLiveReportsAnUnreachableCluster(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Port 1 is reserved and never listening, the same address the e2e suite
	// uses for its deliberately offline context.
	items, skipped, err := Live(ctx, &rest.Config{Host: "https://127.0.0.1:1"})
	if err == nil {
		t.Fatalf("an unreachable cluster answered cleanly: items=%v skipped=%v", items, skipped)
	}
	if items != nil {
		t.Errorf("items = %v, want nil alongside the error", items)
	}
}

// 401 is not a partial answer. Forbidden means authenticated and scoped, so
// what was read is true; unauthorized means nothing was checked, and calling
// that a gap is how the report goes quiet when it stops working.
func TestLiveTreatsUnauthorizedAsAFailureNotAGap(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("list", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewUnauthorized("token expired")
	})

	_, err := tlsSecrets(context.Background(), client)
	if !apierrors.IsUnauthorized(err) {
		t.Fatalf("err = %v, want an unauthorized error to reach the caller", err)
	}
	// The classification Live makes on it: forbidden is recorded as a blind
	// skip, 401 is not tolerated at all.
	if apierrors.IsForbidden(err) {
		t.Error("unauthorized was classified as forbidden")
	}
}

// A cert-manager problem costs the "who renews this" column and nothing else.
// Throwing the error would discard every notAfter already read, so a
// certificate expiring tomorrow would vanish because cert-manager was sick.
func TestLiveKeepsSecretsWhenTheOverlayFails(t *testing.T) {
	soon := time.Now().Add(48 * time.Hour)
	items := []Item{{Namespace: "ns", Name: "tls", Kind: KindTLSSecret, NotAfter: soon}}

	// merge is the join; with no overlay it must leave the secrets intact.
	got := merge(items, nil)
	if len(got) != 1 || got[0].Kind != KindTLSSecret || got[0].Managed() {
		t.Fatalf("items = %+v, want the unmanaged secret kept as-is", got)
	}
}

// The gate is Live's Forbidden branch, not a constant. Asserting through
// Unknown rather than on the record's contents is the whole point: a change
// that carries the reason along, or renames the resource, must not be able to
// take the exit status with it in silence.
func TestARefusedSecretsListReachesTheExitStatus(t *testing.T) {
	forbidden := apierrors.NewForbidden(
		schema.GroupResource{Resource: "secrets"}, "", errors.New("no"))

	skip, blocked, err := classifySecrets(forbidden)
	if err != nil || !blocked {
		t.Fatalf("classifySecrets = %+v, %v, %v; want a recorded skip", skip, blocked, err)
	}
	if !Unknown([]Result{{Context: "a", Skipped: []Skip{skip}}}) {
		t.Fatal("what Live actually produces did not register as unknown; the command would exit 0")
	}
	// The reason is carried for the operator and must stay out of the decision.
	if skip.Reason == "" {
		t.Error("the skip carries no reason")
	}
}

// 401 is not a skip at all: nothing authenticated, so the sweep has no
// business reporting a context it never reached.
func TestClassifySecretsFailsOnUnauthorized(t *testing.T) {
	_, blocked, err := classifySecrets(apierrors.NewUnauthorized("token expired"))
	if err == nil {
		t.Fatal("unauthorized was tolerated")
	}
	if blocked {
		t.Error("unauthorized was recorded as a skip rather than a failure")
	}
}

func TestClassifySecretsPassesACleanRead(t *testing.T) {
	skip, blocked, err := classifySecrets(nil)
	if err != nil || blocked || skip.Blind {
		t.Fatalf("classifySecrets(nil) = %+v, %v, %v", skip, blocked, err)
	}
}

// A cert-manager failure is recorded but must never be Blind: every notAfter
// was already read from the secrets before the overlay ran. Asserting through
// Unknown on what Live actually produces, rather than on a hand-built literal,
// is the point — a Blind added to the branch must fail here.
func TestOverlayFailureIsNotBlind(t *testing.T) {
	skip, blocked := classifyOverlay(apierrors.NewServiceUnavailable("503"))
	if !blocked {
		t.Fatal("an overlay failure was not recorded at all")
	}
	if skip.Blind {
		t.Fatal("an overlay failure was marked blind")
	}
	if Unknown([]Result{{Context: "a", Items: []Item{{Name: "x"}}, Skipped: []Skip{skip}}}) {
		t.Error("an overlay failure took the exit status with it")
	}
	// The reason is carried for the operator: a timeout reported bare reads as
	// a permission problem.
	if !strings.Contains(skip.String(), "certificates.cert-manager.io") || skip.Reason == "" {
		t.Errorf("String() = %q", skip.String())
	}
}

// cert-manager not being installed is not a gap in the answer — the secrets
// already carry every notAfter — so it must not be reported as one.
func TestOverlayRecordsNothingWhenCertManagerIsAbsent(t *testing.T) {
	absent := []error{
		nil,
		apierrors.NewNotFound(certManagerCertificates.GroupResource(), ""),
		&apimeta.NoKindMatchError{GroupKind: schema.GroupKind{Group: "cert-manager.io", Kind: "Certificate"}},
	}
	for _, err := range absent {
		if skip, blocked := classifyOverlay(err); blocked {
			t.Errorf("classifyOverlay(%v) recorded %+v, want nothing", err, skip)
		}
	}
}
