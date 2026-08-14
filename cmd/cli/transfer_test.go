package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/internal/testutil"
	"github.com/somaz94/kube-ctx/pkg/transfer"
)

// writeSource writes a second kubeconfig beside the harness's own, standing in
// for the file a colleague or a cloud CLI handed the user.
func writeSource(t *testing.T, h *harness, name string, spec testutil.Spec) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(h.kubeconfig), name)
	testutil.Write(t, path, testutil.Config(spec))
	return path
}

// foreignSpec is a source holding one context whose name is free but whose
// cluster and user names are the ones every kubeadm config uses.
func foreignSpec() testutil.Spec {
	return testutil.Spec{
		Current: "kubernetes-admin@kubernetes",
		Contexts: []testutil.Ctx{{
			Name:      "kubernetes-admin@kubernetes",
			Cluster:   "kubernetes",
			User:      "kubernetes-admin",
			Namespace: "apps",
			Server:    "https://acme.example.com:6443",
		}},
	}
}

func TestImportAddsContexts(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", foreignSpec())

	if err := h.run("import", src); err != nil {
		t.Fatalf("import: %v", err)
	}

	cfg := h.config()
	imported, ok := cfg.Contexts["kubernetes-admin@kubernetes"]
	if !ok {
		t.Fatalf("contexts = %v", cfg.Contexts)
	}
	if imported.Namespace != "apps" {
		t.Errorf("namespace = %q, want apps", imported.Namespace)
	}
	if cfg.CurrentContext != "dev" {
		t.Errorf("current = %q; importing is not switching", cfg.CurrentContext)
	}
	if !strings.Contains(h.stdout(), "added") || !strings.Contains(h.stdout(), "Imported 1 context(s)") {
		t.Errorf("stdout = %q", h.stdout())
	}
}

// clientcmd routes a write by the file each stanza was read from, so an import
// that kept the source's LocationOfOrigin would write the user's new contexts
// straight back into the file they were imported from — leaving their own
// kubeconfig untouched and quietly editing someone else's.
func TestImportWritesIntoTheUsersOwnKubeconfig(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", foreignSpec())

	if err := h.run("import", src); err != nil {
		t.Fatalf("import: %v", err)
	}

	source := testutil.Read(t, src)
	if len(source.Contexts) != 1 {
		t.Errorf("the source file was modified: contexts = %v", source.Contexts)
	}
	if _, ok := h.config().Contexts["kubernetes-admin@kubernetes"]; !ok {
		t.Error("the import did not reach the user's kubeconfig")
	}
}

func TestImportDryRunWritesNothing(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", foreignSpec())

	if err := h.run("import", src, "--dry-run"); err != nil {
		t.Fatalf("import --dry-run: %v", err)
	}
	if _, ok := h.config().Contexts["kubernetes-admin@kubernetes"]; ok {
		t.Error("--dry-run wrote the import")
	}
	if !strings.Contains(h.stdout(), "Would import") {
		t.Errorf("stdout = %q", h.stdout())
	}
}

func TestImportRefusesToReplaceAnExistingContext(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", testutil.Spec{
		Current:  "prod",
		Contexts: []testutil.Ctx{{Name: "prod", Server: "https://acme.example.com:6443"}},
	})

	err := h.run("import", src)
	if err == nil {
		t.Fatal("a context name already in use must not be replaced silently")
	}
	if !strings.Contains(err.Error(), "--overwrite") {
		t.Errorf("error = %v, want it to name the way out", err)
	}
	if got := h.config().Clusters["prod-cluster"].Server; got == "https://acme.example.com:6443" {
		t.Error("the refused import was written anyway")
	}
}

