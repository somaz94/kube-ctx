package expiry

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
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

// A missing CRD is not a gap in the answer — the secrets already carry every
// notAfter — so it must not be reported as one.
func TestMetaDetectsAMissingCRD(t *testing.T) {
	if !meta(errNotRegistered{}) {
		t.Error("a missing CRD was not recognised")
	}
	if meta(nil) {
		t.Error("nil was treated as a missing CRD")
	}
}

type errNotRegistered struct{}

func (errNotRegistered) Error() string {
	return "the server could not find the requested resource"
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
