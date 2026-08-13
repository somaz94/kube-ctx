package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/somaz94/kube-ctx/internal/testutil"
	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// envValue extracts one KEY=value entry from an environment slice.
//
// The last occurrence wins, matching how exec resolves a duplicated variable:
// the session entries are appended to the inherited environment, so an
// inherited KUBECONFIG appears first and the session's override second.
func envValue(env []string, key string) string {
	value := ""
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok && k == key {
			value = v
		}
	}
	return value
}

func TestShellSpawnsWithSessionKubeconfig(t *testing.T) {
	h := newHarness(t, defaultSpec())
	t.Setenv("SHELL", "/bin/zsh")

	var captured *exec.Cmd
	var sessionPath string
	var sessionContents string

	original := runCommand
	runCommand = func(cmd *exec.Cmd) error {
		captured = cmd
		sessionPath = envValue(cmd.Env, shellenv.EnvKubeconfig)
		data, err := os.ReadFile(sessionPath)
		if err != nil {
			t.Errorf("session kubeconfig missing while the shell runs: %v", err)
		}
		sessionContents = string(data)
		return nil
	}
	t.Cleanup(func() { runCommand = original })

	if err := h.run("shell", "prod"); err != nil {
		t.Fatalf("shell prod: %v", err)
	}

	if captured == nil {
		t.Fatal("no shell was spawned")
	}
	if filepath.Base(captured.Path) != "zsh" {
		t.Errorf("spawned %q, want $SHELL", captured.Path)
	}
	if sessionPath == h.kubeconfig {
		t.Error("the shell was pointed at the global kubeconfig")
	}
	if !strings.Contains(sessionContents, "current-context: prod") {
		t.Errorf("session kubeconfig is not pinned to prod:\n%s", sessionContents)
	}
	if got := envValue(captured.Env, shellenv.EnvActive); got != "prod" {
		t.Errorf("%s = %q, want prod", shellenv.EnvActive, got)
	}
	if got := envValue(captured.Env, shellenv.EnvShellID); got == "" {
		t.Errorf("%s is not set", shellenv.EnvShellID)
	}
	if got := envValue(captured.Env, shellenv.EnvDepth); got != "1" {
		t.Errorf("%s = %q, want 1", shellenv.EnvDepth, got)
	}

	// The global kubeconfig must be untouched — that is the whole point.
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("global current-context = %q, want dev", got)
	}
	// And the session copy must not outlive the shell.
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("session kubeconfig survived the shell")
	}
}

func TestShellDefaultsToCurrentContext(t *testing.T) {
	h := newHarness(t, defaultSpec())
	t.Setenv("SHELL", "/bin/sh")

	var active string
	original := runCommand
	runCommand = func(cmd *exec.Cmd) error {
		active = envValue(cmd.Env, shellenv.EnvActive)
		return nil
	}
	t.Cleanup(func() { runCommand = original })

	if err := h.run("shell"); err != nil {
		t.Fatalf("shell: %v", err)
	}
	if active != "dev" {
		t.Errorf("%s = %q, want dev", shellenv.EnvActive, active)
	}
}

func TestShellWithNamespace(t *testing.T) {
	h := newHarness(t, defaultSpec())
	t.Setenv("SHELL", "/bin/sh")

	var contents string
	original := runCommand
	runCommand = func(cmd *exec.Cmd) error {
		data, err := os.ReadFile(envValue(cmd.Env, shellenv.EnvKubeconfig))
		if err != nil {
			return err
		}
		contents = string(data)
		return nil
	}
	t.Cleanup(func() { runCommand = original })

	if err := h.run("shell", "prod", "-n", "kube-system"); err != nil {
		t.Fatalf("shell -n: %v", err)
	}
	if !strings.Contains(contents, "namespace: kube-system") {
		t.Errorf("session kubeconfig missing the namespace:\n%s", contents)
	}
	// The global copy keeps prod's original namespace.
	if got := h.config().Contexts["prod"].Namespace; got != "monitoring" {
		t.Errorf("global prod namespace = %q, want monitoring", got)
	}
}

func TestShellUnknownContext(t *testing.T) {
	h := newHarness(t, defaultSpec())
	captureRunNoop(t)

	if err := h.run("shell", "nope"); err == nil {
		t.Error("expected an error")
	}
}

func TestShellExitStatusIsNotAnError(t *testing.T) {
	h := newHarness(t, defaultSpec())
	t.Setenv("SHELL", "/bin/sh")

	original := runCommand
	runCommand = func(cmd *exec.Cmd) error {
		// A real non-zero exit, so the *exec.ExitError is the genuine article.
		return exec.Command("sh", "-c", "exit 3").Run()
	}
	t.Cleanup(func() { runCommand = original })

	if err := h.run("shell", "prod"); err != nil {
		t.Errorf("a non-zero shell exit is not a kube-ctx failure: %v", err)
	}
}

