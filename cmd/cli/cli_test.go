package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/internal/testutil"
)

// harness is one isolated CLI invocation: its own kubeconfig, its own config
// and state directories, and captured output.
type harness struct {
	t          *testing.T
	kubeconfig string
	out        bytes.Buffer
	errOut     bytes.Buffer
	in         *strings.Reader
}

// newHarness redirects every file kube-ctx touches into a temp dir.
func newHarness(t *testing.T, spec testutil.Spec) *harness {
	t.Helper()
	dir := t.TempDir()

	kubeconfig := filepath.Join(dir, "config")
	testutil.Write(t, kubeconfig, testutil.Config(spec))

	t.Setenv("KUBECONFIG", kubeconfig)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config-home"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("NO_COLOR", "1")
	t.Setenv(EnvShellID, "")

	return &harness{t: t, kubeconfig: kubeconfig, in: strings.NewReader("")}
}

// run executes one command line.
func (h *harness) run(args ...string) error {
	h.t.Helper()
	h.out.Reset()
	h.errOut.Reset()

	root := NewRootCmd(&h.out, &h.errOut, h.in)
	root.SetArgs(normalizeArgs(args))
	return root.Execute()
}

// stdin queues input for the next run.
func (h *harness) stdin(s string) { h.in = strings.NewReader(s) }

// config reloads the kubeconfig from disk.
func (h *harness) config() *clientcmdapi.Config {
	h.t.Helper()
	return testutil.Read(h.t, h.kubeconfig)
}

// stdout returns captured standard output.
func (h *harness) stdout() string { return h.out.String() }

// stderr returns captured standard error.
func (h *harness) stderr() string { return h.errOut.String() }

func defaultSpec() testutil.Spec {
	return testutil.Spec{
		Current: "dev",
		Contexts: []testutil.Ctx{
			{Name: "dev"},
			{Name: "prod", Namespace: "monitoring"},
			{Name: "staging"},
		},
	}
}

func TestCtxSwitch(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("current context = %q, want prod", got)
	}
	if !strings.Contains(h.stdout(), "Switched to context prod") {
		t.Errorf("stdout = %q", h.stdout())
	}
	if !strings.Contains(h.stdout(), "monitoring") {
		t.Errorf("the namespace should be reported: %q", h.stdout())
	}
}

func TestCtxUnknownContext(t *testing.T) {
	h := newHarness(t, defaultSpec())

	err := h.run("ctx", "nope")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %v, want it to name the context", err)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current context changed to %q on failure", got)
	}
}

func TestCtxPreviousAndHistoryDepth(t *testing.T) {
	h := newHarness(t, defaultSpec())

	for _, target := range []string{"prod", "staging"} {
		if err := h.run("ctx", target); err != nil {
			t.Fatalf("ctx %s: %v", target, err)
		}
	}

	// "-" goes back one: staging -> prod.
	if err := h.run("ctx", "-"); err != nil {
		t.Fatalf("ctx -: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("after '-' current = %q, want prod", got)
	}

	// "-2" goes back two from here.
	if err := h.run("ctx", "-2"); err != nil {
		t.Fatalf("ctx -2: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("after '-2' current = %q, want prod", got)
	}
}

func TestCtxBackFlagEqualsDashN(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	if err := h.run("ctx", "--back", "1"); err != nil {
		t.Fatalf("ctx --back 1: %v", err)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q, want dev", got)
	}
}

func TestCtxEmptyHistory(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("ctx", "-"); err == nil {
		t.Error("expected an error with no history")
	}
}

func TestCtxNoArgsListsContexts(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("ctx"); err != nil {
		t.Fatalf("ctx: %v", err)
	}
	lines := strings.Fields(h.stdout())
	if len(lines) != 3 {
		t.Fatalf("listed %v, want 3 contexts", lines)
	}
	if lines[0] != "dev" || lines[1] != "prod" || lines[2] != "staging" {
		t.Errorf("contexts not sorted: %v", lines)
	}
}

func TestCtxResolvesAlias(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("alias", "p", "prod"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	if err := h.run("ctx", "p"); err != nil {
		t.Fatalf("ctx p: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("current = %q, want prod", got)
	}
}

func TestCtxSwitchToCurrentKeepsHistory(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	// Re-selecting the current context must not push it onto the history and
	// bury the real previous one.
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod again: %v", err)
	}
	if err := h.run("ctx", "-"); err != nil {
		t.Fatalf("ctx -: %v", err)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q, want dev", got)
	}
}

func TestNsSwitch(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("ns", "kube-system"); err != nil {
		t.Fatalf("ns: %v", err)
	}
	if got := h.config().Contexts["dev"].Namespace; got != "kube-system" {
		t.Errorf("namespace = %q, want kube-system", got)
	}
	if !strings.Contains(h.stdout(), "kube-system") {
		t.Errorf("stdout = %q", h.stdout())
	}
}

func TestNsPreviousIsPerContext(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("ns", "kube-system"); err != nil {
		t.Fatalf("ns kube-system: %v", err)
	}
	if err := h.run("ns", "-"); err != nil {
		t.Fatalf("ns -: %v", err)
	}
	if got := h.config().Contexts["dev"].Namespace; got != "default" {
		t.Errorf("namespace = %q, want default", got)
	}

	// The prod context has its own namespace history, which is still empty.
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	if err := h.run("ns", "-"); err == nil {
		t.Error("expected an error: prod has no namespace history of its own")
	}
}

func TestNsWithoutCurrentContext(t *testing.T) {
	h := newHarness(t, testutil.Spec{Contexts: []testutil.Ctx{{Name: "dev"}}})

	err := h.run("ns", "default")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "no current context") {
		t.Errorf("error = %v", err)
	}
}

func TestList(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("list"); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := h.stdout()
	for _, want := range []string{"NAME", "NAMESPACE", "dev", "prod", "monitoring"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "CLUSTER") {
		t.Error("cluster column should need --wide")
	}
	// The current context is marked.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "dev") && !strings.HasPrefix(line, "*") {
			t.Errorf("current context not marked: %q", line)
		}
	}
}

