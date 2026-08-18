package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/internal/testutil"
	"github.com/somaz94/kube-ctx/pkg/picker"
	"github.com/somaz94/kube-ctx/pkg/render"
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

	// Tests must never reach for the real terminal: a test binary launched
	// from a shell has a usable /dev/tty, and the picker would block forever
	// waiting for a keystroke. Individual tests opt in with scriptPicker.
	original := newPicker
	newPicker = func(string) (*picker.Picker, func() error, error) {
		return nil, nil, errPickerUnavailable
	}
	t.Cleanup(func() { newPicker = original })

	// Under `go test` argv[0] is the test binary, so the hook would be
	// generated for a function named after it. Pin the shipped name; tests
	// about the name set it themselves.
	originalName := invokedName
	invokedName = func() string { return "kctx" }
	t.Cleanup(func() { invokedName = originalName })

	return &harness{t: t, kubeconfig: kubeconfig, in: strings.NewReader("")}
}

// scriptPicker replaces the terminal picker with one driven by keys.
func scriptPicker(t *testing.T, keys string) *bytes.Buffer {
	t.Helper()
	var frames bytes.Buffer

	original := newPicker
	newPicker = func(prompt string) (*picker.Picker, func() error, error) {
		return &picker.Picker{
			In:      strings.NewReader(keys),
			Out:     &frames,
			Prompt:  prompt,
			Height:  5,
			Palette: render.NewEnabled(false),
		}, nil, nil
	}
	t.Cleanup(func() { newPicker = original })
	return &frames
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
	// lowerCamel, matching doctor's schema: one binary must not answer to two
	// JSON conventions.
	if !strings.Contains(h.stdout(), `"name": "dev"`) {
		t.Errorf("output = %s", h.stdout())
	}
	if strings.Contains(h.stdout(), `"Name"`) {
		t.Errorf("Go-style keys leaked into the JSON output:\n%s", h.stdout())
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

// Inside a kube-ctx-managed shell $KUBECONFIG is a private copy that is thrown
// away when the shell exits. A rename or delete there would report success and
// then leave with the copy, so both must refuse rather than quietly no-op.
func TestDurableEditsRefuseInsideAManagedShell(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"rename", []string{"rename", "prod", "prod2"}},
		{"delete", []string{"delete", "prod"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, defaultSpec())
			t.Setenv(EnvShellID, "abc123")
			h.stdin("y\n")

			err := h.run(tt.args...)
			if err == nil {
				t.Fatalf("%s succeeded inside a managed shell", tt.name)
			}
			if !strings.Contains(err.Error(), "private kubeconfig copy") {
				t.Errorf("error = %q, want it to explain the session copy", err)
			}
			if _, ok := h.config().Contexts["prod"]; !ok {
				t.Error("the kubeconfig was edited anyway")
			}
		})
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
		// Past the terminator the argv belongs to the child, and "-1" is an
		// ordinary value there — kubectl's --tail takes it. Rewriting it fed
		// the child a kube-ctx shorthand it has never heard of.
		{
			[]string{"exec", "dev", "--", "kubectl", "logs", "--tail", "-1"},
			[]string{"exec", "dev", "--", "kubectl", "logs", "--tail", "-1"},
		},
		// The terminator does not have to be the last chance to use the
		// shorthand: what comes before it is still kube-ctx's own argv.
		{
			[]string{"exec", "-2", "--", "sh", "-c", "echo -1"},
			[]string{"exec", "--back=2", "--", "sh", "-c", "echo -1"},
		},
		// A second "--" is the child's, not another terminator.
		{
			[]string{"exec", "dev", "--", "git", "log", "--", "-1"},
			[]string{"exec", "dev", "--", "git", "log", "--", "-1"},
		},
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

	// Commands taking one context have nothing to complete for a second
	// argument.
	if got, _ := completeContexts(a)(nil, []string{"dev"}, ""); got != nil {
		t.Errorf("completions for a second arg = %v, want none", got)
	}
}