func TestShellSpawnFailureSurfaces(t *testing.T) {
	h := newHarness(t, defaultSpec())
	t.Setenv("SHELL", "/nonexistent/shell")

	original := runCommand
	runCommand = func(cmd *exec.Cmd) error { return errors.New("no such file") }
	t.Cleanup(func() { runCommand = original })

	err := h.run("shell", "prod")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "start shell") {
		t.Errorf("error = %v", err)
	}
}

func TestExecRunsAgainstContextWithoutSwitching(t *testing.T) {
	h := newHarness(t, defaultSpec())

	var captured *exec.Cmd
	var sessionContents string
	original := runCommand
	runCommand = func(cmd *exec.Cmd) error {
		captured = cmd
		data, err := os.ReadFile(envValue(cmd.Env, shellenv.EnvKubeconfig))
		if err != nil {
			return err
		}
		sessionContents = string(data)
		return nil
	}
	t.Cleanup(func() { runCommand = original })

	if err := h.run("exec", "prod", "--", "kubectl", "get", "pods"); err != nil {
		t.Fatalf("exec: %v", err)
	}

	if captured == nil {
		t.Fatal("nothing was executed")
	}
	if got := captured.Args; len(got) != 3 || got[0] != "kubectl" || got[2] != "pods" {
		t.Errorf("args = %v, want the command after --", got)
	}
	if !strings.Contains(sessionContents, "current-context: prod") {
		t.Errorf("the command did not run against prod:\n%s", sessionContents)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("global current-context = %q; exec must not switch", got)
	}
}

func TestExecPropagatesExitCode(t *testing.T) {
	h := newHarness(t, defaultSpec())

	original := runCommand
	runCommand = func(cmd *exec.Cmd) error {
		return exec.Command("sh", "-c", "exit 7").Run()
	}
	t.Cleanup(func() { runCommand = original })

	err := h.run("exec", "prod", "--", "false")
	if err == nil {
		t.Fatal("expected a non-zero exit")
	}
	if err.Error() != "" {
		t.Errorf("the failure should be silent, got %q", err.Error())
	}
	// The command's own status is what a caller scripting around kctx needs.
	if code := ExitCode(err); code != 7 {
		t.Errorf("ExitCode = %d, want 7", code)
	}
}

func TestExecStartFailureSurfaces(t *testing.T) {
	h := newHarness(t, defaultSpec())

	original := runCommand
	runCommand = func(cmd *exec.Cmd) error { return errors.New("executable file not found") }
	t.Cleanup(func() { runCommand = original })

	err := h.run("exec", "prod", "--", "nosuchbinary")
	if err == nil || !strings.Contains(err.Error(), "run nosuchbinary") {
		t.Errorf("error = %v", err)
	}
}

func TestExecNeedsAContextAndACommand(t *testing.T) {
	h := newHarness(t, defaultSpec())
	captureRunNoop(t)

	if err := h.run("exec", "prod"); err == nil {
		t.Error("expected an error without a command")
	}
	if err := h.run("exec", "nope", "--", "true"); err == nil {
		t.Error("expected an error for an unknown context")
	}
}

func TestExecResolvesAlias(t *testing.T) {
	h := newHarness(t, defaultSpec())
	if err := h.run("alias", "p", "prod"); err != nil {
		t.Fatalf("alias: %v", err)
	}

	var contents string
	original := runCommand
	runCommand = func(cmd *exec.Cmd) error {
		data, err := os.ReadFile(envValue(cmd.Env, shellenv.EnvKubeconfig))
		if err != nil {
			return err
		}
		contents = string(data)
		return nil
	}
	t.Cleanup(func() { runCommand = original })

	if err := h.run("exec", "p", "--", "true"); err != nil {
		t.Fatalf("exec p: %v", err)
	}
	if !strings.Contains(contents, "current-context: prod") {
		t.Errorf("alias not resolved:\n%s", contents)
	}
}

func TestShellWithoutCurrentContext(t *testing.T) {
	h := newHarness(t, testutil.Spec{Contexts: []testutil.Ctx{{Name: "dev"}}})
	captureRunNoop(t)

	if err := h.run("shell"); err == nil {
		t.Error("expected an error with no current context")
	}
}