func TestImportOverwrite(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", testutil.Spec{
		Current:  "prod",
		Contexts: []testutil.Ctx{{Name: "prod", Cluster: "acme", User: "acme-user", Server: "https://acme.example.com:6443"}},
	})

	if err := h.run("import", src, "--overwrite"); err != nil {
		t.Fatalf("import --overwrite: %v", err)
	}
	cfg := h.config()
	if got := cfg.Clusters[cfg.Contexts["prod"].Cluster].Server; got != "https://acme.example.com:6443" {
		t.Errorf("prod points at %q", got)
	}
	if !strings.Contains(h.stdout(), "overwritten") {
		t.Errorf("stdout = %q", h.stdout())
	}
}

func TestImportPrefixAndAs(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", testutil.Spec{
		Current: "prod",
		Contexts: []testutil.Ctx{
			{Name: "prod", Server: "https://acme-prod.example.com:6443"},
			{Name: "dev", Server: "https://acme-dev.example.com:6443"},
		},
	})

	if err := h.run("import", src, "--prefix", "acme-"); err != nil {
		t.Fatalf("import --prefix: %v", err)
	}
	cfg := h.config()
	for _, want := range []string{"acme-prod", "acme-dev"} {
		if _, ok := cfg.Contexts[want]; !ok {
			t.Errorf("contexts = %v, want %s", cfg.Contexts, want)
		}
	}

	if err := h.run("import", src, "--context", "prod", "--as", "acme"); err != nil {
		t.Fatalf("import --as: %v", err)
	}
	if _, ok := h.config().Contexts["acme"]; !ok {
		t.Errorf("contexts = %v, want acme", h.config().Contexts)
	}
}

// --as renames one context; with several selected there is no one name to give
// them, and picking the first silently would import the rest under their own.
func TestImportAsRejectsSeveralContexts(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", testutil.Spec{
		Contexts: []testutil.Ctx{{Name: "a"}, {Name: "b"}},
	})

	if err := h.run("import", src, "--as", "one"); err == nil {
		t.Fatal("--as must refuse a multi-context import")
	}
}

func TestImportSelectsContexts(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", testutil.Spec{
		Contexts: []testutil.Ctx{{Name: "wanted"}, {Name: "unwanted"}},
	})

	if err := h.run("import", src, "--context", "wanted"); err != nil {
		t.Fatalf("import --context: %v", err)
	}
	cfg := h.config()
	if _, ok := cfg.Contexts["wanted"]; !ok {
		t.Errorf("contexts = %v", cfg.Contexts)
	}
	if _, ok := cfg.Contexts["unwanted"]; ok {
		t.Error("an unselected context was imported")
	}
}

// Re-running an import is how people check whether it worked. It has to be a
// no-op rather than a wall of collisions.
func TestImportIsRepeatable(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", foreignSpec())

	if err := h.run("import", src); err != nil {
		t.Fatalf("first import: %v", err)
	}
	before := len(h.config().Clusters)

	if err := h.run("import", src); err != nil {
		t.Fatalf("second import: %v", err)
	}
	if !strings.Contains(h.stdout(), "unchanged") {
		t.Errorf("stdout = %q", h.stdout())
	}
	if !strings.Contains(h.stderr(), "Nothing to import") {
		t.Errorf("stderr = %q", h.stderr())
	}
	if got := len(h.config().Clusters); got != before {
		t.Errorf("clusters = %d, want %d — the second import duplicated stanzas", got, before)
	}
}

func TestImportJSON(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", foreignSpec())

	if err := h.run("import", src, "-o", "json"); err != nil {
		t.Fatalf("import -o json: %v", err)
	}

	var entries []transfer.Entry
	if err := json.Unmarshal([]byte(h.stdout()), &entries); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, h.stdout())
	}
	if len(entries) != 1 || entries[0].Action != transfer.ActionAdded {
		t.Errorf("entries = %+v", entries)
	}
}

func TestImportMissingFile(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("import", filepath.Join(filepath.Dir(h.kubeconfig), "absent.yaml")); err == nil {
		t.Fatal("importing a file that is not there must fail")
	}
}

