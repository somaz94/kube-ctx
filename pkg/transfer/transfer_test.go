package transfer

import (
	"strings"
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// stanza describes one context and the cluster and user it points at, spelled
// out far enough that a test can make two of them collide by name while
// differing in contents — which is the whole subject of this package.
type stanza struct {
	context   string
	cluster   string
	user      string
	server    string
	token     string
	namespace string
}

// build assembles a kubeconfig from stanzas. origin stands in for the file
// clientcmd read the config from, which every stanza is stamped with.
func build(origin, current string, stanzas ...stanza) *clientcmdapi.Config {
	cfg := clientcmdapi.NewConfig()
	cfg.CurrentContext = current
	for _, s := range stanzas {
		if s.cluster != "" {
			cfg.Clusters[s.cluster] = &clientcmdapi.Cluster{Server: s.server, LocationOfOrigin: origin}
		}
		if s.user != "" {
			cfg.AuthInfos[s.user] = &clientcmdapi.AuthInfo{Token: s.token, LocationOfOrigin: origin}
		}
		cfg.Contexts[s.context] = &clientcmdapi.Context{
			Cluster: s.cluster, AuthInfo: s.user, Namespace: s.namespace, LocationOfOrigin: origin,
		}
	}
	return cfg
}

// mine is the destination in most tests: one context on its own cluster.
func mine() *clientcmdapi.Config {
	return build("/home/u/.kube/config", "dev", stanza{
		context: "dev", cluster: "kubernetes", user: "kubernetes-admin",
		server: "https://dev.example.com:6443", token: "dev",
	})
}

// theirs is a kubeadm-shaped source: the same stanza names as mine, pointing at
// a different cluster.
func theirs() *clientcmdapi.Config {
	return build("/tmp/downloaded", "kubernetes-admin@kubernetes", stanza{
		context: "kubernetes-admin@kubernetes", cluster: "kubernetes", user: "kubernetes-admin",
		server: "https://prod.example.com:6443", token: "prod", namespace: "apps",
	})
}

func TestMergeAdds(t *testing.T) {
	dst, src := mine(), theirs()

	result, err := Merge(dst, src, Options{})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("entries = %+v, want 1", result.Entries)
	}
	e := result.Entries[0]
	if e.Action != ActionAdded || e.Name != "kubernetes-admin@kubernetes" {
		t.Errorf("entry = %+v", e)
	}
	if e.Renamed() {
		t.Errorf("the context kept its name, so it is not renamed: %+v", e)
	}
	if !result.Changed() {
		t.Error("an added context is a change")
	}

	imported, ok := dst.Contexts["kubernetes-admin@kubernetes"]
	if !ok {
		t.Fatalf("contexts = %v", dst.Contexts)
	}
	if imported.Namespace != "apps" {
		t.Errorf("namespace = %q, want apps", imported.Namespace)
	}
	// The import must not be written back into the file it came from, and
	// clientcmd routes a write by exactly this field.
	if imported.LocationOfOrigin != "" {
		t.Errorf("LocationOfOrigin = %q, want empty", imported.LocationOfOrigin)
	}
	if got := dst.Clusters[imported.Cluster].LocationOfOrigin; got != "" {
		t.Errorf("cluster LocationOfOrigin = %q, want empty", got)
	}
	// current-context belongs to the destination; importing is not switching.
	if dst.CurrentContext != "dev" {
		t.Errorf("current context = %q, want dev", dst.CurrentContext)
	}
}