func TestHookModeKeepsTheGlobalKubeconfigIntact(t *testing.T) {
	h := newHarness(t, defaultSpec())
	t.Setenv("SHELL", "/bin/zsh")

	envFile := filepath.Join(t.TempDir(), "env")
	t.Setenv(shellenv.EnvFile, envFile)

	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}

	// The global kubeconfig is untouched; the change lives in the exports.
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("global current-context = %q, want dev", got)
	}

	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	exports := string(data)
	for _, want := range []string{"export KUBECONFIG=", shellenv.EnvShellID, "export " + shellenv.EnvActive + "='prod'"} {
		if !strings.Contains(exports, want) {
			t.Errorf("exports missing %q:\n%s", want, exports)
		}
	}

	sessionPath := strings.Trim(strings.TrimPrefix(
		strings.SplitN(exports, "\n", 2)[0], "export KUBECONFIG="), "'")
	session := testutil.Read(t, sessionPath)
	if session.CurrentContext != "prod" {
		t.Errorf("session current-context = %q, want prod", session.CurrentContext)
	}
}

func TestHookModeSecondSwitchWritesToTheSession(t *testing.T) {
	h := newHarness(t, defaultSpec())
	envFile := filepath.Join(t.TempDir(), "env")
	t.Setenv(shellenv.EnvFile, envFile)

	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("first switch: %v", err)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	sessionPath := strings.Trim(strings.TrimPrefix(
		strings.SplitN(string(data), "\n", 2)[0], "export KUBECONFIG="), "'")

	// Simulate the shell having sourced the exports.
	t.Setenv("KUBECONFIG", sessionPath)
	t.Setenv(shellenv.EnvShellID, "session-1")
	if err := os.Truncate(envFile, 0); err != nil {
		t.Fatalf("truncate env file: %v", err)
	}

	if err := h.run("ctx", "staging"); err != nil {
		t.Fatalf("second switch: %v", err)
	}

	if got := testutil.Read(t, sessionPath).CurrentContext; got != "staging" {
		t.Errorf("session current-context = %q, want staging", got)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("global current-context = %q, want dev", got)
	}
	// No second session: the shell already has one.
	info, err := os.Stat(envFile)
	if err != nil {
		t.Fatalf("stat env file: %v", err)
	}
	if info.Size() != 0 {
		t.Error("a second session was created for a shell that already had one")
	}
}

func TestHookModeNamespaceSwitchIsAlsoLocal(t *testing.T) {
	h := newHarness(t, defaultSpec())
	envFile := filepath.Join(t.TempDir(), "env")
	t.Setenv(shellenv.EnvFile, envFile)

	if err := h.run("ns", "kube-system"); err != nil {
		t.Fatalf("ns: %v", err)
	}
	if got := h.config().Contexts["dev"].Namespace; got != "" {
		t.Errorf("global namespace = %q; the hook must keep it local", got)
	}
	if _, err := os.Stat(envFile); err != nil {
		t.Fatalf("no exports written: %v", err)
	}
}

func TestInitPrintsHookAndCompletions(t *testing.T) {
	h := newHarness(t, defaultSpec())

	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			if err := h.run("init", shell); err != nil {
				t.Fatalf("init %s: %v", shell, err)
			}
			out := h.stdout()
			if !strings.Contains(out, shellenv.EnvFile) {
				t.Errorf("hook missing from init %s output", shell)
			}
			if !strings.Contains(out, shellenv.EnvActive) {
				t.Errorf("prompt hint missing from init %s output", shell)
			}
			if !strings.Contains(out, "completion") && !strings.Contains(out, "complete") {
				t.Errorf("completions missing from init %s output", shell)
			}
		})
	}
}

func TestInitWithoutCompletion(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("init", "zsh", "--no-completion"); err != nil {
		t.Fatalf("init: %v", err)
	}
	if strings.Contains(h.stdout(), "compdef") {
		t.Errorf("completions should be omitted:\n%s", h.stdout())
	}
	if !strings.Contains(h.stdout(), "kctx() {") && !strings.Contains(h.stdout(), "() {") {
		t.Errorf("hook missing:\n%s", h.stdout())
	}
}

func TestInitFallsBackToShellEnv(t *testing.T) {
	h := newHarness(t, defaultSpec())
	t.Setenv("SHELL", "/bin/fish")

	if err := h.run("init", "--no-completion"); err != nil {
		t.Fatalf("init: %v", err)
	}
	if !strings.Contains(h.stdout(), "function") {
		t.Errorf("expected the fish hook:\n%s", h.stdout())
	}
}

func TestInitUnsupportedShell(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("init", "csh"); err == nil {
		t.Error("expected an error")
	}
}

// captureRunNoop stubs the spawner for tests that must never reach it.
func captureRunNoop(t *testing.T) {
	t.Helper()
	original := runCommand
	runCommand = func(cmd *exec.Cmd) error {
		t.Errorf("unexpected process spawn: %v", cmd.Args)
		return nil
	}
	t.Cleanup(func() { runCommand = original })
}