// Inside a managed shell $KUBECONFIG is a copy that dies with the shell, so an
// import would report success and then leave with it.
func TestImportRefusedInsideASession(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", foreignSpec())
	t.Setenv(EnvShellID, "session-1")

	err := h.run("import", src)
	if err == nil {
		t.Fatal("import must refuse to write a throwaway kubeconfig")
	}
	if !strings.Contains(err.Error(), "import") {
		t.Errorf("error = %v, want it to name the operation", err)
	}
}

func TestExportWritesTheCurrentContext(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("export"); err != nil {
		t.Fatalf("export: %v", err)
	}

	out := parseKubeconfig(t, h.stdout())
	if len(out.Contexts) != 1 {
		t.Fatalf("contexts = %v, want only the current one", out.Contexts)
	}
	if out.CurrentContext != "dev" {
		t.Errorf("current context = %q, want dev", out.CurrentContext)
	}
	if len(out.Clusters) != 1 || len(out.AuthInfos) != 1 {
		t.Errorf("the export carries stanzas it does not reference: %+v", out)
	}
}

func TestExportNamedAndAll(t *testing.T) {
	h := newHarness(t, defaultSpec())

	// "." and an explicit name resolve the same way they do everywhere else, and
	// naming a context twice exports it once.
	if err := h.run("export", ".", "prod", "prod"); err != nil {
		t.Fatalf("export: %v", err)
	}
	out := parseKubeconfig(t, h.stdout())
	if len(out.Contexts) != 2 {
		t.Errorf("contexts = %v, want dev and prod once each", out.Contexts)
	}

	if err := h.run("export", "--all"); err != nil {
		t.Fatalf("export --all: %v", err)
	}
	if got := len(parseKubeconfig(t, h.stdout()).Contexts); got != 3 {
		t.Errorf("contexts = %d, want all 3", got)
	}
}

func TestExportRejectsBadInput(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("export", "nope"); err == nil {
		t.Error("exporting an unknown context must fail")
	}
	if err := h.run("export", "--all", "dev"); err == nil {
		t.Error("--all plus a name is a contradiction, not a filter")
	}
}

func TestExportToFile(t *testing.T) {
	h := newHarness(t, defaultSpec())
	path := filepath.Join(filepath.Dir(h.kubeconfig), "exported.yaml")

	if err := h.run("export", "prod", "-f", path); err != nil {
		t.Fatalf("export -f: %v", err)
	}
	if got := len(testutil.Read(t, path).Contexts); got != 1 {
		t.Errorf("contexts = %d, want 1", got)
	}
	// The file holds a token; anything group- or world-readable is a leak.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %v, want 0600", perm)
	}
	if !strings.Contains(h.stderr(), "credentials") {
		t.Errorf("stderr = %q, want the credential warning", h.stderr())
	}
	if h.stdout() != "" {
		t.Errorf("stdout = %q, want nothing when writing to a file", h.stdout())
	}

	// The natural typo is exporting over a kubeconfig that already exists.
	err = h.run("export", "dev", "-f", path)
	if err == nil {
		t.Fatal("an existing file must not be replaced silently")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error = %v, want it to name --force", err)
	}
	if got := testutil.Read(t, path).CurrentContext; got != "prod" {
		t.Errorf("the file was overwritten: current context = %q", got)
	}

	if err := h.run("export", "dev", "-f", path, "--force"); err != nil {
		t.Fatalf("export --force: %v", err)
	}
	if got := testutil.Read(t, path).CurrentContext; got != "dev" {
		t.Errorf("--force did not replace the file: current context = %q", got)
	}
}

