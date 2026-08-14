package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/somaz94/kube-ctx/pkg/config"
	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// inDir runs the rest of the test with the process working directory moved to
// dir. Bindings are keyed by the working directory, so a test that wants to be
// somewhere has to actually go there.
func inDir(t *testing.T, dir string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
}

// bindEnvFile points kube-ctx at a file to write shell exports to, the way the
// hook does, and returns a reader for it.
func bindEnvFile(t *testing.T) func() string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "env")
	t.Setenv(shellenv.EnvFile, path)
	t.Setenv(shellenv.EnvShell, string(shellenv.Bash))

	return func() string {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return ""
		}
		if err != nil {
			t.Fatalf("read env file: %v", err)
		}
		return string(data)
	}
}

func TestBindRecordsAndLists(t *testing.T) {
	h := newHarness(t, defaultSpec())
	dir := t.TempDir()
	inDir(t, dir)

	if err := h.run("bind", "prod"); err != nil {
		t.Fatalf("bind prod: %v", err)
	}
	if !strings.Contains(h.stdout(), "prod") {
		t.Errorf("stdout = %q", h.stdout())
	}

	if err := h.run("bind", "-o", "json"); err != nil {
		t.Fatalf("bind -o json: %v", err)
	}
	var list []config.BindingPair
	if err := json.Unmarshal([]byte(h.stdout()), &list); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, h.stdout())
	}
	if len(list) != 1 || list[0].Target != "prod" {
		t.Fatalf("bindings = %+v", list)
	}
	// Stored absolute, so the binding still resolves from anywhere.
	if !filepath.IsAbs(list[0].Directory) {
		t.Errorf("directory = %q, want an absolute path", list[0].Directory)
	}

	if err := h.run("bind", "--delete"); err != nil {
		t.Fatalf("bind --delete: %v", err)
	}
	if err := h.run("bind"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if !strings.Contains(h.stderr(), "No directories are bound") {
		t.Errorf("stderr = %q", h.stderr())
	}
}

func TestBindResolvesAliasesAndRejectsUnknown(t *testing.T) {
	h := newHarness(t, defaultSpec())
	dir := t.TempDir()
	inDir(t, dir)

	if err := h.run("alias", "p", "prod"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	if err := h.run("bind", "p"); err != nil {
		t.Fatalf("bind p: %v", err)
	}
	if !strings.Contains(h.stdout(), "prod") {
		t.Errorf("stdout = %q, want the alias resolved", h.stdout())
	}

	if err := h.run("bind", "nope"); err == nil {
		t.Error("binding to a context that does not exist must fail")
	}
	if err := h.run("bind", "--delete", "--path", filepath.Join(dir, "elsewhere")); err == nil {
		t.Error("unbinding a directory with no binding must fail")
	}
}

// The hook runs --apply on every directory change, so the path where nothing is
// bound must not even open the kubeconfig, let alone switch.
func TestBindApplyDoesNothingWithoutBindings(t *testing.T) {
	h := newHarness(t, defaultSpec())
	inDir(t, t.TempDir())
	envFile := bindEnvFile(t)

	if err := h.run("bind", "--apply"); err != nil {
		t.Fatalf("bind --apply: %v", err)
	}
	if h.stdout() != "" || h.stderr() != "" {
		t.Errorf("stdout = %q, stderr = %q; an unbound directory is silent", h.stdout(), h.stderr())
	}
	if envFile() != "" {
		t.Errorf("env file = %q, want nothing written", envFile())
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q, want dev", got)
	}
}

func TestBindApplySwitchesOnceOnEntry(t *testing.T) {
	h := newHarness(t, defaultSpec())
	dir := t.TempDir()
	inDir(t, dir)
	envFile := bindEnvFile(t)

	if err := h.run("bind", "staging"); err != nil {
		t.Fatalf("bind staging: %v", err)
	}
	if err := h.run("bind", "--apply"); err != nil {
		t.Fatalf("bind --apply: %v", err)
	}

	exports := envFile()
	if !strings.Contains(exports, "export "+shellenv.EnvBound+"='staging'") {
		t.Errorf("exports = %q, want the binding recorded", exports)
	}
	// Shell-local: the hook is installed, so the global kubeconfig is untouched.
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("global current-context = %q, want dev", got)
	}
	// The switch is announced on stderr, since a cd did not ask for stdout.
	if !strings.Contains(h.stderr(), "staging") || h.stdout() != "" {
		t.Errorf("stdout = %q, stderr = %q", h.stdout(), h.stderr())
	}

	// Moving around inside the bound tree does not switch again — which is what
	// keeps a context chosen by hand in there from being undone by the next cd.
	t.Setenv(shellenv.EnvBound, "staging")
	if err := h.run("bind", "--apply"); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if h.stderr() != "" {
		t.Errorf("stderr = %q, want silence on a repeat", h.stderr())
	}
}