// delete, doctor and guard add take a list, so every argument completes — and
// a name already on the line is not offered again.
func TestCompleteContextListCoversEveryArgument(t *testing.T) {
	h := newHarness(t, defaultSpec())
	a := &app{out: &h.out, errOut: &h.errOut, in: h.in}

	got, _ := completeContextList(a)(nil, []string{"dev"}, "")
	joined := strings.Join(got, " ")
	for _, want := range []string{"prod", "staging"} {
		if !strings.Contains(joined, want) {
			t.Errorf("completions %v missing %q", got, want)
		}
	}
	for _, unwanted := range []string{"dev "} {
		if strings.Contains(joined+" ", unwanted) {
			t.Errorf("completions %v re-offer an argument already given", got)
		}
	}
}

// seedNamespaceCache writes a fresh namespace cache entry so the ns command
// never needs a reachable API server.
func seedNamespaceCache(t *testing.T, ctxName string, names ...string) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "kube-ctx", "namespaces")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	entry := struct {
		Fetched    time.Time `json:"fetched"`
		Namespaces []string  `json:"namespaces"`
	}{Fetched: time.Now(), Namespaces: names}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ctxName+".json"), data, 0o600); err != nil {
		t.Fatalf("write cache: %v", err)
	}
}

func TestCtxPickerSelects(t *testing.T) {
	h := newHarness(t, defaultSpec())
	frames := scriptPicker(t, "prod\r")

	if err := h.run("ctx"); err != nil {
		t.Fatalf("ctx: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("current = %q, want prod", got)
	}
	if !strings.Contains(frames.String(), "prod") {
		t.Errorf("picker frame did not render the contexts:\n%q", frames.String())
	}
	if !strings.Contains(frames.String(), "current") {
		t.Errorf("the active context should be badged:\n%q", frames.String())
	}
}

func TestCtxPickerAbortChangesNothing(t *testing.T) {
	h := newHarness(t, defaultSpec())
	scriptPicker(t, "\x03")

	if err := h.run("ctx"); err != nil {
		t.Fatalf("aborting the picker should not be an error: %v", err)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q, want dev", got)
	}
	if h.stdout() != "" {
		t.Errorf("stdout = %q, want nothing after an abort", h.stdout())
	}
}

func TestNsPickerSelects(t *testing.T) {
	h := newHarness(t, defaultSpec())
	seedNamespaceCache(t, "dev", "default", "kube-system", "monitoring")
	scriptPicker(t, "kube\r")

	if err := h.run("ns"); err != nil {
		t.Fatalf("ns: %v", err)
	}
	if got := h.config().Contexts["dev"].Namespace; got != "kube-system" {
		t.Errorf("namespace = %q, want kube-system", got)
	}
}

func TestNsPickerAbortChangesNothing(t *testing.T) {
	h := newHarness(t, defaultSpec())
	seedNamespaceCache(t, "dev", "default", "kube-system")
	scriptPicker(t, "\x03")

	if err := h.run("ns"); err != nil {
		t.Fatalf("aborting the picker should not be an error: %v", err)
	}
	if got := h.config().Contexts["dev"].Namespace; got != "" {
		t.Errorf("namespace = %q, want it untouched", got)
	}
}

func TestNsNoArgsWithoutTerminalListsNamespaces(t *testing.T) {
	h := newHarness(t, defaultSpec())
	seedNamespaceCache(t, "dev", "default", "kube-system")

	if err := h.run("ns"); err != nil {
		t.Fatalf("ns: %v", err)
	}
	if !strings.Contains(h.stdout(), "kube-system") {
		t.Errorf("stdout = %q", h.stdout())
	}
	if got := h.config().Contexts["dev"].Namespace; got != "" {
		t.Errorf("listing must not change the namespace, got %q", got)
	}
}

func TestPickerWithNoItemsFallsBack(t *testing.T) {
	h := newHarness(t, testutil.Spec{})
	scriptPicker(t, "\r")

	// An empty context list has nothing to pick from, so the command falls
	// back to listing rather than opening an empty picker.
	if err := h.run("ctx"); err != nil {
		t.Fatalf("ctx: %v", err)
	}
	if h.stdout() != "" {
		t.Errorf("stdout = %q, want nothing", h.stdout())
	}
	// Bare "kctx" on a machine with no kubeconfig yet must not look like a
	// binary that silently did nothing.
	if !strings.Contains(h.stderr(), "No contexts found") {
		t.Errorf("stderr = %q, want an explanation", h.stderr())
	}
}

// kubectx trained everyone to type the context name straight after the tool,
// and until this worked "kctx staging" answered a context that plainly exists
// with "unknown command".
func TestBareContextArgumentSwitches(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("prod"); err != nil {
		t.Fatalf("kctx prod: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("current = %q, want prod", got)
	}
	if !strings.Contains(h.stdout(), "Switched to context prod") {
		t.Errorf("stdout = %q", h.stdout())
	}
}

// The error a typo gets has to name the real problem. "unknown command" sent
// the reader looking for a subcommand that was never the point.
func TestBareContextArgumentReportsAnUnknownContext(t *testing.T) {
	h := newHarness(t, defaultSpec())

	err := h.run("prodd")
	if err == nil || !strings.Contains(err.Error(), `no context named "prodd"`) {
		t.Fatalf("err = %v, want it to name the missing context", err)
	}
}

func TestBareContextArgumentAcceptsHistoryAndAliases(t *testing.T) {
	h := newHarness(t, defaultSpec())
	if err := h.run("alias", "p", "staging"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	if err := h.run("@p"); err != nil {
		t.Fatalf("kctx @p: %v", err)
	}
	if got := h.config().CurrentContext; got != "staging" {
		t.Errorf("current = %q, want staging", got)
	}

	// "-" and "-N" walk history here too; without the root --back flag,
	// normalizeArgs' rewrite left "kctx -2" saying "unknown flag: --back".
	if err := h.run("-"); err != nil {
		t.Fatalf("kctx -: %v", err)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q, want dev", got)
	}
}

// A guard is not optional just because the shorter form was used.
func TestBareContextArgumentIsGuarded(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardConfirmConfig)
	h.stdin("no\n")

	err := h.run("prod")
	if code := ExitCode(err); code != ExitAborted {
		t.Errorf("ExitCode = %d, want %d", code, ExitAborted)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q; a declined guard must change nothing", got)
	}
}

// A context whose name is also a subcommand loses to the subcommand, because
// "kctx list" must keep listing. The point of the test is that the escape
// hatch works, not that the collision is resolved the other way.
func TestSubcommandWinsOverACollidingContextName(t *testing.T) {
	h := newHarness(t, testutil.Spec{
		Current:  "dev",
		Contexts: []testutil.Ctx{{Name: "dev"}, {Name: "list"}},
	})

	if err := h.run("list"); err != nil {
		t.Fatalf("kctx list: %v", err)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q; \"kctx list\" must list, not switch", got)
	}

	if err := h.run("ctx", "list"); err != nil {
		t.Fatalf("kctx ctx list: %v", err)
	}
	if got := h.config().CurrentContext; got != "list" {
		t.Errorf("current = %q; \"kctx ctx list\" is the escape hatch", got)
	}
}

// Bare "kctx" keeps doing what it did: the picker, or the list without a
// terminal.
func TestBareKctxStillListsWithoutATerminal(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run(); err != nil {
		t.Fatalf("kctx: %v", err)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q; bare kctx must not switch", got)
	}
	for _, name := range []string{"dev", "prod", "staging"} {
		if !strings.Contains(h.stdout(), name) {
			t.Errorf("stdout = %q, want it to list %s", h.stdout(), name)
		}
	}
}