func TestListWide(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("list", "--wide"); err != nil {
		t.Fatalf("list --wide: %v", err)
	}
	for _, want := range []string{"CLUSTER", "USER", "SERVER", "dev-cluster", "https://dev.example.com:6443"} {
		if !strings.Contains(h.stdout(), want) {
			t.Errorf("output missing %q:\n%s", want, h.stdout())
		}
	}
}

func TestListJSON(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("list", "-o", "json"); err != nil {
		t.Fatalf("list -o json: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(h.stdout()), "[") {
		t.Errorf("output is not a JSON array:\n%s", h.stdout())
	}
	if !strings.Contains(h.stdout(), `"Name":"dev"`) {
		t.Errorf("output = %s", h.stdout())
	}
}

func TestListEmptyKubeconfig(t *testing.T) {
	h := newHarness(t, testutil.Spec{})

	if err := h.run("list"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(h.stderr(), "No contexts") {
		t.Errorf("stderr = %q", h.stderr())
	}
}

func TestRename(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("rename", "dev", "development"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	cfg := h.config()
	if _, ok := cfg.Contexts["development"]; !ok {
		t.Error("renamed context missing")
	}
	if cfg.CurrentContext != "development" {
		t.Errorf("current = %q, want development", cfg.CurrentContext)
	}
}

func TestRenameCurrentWithDot(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("rename", ".", "here"); err != nil {
		t.Fatalf("rename .: %v", err)
	}
	if got := h.config().CurrentContext; got != "here" {
		t.Errorf("current = %q, want here", got)
	}
}

func TestRenameDotWithoutCurrent(t *testing.T) {
	h := newHarness(t, testutil.Spec{Contexts: []testutil.Ctx{{Name: "dev"}}})

	if err := h.run("rename", ".", "x"); err == nil {
		t.Error("expected an error")
	}
}

func TestRenameBacksUpKubeconfig(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("rename", "dev", "development"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	backups := filepath.Join(os.Getenv("XDG_STATE_HOME"), "kube-ctx", "backups")
	entries, err := os.ReadDir(backups)
	if err != nil {
		t.Fatalf("read backups: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d backup generations, want 1", len(entries))
	}
}

func TestDeleteRequiresConfirmation(t *testing.T) {
	h := newHarness(t, defaultSpec())
	h.stdin("n\n")

	if err := h.run("delete", "prod"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := h.config().Contexts["prod"]; !ok {
		t.Error("context deleted despite declining")
	}
	if !strings.Contains(h.stdout(), "Aborted") {
		t.Errorf("stdout = %q", h.stdout())
	}
}

func TestDeleteConfirmed(t *testing.T) {
	h := newHarness(t, defaultSpec())
	h.stdin("y\n")

	if err := h.run("delete", "prod"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	cfg := h.config()
	if _, ok := cfg.Contexts["prod"]; ok {
		t.Error("context not deleted")
	}
	// Shared-by-nobody cluster and user survive without --prune.
	if _, ok := cfg.Clusters["prod-cluster"]; !ok {
		t.Error("cluster removed without --prune")
	}
	if !strings.Contains(h.stderr(), "--prune") {
		t.Errorf("stderr should hint at --prune: %q", h.stderr())
	}
}

func TestDeleteWithPrune(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("delete", "prod", "--prune", "--yes"); err != nil {
		t.Fatalf("delete --prune: %v", err)
	}
	cfg := h.config()
	if _, ok := cfg.Clusters["prod-cluster"]; ok {
		t.Error("orphaned cluster not pruned")
	}
	if _, ok := cfg.AuthInfos["prod-user"]; ok {
		t.Error("orphaned user not pruned")
	}
	if _, ok := cfg.Clusters["dev-cluster"]; !ok {
		t.Error("in-use cluster was pruned")
	}
}

func TestDeleteCurrentWithDot(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("delete", ".", "--yes"); err != nil {
		t.Fatalf("delete .: %v", err)
	}
	cfg := h.config()
	if _, ok := cfg.Contexts["dev"]; ok {
		t.Error("current context not deleted")
	}
	if cfg.CurrentContext != "" {
		t.Errorf("current = %q, want empty", cfg.CurrentContext)
	}
}

func TestDeleteDotWithoutCurrent(t *testing.T) {
	h := newHarness(t, testutil.Spec{Contexts: []testutil.Ctx{{Name: "dev"}}})

	if err := h.run("delete", ".", "--yes"); err == nil {
		t.Error("expected an error")
	}
}

func TestDeleteUnknownContext(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("delete", "nope", "--yes"); err == nil {
		t.Error("expected an error")
	}
}

func TestAliasLifecycle(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("alias"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	if !strings.Contains(h.stderr(), "No aliases") {
		t.Errorf("stderr = %q", h.stderr())
	}

	if err := h.run("alias", "p", "prod"); err != nil {
		t.Fatalf("alias p prod: %v", err)
	}
	if err := h.run("alias"); err != nil {
		t.Fatalf("alias list: %v", err)
	}
	if !strings.Contains(h.stdout(), "p") || !strings.Contains(h.stdout(), "prod") {
		t.Errorf("stdout = %q", h.stdout())
	}

	if err := h.run("alias", "--delete", "p"); err != nil {
		t.Fatalf("alias --delete: %v", err)
	}
	if err := h.run("alias"); err != nil {
		t.Fatalf("alias list: %v", err)
	}
	if !strings.Contains(h.stderr(), "No aliases") {
		t.Errorf("alias survived deletion: %q", h.stdout())
	}
}

func TestAliasErrors(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("alias", "p"); err == nil {
		t.Error("expected an error for a lone alias name")
	}
	if err := h.run("alias", "p", "nope"); err == nil {
		t.Error("expected an error aliasing an unknown context")
	}
	if err := h.run("alias", "--delete", "nope"); err == nil {
		t.Error("expected an error deleting an unknown alias")
	}
}

func TestVersion(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("version"); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.HasPrefix(h.stdout(), "kctx ") {
		t.Errorf("stdout = %q", h.stdout())
	}

	if err := h.run("version", "-o", "json"); err != nil {
		t.Fatalf("version -o json: %v", err)
	}
	if !strings.Contains(h.stdout(), `"version"`) {
		t.Errorf("stdout = %q", h.stdout())
	}
}

func TestKubeconfigFlagOverridesEnv(t *testing.T) {
	h := newHarness(t, defaultSpec())

	other := filepath.Join(t.TempDir(), "other")
	testutil.Write(t, other, testutil.Config(testutil.Spec{
		Current: "only", Contexts: []testutil.Ctx{{Name: "only"}},
	}))

	if err := h.run("list", "--kubeconfig", other); err != nil {
		t.Fatalf("list --kubeconfig: %v", err)
	}
	if !strings.Contains(h.stdout(), "only") {
		t.Errorf("stdout = %q", h.stdout())
	}
	if strings.Contains(h.stdout(), "staging") {
		t.Error("the env kubeconfig leaked into the output")
	}
}

func TestNormalizeArgs(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{[]string{"ctx", "-2"}, []string{"ctx", "--back=2"}},
		{[]string{"ctx", "-"}, []string{"ctx", "-"}},
		{[]string{"ctx", "prod"}, []string{"ctx", "prod"}},
		{[]string{"list", "--wide"}, []string{"list", "--wide"}},
		{[]string{"ctx", "-o", "json"}, []string{"ctx", "-o", "json"}},
	}
	for _, tt := range tests {
		got := normalizeArgs(tt.in)
		if strings.Join(got, " ") != strings.Join(tt.want, " ") {
			t.Errorf("normalizeArgs(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestCompleteContexts(t *testing.T) {
	h := newHarness(t, defaultSpec())
	if err := h.run("alias", "p", "prod"); err != nil {
		t.Fatalf("alias: %v", err)
	}

	a := &app{out: &h.out, errOut: &h.errOut, in: h.in}
	got, directive := completeContexts(a)(nil, nil, "")
	if directive != 0 && directive != 4 {
		t.Logf("directive = %v", directive)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"dev", "prod", "staging", "p\t→ prod"} {
		if !strings.Contains(joined, want) {
			t.Errorf("completions %v missing %q", got, want)
		}
	}

	// A second positional argument has nothing to complete.
	if got, _ := completeContexts(a)(nil, []string{"dev"}, ""); got != nil {
		t.Errorf("completions for a second arg = %v, want none", got)
	}
}