// The deepest binding wins, and a directory that merely starts with the same
// letters as a bound one is not inside it.
func TestBindApplyPicksTheNearestBinding(t *testing.T) {
	h := newHarness(t, defaultSpec())
	root := t.TempDir()
	inner := filepath.Join(root, "inner")
	sibling := root + "-sibling"
	for _, dir := range []string{inner, sibling} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	inDir(t, root)
	if err := h.run("bind", "staging"); err != nil {
		t.Fatalf("bind root: %v", err)
	}
	inDir(t, inner)
	if err := h.run("bind", "prod"); err != nil {
		t.Fatalf("bind inner: %v", err)
	}

	bindEnvFile(t)
	if err := h.run("bind", "--apply"); err != nil {
		t.Fatalf("apply in inner: %v", err)
	}
	if !strings.Contains(h.stderr(), "prod") {
		t.Errorf("stderr = %q, want the deeper binding", h.stderr())
	}

	inDir(t, sibling)
	if err := h.run("bind", "--apply"); err != nil {
		t.Fatalf("apply in the sibling: %v", err)
	}
	if h.stderr() != "" {
		t.Errorf("stderr = %q; %s is not inside %s", h.stderr(), sibling, root)
	}
}

// Arriving in production by walking into a directory is the accident this tool
// exists to prevent, and a prompt on every cd is unusable — so a guarded
// context is named, not entered.
func TestBindApplyRefusesAGuardedContext(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardConfirmConfig)
	inDir(t, t.TempDir())
	envFile := bindEnvFile(t)

	if err := h.run("bind", "prod"); err != nil {
		t.Fatalf("bind prod: %v", err)
	}
	if err := h.run("bind", "--apply"); err != nil {
		t.Fatalf("bind --apply: %v", err)
	}

	if !strings.Contains(h.stderr(), "guarded") {
		t.Errorf("stderr = %q, want it to say why", h.stderr())
	}
	if strings.Contains(envFile(), "KUBECONFIG") {
		t.Errorf("exports = %q; a guarded context must not be entered", envFile())
	}
	// Recorded anyway, so the refusal is stated once on entering the tree
	// rather than on every directory change inside it.
	if !strings.Contains(envFile(), shellenv.EnvBound) {
		t.Errorf("exports = %q, want the refusal recorded", envFile())
	}
}

// A binding outliving its context is ordinary — the context was renamed or
// deleted — and must not turn every cd into an error.
func TestBindApplyToleratesAMissingContext(t *testing.T) {
	h := newHarness(t, defaultSpec())
	inDir(t, t.TempDir())
	bindEnvFile(t)

	if err := h.run("bind", "staging"); err != nil {
		t.Fatalf("bind staging: %v", err)
	}
	if err := h.run("delete", "staging", "--yes"); err != nil {
		t.Fatalf("delete staging: %v", err)
	}
	if err := h.run("bind", "--apply"); err != nil {
		t.Errorf("a stale binding must not fail the directory change: %v", err)
	}
	if !strings.Contains(h.stderr(), "staging") {
		t.Errorf("stderr = %q, want the stale binding named", h.stderr())
	}
}

// "kctx shell prod" promised to stay on prod. A cd is not a request to break it.
func TestBindApplySkipsAPinnedShell(t *testing.T) {
	h := newHarness(t, defaultSpec())
	inDir(t, t.TempDir())
	envFile := bindEnvFile(t)

	if err := h.run("bind", "staging"); err != nil {
		t.Fatalf("bind staging: %v", err)
	}
	t.Setenv(shellenv.EnvPinned, "1")
	if err := h.run("bind", "--apply"); err != nil {
		t.Fatalf("bind --apply: %v", err)
	}
	if envFile() != "" || h.stderr() != "" {
		t.Errorf("exports = %q, stderr = %q; a pinned shell is left alone", envFile(), h.stderr())
	}
}

// Without the hook there is nowhere to record the binding, and the switch was
// global — so it has to land in the kubeconfig rather than vanish.
func TestBindApplyWithoutTheHookSwitchesGlobally(t *testing.T) {
	h := newHarness(t, defaultSpec())
	inDir(t, t.TempDir())

	if err := h.run("bind", "staging"); err != nil {
		t.Fatalf("bind staging: %v", err)
	}
	if err := h.run("bind", "--apply"); err != nil {
		t.Fatalf("bind --apply: %v", err)
	}
	if got := h.config().CurrentContext; got != "staging" {
		t.Errorf("current-context = %q, want staging", got)
	}
}

func TestBindPathTargetsAnotherDirectory(t *testing.T) {
	h := newHarness(t, defaultSpec())
	inDir(t, t.TempDir())
	elsewhere := t.TempDir()

	if err := h.run("bind", "prod", "--path", elsewhere); err != nil {
		t.Fatalf("bind --path: %v", err)
	}
	// The current directory is not the one that was bound.
	bindEnvFile(t)
	if err := h.run("bind", "--apply"); err != nil {
		t.Fatalf("bind --apply: %v", err)
	}
	if h.stderr() != "" {
		t.Errorf("stderr = %q; nothing is bound here", h.stderr())
	}

	inDir(t, elsewhere)
	if err := h.run("bind", "--apply"); err != nil {
		t.Fatalf("bind --apply: %v", err)
	}
	if !strings.Contains(h.stderr(), "prod") {
		t.Errorf("stderr = %q, want the binding applied there", h.stderr())
	}
}