func TestMergeDisambiguatesCollidingStanza(t *testing.T) {
	dst := mine()

	if _, err := Merge(dst, theirs(), Options{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	imported := dst.Contexts["kubernetes-admin@kubernetes"]
	if imported.Cluster != "kubernetes-2" || imported.AuthInfo != "kubernetes-admin-2" {
		t.Errorf("imported context points at %s/%s, want the suffixed stanzas",
			imported.Cluster, imported.AuthInfo)
	}
	// The point of the suffix: the context that already used the name still
	// reaches the cluster it always did.
	if got := dst.Clusters["kubernetes"].Server; got != "https://dev.example.com:6443" {
		t.Errorf("the existing cluster was replaced: server = %q", got)
	}
	if got := dst.AuthInfos["kubernetes-admin"].Token; got != "dev" {
		t.Errorf("the existing user was replaced: token = %q", got)
	}
}

func TestMergeReusesAnAlreadyAdoptedStanza(t *testing.T) {
	dst := mine()
	src := build("/tmp/downloaded", "a", stanza{
		context: "a", cluster: "kubernetes", user: "kubernetes-admin",
		server: "https://prod.example.com:6443", token: "prod",
	})
	// Both source contexts share one cluster and user, the usual shape of a
	// kubeconfig holding several namespaces of the same cluster.
	src.Contexts["b"] = &clientcmdapi.Context{Cluster: "kubernetes", AuthInfo: "kubernetes-admin", Namespace: "other"}

	if _, err := Merge(dst, src, Options{}); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	if a, b := dst.Contexts["a"].Cluster, dst.Contexts["b"].Cluster; a != b {
		t.Errorf("contexts sharing a cluster were given separate copies: %q and %q", a, b)
	}
	if len(dst.Clusters) != 2 {
		t.Errorf("clusters = %v, want the original plus exactly one import", dst.Clusters)
	}
	if len(dst.AuthInfos) != 2 {
		t.Errorf("users = %v, want the original plus exactly one import", dst.AuthInfos)
	}
}

func TestMergeIsIdempotent(t *testing.T) {
	dst := mine()
	if _, err := Merge(dst, theirs(), Options{}); err != nil {
		t.Fatalf("first Merge: %v", err)
	}
	before := len(dst.Clusters)

	result, err := Merge(dst, theirs(), Options{})
	if err != nil {
		t.Fatalf("second Merge: %v", err)
	}
	if result.Entries[0].Action != ActionUnchanged {
		t.Errorf("action = %q, want unchanged", result.Entries[0].Action)
	}
	if result.Changed() {
		t.Error("re-importing the same file changes nothing")
	}
	if len(dst.Clusters) != before {
		t.Errorf("clusters = %v, want no new copies", dst.Clusters)
	}
}

func TestMergeRefusesACollision(t *testing.T) {
	dst := mine()
	src := build("/tmp/downloaded", "dev", stanza{
		context: "dev", cluster: "other", user: "other-admin",
		server: "https://prod.example.com:6443", token: "prod",
	})

	_, err := Merge(dst, src, Options{})
	if err == nil {
		t.Fatal("a context name already in use must not be replaced silently")
	}
	if !strings.Contains(err.Error(), "--overwrite") {
		t.Errorf("the error should name the way out: %v", err)
	}
	// Nothing may have landed: a half-applied import is the worst outcome.
	if len(dst.Contexts) != 1 || len(dst.Clusters) != 1 {
		t.Errorf("the destination was modified: contexts = %v, clusters = %v", dst.Contexts, dst.Clusters)
	}
	if got := dst.Clusters["kubernetes"].Server; got != "https://dev.example.com:6443" {
		t.Errorf("server = %q, want the original", got)
	}
}

func TestMergeOverwrite(t *testing.T) {
	dst := mine()
	src := build("/tmp/downloaded", "dev", stanza{
		context: "dev", cluster: "other", user: "other-admin",
		server: "https://prod.example.com:6443", token: "prod",
	})

	result, err := Merge(dst, src, Options{Overwrite: true})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if result.Entries[0].Action != ActionOverwritten {
		t.Errorf("action = %q, want overwritten", result.Entries[0].Action)
	}
	if got := dst.Clusters[dst.Contexts["dev"].Cluster].Server; got != "https://prod.example.com:6443" {
		t.Errorf("dev still points at %q", got)
	}
}

func TestMergePrefix(t *testing.T) {
	dst := mine()

	result, err := Merge(dst, theirs(), Options{Prefix: "acme-"})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	e := result.Entries[0]
	if e.Name != "acme-kubernetes-admin@kubernetes" || e.Source != "kubernetes-admin@kubernetes" {
		t.Fatalf("entry = %+v", e)
	}
	if !e.Renamed() {
		t.Error("a prefixed context is renamed")
	}
	if _, ok := dst.Contexts[e.Name]; !ok {
		t.Errorf("contexts = %v", dst.Contexts)
	}
}

func TestMergeRenameNeedsExactlyOneContext(t *testing.T) {
	dst := mine()
	src := theirs()
	src.Contexts["extra"] = &clientcmdapi.Context{Cluster: "kubernetes", AuthInfo: "kubernetes-admin"}

	_, err := Merge(dst, src, Options{Rename: "prod"})
	if err == nil {
		t.Fatal("--as cannot name two contexts at once")
	}
	if !strings.Contains(err.Error(), "--context") {
		t.Errorf("the error should point at the way to narrow it: %v", err)
	}

	result, err := Merge(dst, src, Options{Rename: "prod", Contexts: []string{"extra"}})
	if err != nil {
		t.Fatalf("Merge with one context selected: %v", err)
	}
	if result.Entries[0].Name != "prod" {
		t.Errorf("entry = %+v, want the renamed context", result.Entries[0])
	}
}

func TestMergeSelectsContextsInOrder(t *testing.T) {
	dst := mine()
	src := build("/tmp/downloaded", "a",
		stanza{context: "a", cluster: "ca", user: "ua", server: "https://a.example.com", token: "ta"},
		stanza{context: "b", cluster: "cb", user: "ub", server: "https://b.example.com", token: "tb"},
		stanza{context: "c", cluster: "cc", user: "uc", server: "https://c.example.com", token: "tc"},
	)

	// Repeated names collapse; the order the user typed is kept.
	result, err := Merge(dst, src, Options{Contexts: []string{"c", "a", "c"}})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(result.Entries) != 2 || result.Entries[0].Name != "c" || result.Entries[1].Name != "a" {
		t.Fatalf("entries = %+v", result.Entries)
	}
	if _, ok := dst.Contexts["b"]; ok {
		t.Error("an unselected context was imported")
	}

	if _, err := Merge(dst, src, Options{Contexts: []string{"nope"}}); err == nil {
		t.Error("selecting a context the source does not have must fail")
	}
}

func TestMergeSourceWithoutContexts(t *testing.T) {
	if _, err := Merge(mine(), clientcmdapi.NewConfig(), Options{}); err == nil {
		t.Fatal("an empty source is an error, not a silent success")
	}
}

func TestMergeSourceWithDanglingReferences(t *testing.T) {
	src := theirs()
	delete(src.Clusters, "kubernetes")
	if _, err := Merge(mine(), src, Options{}); err == nil {
		t.Error("a context referencing an undefined cluster must fail")
	}

	src = theirs()
	delete(src.AuthInfos, "kubernetes-admin")
	if _, err := Merge(mine(), src, Options{}); err == nil {
		t.Error("a context referencing an undefined user must fail")
	}
}

func TestMergeAnonymousContext(t *testing.T) {
	dst := mine()
	src := build("/tmp/downloaded", "anon", stanza{
		context: "anon", cluster: "anon-cluster", server: "https://anon.example.com",
	})

	result, err := Merge(dst, src, Options{})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if result.Entries[0].User != "" {
		t.Errorf("user = %q, want empty for an anonymous context", result.Entries[0].User)
	}
	if dst.Contexts["anon"].AuthInfo != "" {
		t.Errorf("AuthInfo = %q, want empty", dst.Contexts["anon"].AuthInfo)
	}
}

func TestMergeIntoAConfigWithNilMaps(t *testing.T) {
	dst := &clientcmdapi.Config{}
	if _, err := Merge(dst, theirs(), Options{}); err != nil {
		t.Fatalf("Merge into a bare Config: %v", err)
	}
	if len(dst.Contexts) != 1 {
		t.Errorf("contexts = %v", dst.Contexts)
	}
}

func TestExtract(t *testing.T) {
	cfg := build("/home/u/.kube/config", "dev",
		stanza{context: "dev", cluster: "dev-cluster", user: "dev-user", server: "https://dev.example.com", token: "dev"},
		stanza{context: "prod", cluster: "prod-cluster", user: "prod-user", server: "https://prod.example.com", token: "prod", namespace: "apps"},
	)

	out, err := Extract(cfg, []string{"prod"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(out.Contexts) != 1 || len(out.Clusters) != 1 || len(out.AuthInfos) != 1 {
		t.Fatalf("the export carries stanzas it does not reference: %+v", out)
	}
	// The original current-context did not survive the extraction, so the export
	// names the one context it holds — a kubeconfig without one is unusable.
	if out.CurrentContext != "prod" {
		t.Errorf("current context = %q, want prod", out.CurrentContext)
	}
	if out.Contexts["prod"].Namespace != "apps" {
		t.Errorf("the namespace was dropped: %+v", out.Contexts["prod"])
	}
	// Kept, unlike on import: it is what resolves a relative certificate path
	// when the export is flattened.
	if out.Clusters["prod-cluster"].LocationOfOrigin == "" {
		t.Error("LocationOfOrigin must survive an extraction")
	}

	out, err = Extract(cfg, []string{"prod", "dev"})
	if err != nil {
		t.Fatalf("Extract two: %v", err)
	}
	if out.CurrentContext != "dev" {
		t.Errorf("current context = %q, want the original when it was exported too", out.CurrentContext)
	}
}

func TestExtractRejectsBadInput(t *testing.T) {
	cfg := mine()

	if _, err := Extract(cfg, nil); err == nil {
		t.Error("extracting nothing is an error")
	}
	if _, err := Extract(cfg, []string{"nope"}); err == nil {
		t.Error("extracting an unknown context is an error")
	}

	broken := mine()
	delete(broken.Clusters, "kubernetes")
	if _, err := Extract(broken, []string{"dev"}); err == nil {
		t.Error("a dangling cluster reference must fail rather than export a stub")
	}

	broken = mine()
	delete(broken.AuthInfos, "kubernetes-admin")
	if _, err := Extract(broken, []string{"dev"}); err == nil {
		t.Error("a dangling user reference must fail")
	}
}

func TestExtractAnonymousContext(t *testing.T) {
	cfg := build("/home/u/.kube/config", "anon", stanza{
		context: "anon", cluster: "anon-cluster", server: "https://anon.example.com",
	})

	out, err := Extract(cfg, []string{"anon"})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(out.AuthInfos) != 0 {
		t.Errorf("users = %v, want none", out.AuthInfos)
	}
}

func TestFreeNameSkipsWhatIsTaken(t *testing.T) {
	taken := map[string]bool{"c-2": true, "c-3": true}
	if got := freeName("c", func(name string) bool { return taken[name] }); got != "c-4" {
		t.Errorf("freeName = %q, want c-4", got)
	}
}