func TestExportJSON(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("export", "-o", "json"); err != nil {
		t.Fatalf("export -o json: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(h.stdout()), &doc); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, h.stdout())
	}
	if doc["kind"] != "Config" {
		t.Errorf("kind = %v, want Config", doc["kind"])
	}
	// JSON is a subset of YAML, so the same bytes are still a usable kubeconfig.
	if got := parseKubeconfig(t, h.stdout()).CurrentContext; got != "dev" {
		t.Errorf("current context = %q", got)
	}
}

// Without --flatten an export names certificate paths that exist only on this
// machine; with it the file stands on its own.
func TestExportFlattenInlinesCertificates(t *testing.T) {
	h := newHarness(t, defaultSpec())
	dir := filepath.Dir(h.kubeconfig)
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("-----BEGIN CERTIFICATE-----\n"), 0o600); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	cfg := testutil.Config(defaultSpec())
	cfg.Clusters["dev-cluster"].CertificateAuthority = "ca.crt"
	testutil.Write(t, h.kubeconfig, cfg)

	if err := h.run("export", "dev"); err != nil {
		t.Fatalf("export: %v", err)
	}
	if out := parseKubeconfig(t, h.stdout()); len(out.Clusters["dev-cluster"].CertificateAuthorityData) != 0 {
		t.Error("the plain export should keep the path, not the contents")
	}

	if err := h.run("export", "dev", "--flatten"); err != nil {
		t.Fatalf("export --flatten: %v", err)
	}
	out := parseKubeconfig(t, h.stdout())
	if len(out.Clusters["dev-cluster"].CertificateAuthorityData) == 0 {
		t.Errorf("--flatten did not inline the certificate: %+v", out.Clusters["dev-cluster"])
	}
	if out.Clusters["dev-cluster"].CertificateAuthority != "" {
		t.Error("the path should be gone once the contents are embedded")
	}
}

// An export hands over a route to the cluster, so a guarded context asks first —
// and the question has to go to stderr, because stdout is the kubeconfig.
func TestExportGuardPromptStaysOffStdout(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardConfirmConfig)
	h.stdin("no\n")

	err := h.run("export", "prod")
	if code := ExitCode(err); code != ExitAborted {
		t.Errorf("ExitCode = %d, want %d", code, ExitAborted)
	}
	if h.stdout() != "" {
		t.Errorf("stdout = %q; a declined export must write nothing at all", h.stdout())
	}
	if !strings.Contains(h.stderr(), "Aborted") {
		t.Errorf("stderr = %q, want the prompt and the verdict", h.stderr())
	}

	h.stdin("prod\n")
	if err := h.run("export", "prod"); err != nil {
		t.Fatalf("export after retyping the name: %v", err)
	}
	if got := parseKubeconfig(t, h.stdout()).CurrentContext; got != "prod" {
		t.Errorf("current context = %q, want prod", got)
	}
}

// parseKubeconfig reads a kubeconfig out of captured output.
func parseKubeconfig(t *testing.T, data string) *clientcmdapi.Config {
	t.Helper()
	cfg, err := clientcmd.Load([]byte(data))
	if err != nil {
		t.Fatalf("output is not a kubeconfig: %v\n%s", err, data)
	}
	return cfg
}

// --context names something that is not in the user's kubeconfig yet, so its
// completion has to come from the source file rather than the merged config.
func TestCompleteSourceContexts(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", testutil.Spec{
		Contexts: []testutil.Ctx{{Name: "acme-prod"}, {Name: "acme-dev"}},
	})

	got, _ := completeSourceContexts(nil, []string{src}, "")
	joined := strings.Join(got, " ")
	for _, want := range []string{"acme-prod", "acme-dev"} {
		if !strings.Contains(joined, want) {
			t.Errorf("completions %v missing %q", got, want)
		}
	}
	if strings.Contains(joined, "staging") {
		t.Errorf("completions %v came from the user's own kubeconfig", got)
	}

	// A half-typed path is the normal state while completing, and must not turn
	// into an error the shell prints over the prompt.
	if got, _ := completeSourceContexts(nil, []string{src + ".partial"}, ""); len(got) != 0 {
		t.Errorf("completions = %v, want none", got)
	}
}

