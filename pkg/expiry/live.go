package expiry

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// certManagerCertificates is the CRD that says what renews a certificate.
//
// Read through the dynamic client rather than cert-manager's typed API: this
// is two string fields off an unstructured object, and taking the dependency
// would pull cert-manager's whole scheme in to avoid one type assertion.
var certManagerCertificates = schema.GroupVersionResource{
	Group:    "cert-manager.io",
	Version:  "v1",
	Resource: "certificates",
}

// tlsSecretSelector is how the API server is asked for only the TLS secrets.
//
// Filtering server-side matters here: on a busy cluster the secret list is
// mostly Helm release state and service account tokens, and pulling all of it
// back to discard it is the difference between a quick answer and a timeout.
const tlsSecretSelector = "type=" + string(corev1.SecretTypeTLS)

// Live reads the certificates one cluster knows about.
//
// Secrets are the base and cert-manager Certificates are an overlay, keyed by
// namespace and the secret each Certificate writes: every managed certificate
// also exists as a TLS secret, so reading both and merging gives one row per
// certificate that knows both when it expires and what will renew it.
func Live(ctx context.Context, rc *rest.Config) ([]Item, []Skip, error) {
	clientset, err := kubernetes.NewForConfig(rc)
	if err != nil {
		return nil, nil, fmt.Errorf("build client: %w", err)
	}

	var skipped []Skip

	items, err := tlsSecrets(ctx, clientset)
	skip, blocked, err := classifySecrets(err)
	if err != nil {
		return nil, nil, err
	}
	if blocked {
		skipped = append(skipped, skip)
	}

	dynamicClient, err := dynamic.NewForConfig(rc)
	if err != nil {
		return nil, nil, fmt.Errorf("build dynamic client: %w", err)
	}

	managed, err := certManagerOverlay(ctx, dynamicClient)
	switch {
	case apierrors.IsNotFound(err), apimeta.IsNoMatchError(err):
		// cert-manager is simply not installed. Not a gap in the answer — the
		// secrets already carry every notAfter — so it is not reported as one.
	case err != nil:
		// Every other overlay failure — forbidden, a webhook down, the
		// aggregated API returning 503, this context running out of deadline —
		// costs only the "who renews this" column. Returning the error instead
		// would throw away every notAfter already read, so a certificate
		// expiring tomorrow would vanish because cert-manager was unhealthy.
		//
		// The reason travels with it: reported bare, a timeout reads as a
		// permission problem and sends the operator to check RBAC. It is not
		// Blind — every notAfter was already in hand before this ran.
		skipped = append(skipped, Skip{
			Resource: "certificates.cert-manager.io",
			Reason:   err.Error(),
		})
	}

	return merge(items, managed), skipped, nil
}

// classifySecrets turns the secrets list error into either a fatal error or a
// skip record.
//
// Split out of Live because this is the branch the exit status reads, and Live
// itself cannot be driven without an API server — leaving the two together put
// the one decision that matters in the one function no test could reach.
func classifySecrets(err error) (Skip, bool, error) {
	switch {
	case err == nil:
		return Skip{}, false, nil
	case apierrors.IsForbidden(err):
		// Blind, not partial. The list is cluster-wide and issued once, so a
		// refusal reads zero certificates rather than some of them — there is
		// no namespaced fallback to fall back to. Recorded rather than
		// returned only so the row still names the context and says why.
		return Skip{Resource: "secrets", Reason: err.Error(), Blind: true}, true, nil
	default:
		// Unauthorized lands here. Forbidden means authenticated and scoped;
		// 401 means nothing authenticated at all, and the sweep has no
		// business reporting a context it never reached.
		return Skip{}, false, err
	}
}

// tlsSecrets lists every kubernetes.io/tls secret and reads its leaf notAfter.
func tlsSecrets(ctx context.Context, clientset kubernetes.Interface) ([]Item, error) {
	list, err := clientset.CoreV1().Secrets(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: tlsSecretSelector,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(list.Items))
	for _, secret := range list.Items {
		pemData, ok := secret.Data[corev1.TLSCertKey]
		if !ok {
			continue
		}
		notAfter, err := certNotAfter(pemData)
		if err != nil {
			// One unreadable secret is not a reason to lose the other ninety.
			continue
		}
		items = append(items, Item{
			Namespace: secret.Namespace,
			Kind:      KindTLSSecret,
			Name:      secret.Name,
			NotAfter:  notAfter,
		})
	}
	return items, nil
}

// managedCert is what a cert-manager Certificate adds to a secret.
type managedCert struct {
	name        string
	issuer      string
	renewalTime *time.Time
}

// certManagerOverlay maps namespace/secretName to the Certificate managing it.
//
// Takes the client rather than the config, for the same reason tlsSecrets
// does: it is the half worth testing, and building the connection is not.
func certManagerOverlay(ctx context.Context, client dynamic.Interface) (map[string]managedCert, error) {
	list, err := client.Resource(certManagerCertificates).
		Namespace(metav1.NamespaceAll).
		List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	out := make(map[string]managedCert, len(list.Items))
	for _, obj := range list.Items {
		secretName, _, _ := unstructured.NestedString(obj.Object, "spec", "secretName")
		if secretName == "" {
			continue
		}
		issuer, _, _ := unstructured.NestedString(obj.Object, "spec", "issuerRef", "name")
		entry := managedCert{name: obj.GetName(), issuer: issuer}
		if raw, ok, _ := unstructured.NestedString(obj.Object, "status", "renewalTime"); ok {
			if t, err := time.Parse(time.RFC3339, raw); err == nil {
				entry.renewalTime = &t
			}
		}
		out[obj.GetNamespace()+"/"+secretName] = entry
	}
	return out, nil
}

// merge folds the Certificate overlay onto the secrets it manages.
func merge(items []Item, managed map[string]managedCert) []Item {
	for i := range items {
		entry, ok := managed[items[i].Namespace+"/"+items[i].Name]
		if !ok {
			continue
		}
		// The Certificate's name is what the operator will go looking for, and
		// it is often not the secret's — reporting the secret would send them
		// to a resource they cannot edit.
		items[i].Kind = KindCertificate
		items[i].Name = entry.name
		items[i].Issuer = entry.issuer
		items[i].RenewalTime = entry.renewalTime
	}
	return items
}