// --overwrite repoints a context, which can leave the cluster and user it used
// to name unreferenced. Reporting every unreferenced stanza instead of the ones
// this import created would blame it for a year of accumulated cruft.
func TestImportReportsOnlyWhatItOrphaned(t *testing.T) {
	h := newHarness(t, defaultSpec())

	// A stanza nothing referenced before the import was ever run.
	cfg := testutil.Config(defaultSpec())
	cfg.Clusters["already-orphaned"] = &clientcmdapi.Cluster{Server: "https://old.example.com:6443"}
	testutil.Write(t, h.kubeconfig, cfg)

	src := writeSource(t, h, "downloaded.yaml", testutil.Spec{
		Current:  "prod",
		Contexts: []testutil.Ctx{{Name: "prod", Cluster: "acme", User: "acme-user", Server: "https://acme.example.com:6443"}},
	})

	if err := h.run("import", src, "--overwrite"); err != nil {
		t.Fatalf("import --overwrite: %v", err)
	}
	for _, want := range []string{"prod-cluster", "prod-user", "--prune"} {
		if !strings.Contains(h.stderr(), want) {
			t.Errorf("stderr = %q, want it to mention %s", h.stderr(), want)
		}
	}
	if strings.Contains(h.stderr(), "already-orphaned") {
		t.Errorf("stderr = %q; this import did not orphan that one", h.stderr())
	}
	// Reported, not removed — the same bargain "kctx delete" strikes.
	if _, ok := h.config().Clusters["prod-cluster"]; !ok {
		t.Error("the stanza was removed without --prune being asked for")
	}
}

// The note says to re-run with --prune, so re-running with --prune has to work.
// By then the stanzas it named are no longer *newly* orphaned, so a prune scoped
// to this import's own leftovers would quietly skip them — and nothing else
// changed on that run either, so the write has to happen for the prune alone.
func TestImportPruneAfterTheHint(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", testutil.Spec{
		Current:  "prod",
		Contexts: []testutil.Ctx{{Name: "prod", Cluster: "acme", User: "acme-user", Server: "https://acme.example.com:6443"}},
	})

	if err := h.run("import", src, "--overwrite"); err != nil {
		t.Fatalf("import --overwrite: %v", err)
	}
	if err := h.run("import", src, "--overwrite", "--prune"); err != nil {
		t.Fatalf("import --prune: %v", err)
	}

	cfg := h.config()
	if _, ok := cfg.Clusters["prod-cluster"]; ok {
		t.Errorf("clusters = %v, want prod-cluster gone", cfg.Clusters)
	}
	if _, ok := cfg.AuthInfos["prod-user"]; ok {
		t.Errorf("users = %v, want prod-user gone", cfg.AuthInfos)
	}
	if strings.Contains(h.stderr(), "unreferenced") {
		t.Errorf("stderr = %q, want no leftover note after a prune", h.stderr())
	}
	// The context that replaced it still has to resolve.
	if got := cfg.Contexts["prod"].Cluster; got != "acme" {
		t.Errorf("prod points at %q, want acme", got)
	}
	if _, ok := cfg.Clusters["acme"]; !ok {
		t.Errorf("clusters = %v, want the imported one kept", cfg.Clusters)
	}
}

func TestImportDryRunDoesNotPrune(t *testing.T) {
	h := newHarness(t, defaultSpec())
	src := writeSource(t, h, "downloaded.yaml", testutil.Spec{
		Current:  "prod",
		Contexts: []testutil.Ctx{{Name: "prod", Cluster: "acme", User: "acme-user", Server: "https://acme.example.com:6443"}},
	})

	if err := h.run("import", src, "--overwrite", "--prune", "--dry-run"); err != nil {
		t.Fatalf("import --dry-run --prune: %v", err)
	}
	cfg := h.config()
	if _, ok := cfg.Clusters["prod-cluster"]; !ok {
		t.Error("--dry-run pruned anyway")
	}
	if got := cfg.Contexts["prod"].Cluster; got != "prod-cluster" {
		t.Errorf("--dry-run wrote the overwrite: prod points at %q", got)
	}
}
