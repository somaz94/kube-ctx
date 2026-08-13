package cli

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/somaz94/kube-ctx/internal/testutil"
	"github.com/somaz94/kube-ctx/pkg/namespaces"
	"github.com/somaz94/kube-ctx/pkg/picker"
)

func TestConfirm(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		assumeYes bool
		want      bool
	}{
		{"y", "y\n", false, true},
		{"yes", "YES\n", false, true},
		{"n", "n\n", false, false},
		{"empty defaults to no", "\n", false, false},
		{"garbage is no", "maybe\n", false, false},
		{"closed stdin is no", "", false, false},
		{"--yes skips the prompt", "", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			a := &app{out: &out, errOut: &out, in: strings.NewReader(tt.input)}
			a.opts.assumeYes = tt.assumeYes

			got, err := confirm(a, "Proceed?")
			if err != nil {
				t.Fatalf("confirm: %v", err)
			}
			if got != tt.want {
				t.Errorf("confirm = %v, want %v", got, tt.want)
			}
			if tt.assumeYes && out.Len() != 0 {
				t.Errorf("--yes should not prompt, got %q", out.String())
			}
		})
	}
}

func TestConfirmPhrase(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		assumeYes bool
		want      bool
	}{
		{"exact match", "prod-eks\n", false, true},
		{"surrounding space is tolerated", "  prod-eks  \n", false, true},
		{"wrong phrase", "prod\n", false, false},
		{"empty", "\n", false, false},
		{"--yes skips", "", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			a := &app{out: &out, errOut: &out, in: strings.NewReader(tt.input)}
			a.opts.assumeYes = tt.assumeYes

			got, err := confirmPhrase(a, "Careful.", "prod-eks")
			if err != nil {
				t.Fatalf("confirmPhrase: %v", err)
			}
			if got != tt.want {
				t.Errorf("confirmPhrase = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfirmSurfacesWriteErrors(t *testing.T) {
	a := &app{out: failingWriter{}, errOut: failingWriter{}, in: strings.NewReader("y\n")}

	if _, err := confirm(a, "Proceed?"); err == nil {
		t.Error("expected the write failure to surface")
	}
	if _, err := confirmPhrase(a, "Careful.", "x"); err == nil {
		t.Error("expected the write failure to surface")
	}
}

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func TestContextWithTimeout(t *testing.T) {
	ctx, cancel := contextWithTimeout(0)
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Error("a non-positive duration should leave the context unbounded")
	}

	ctx, cancel = contextWithTimeout(time.Second)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Error("expected a deadline")
	}
}

func TestExitCode(t *testing.T) {
	if got := ExitCode(nil); got != 0 {
		t.Errorf("ExitCode(nil) = %d, want 0", got)
	}
	if got := ExitCode(errors.New("boom")); got != 1 {
		t.Errorf("ExitCode(plain error) = %d, want 1", got)
	}
	if got := ExitCode(&exitError{code: 7}); got != 7 {
		t.Errorf("ExitCode(exitError) = %d, want 7", got)
	}
}

func TestExecuteUsesProcessArgs(t *testing.T) {
	h := newHarness(t, defaultSpec())
	_ = h

	originalArgs := os.Args
	originalStdout := os.Stdout
	defer func() { os.Args, os.Stdout = originalArgs, originalStdout }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	os.Args = []string{"kctx", "version"}

	execErr := Execute()
	w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if execErr != nil {
		t.Fatalf("Execute: %v", execErr)
	}
	if !strings.HasPrefix(buf.String(), "kctx ") {
		t.Errorf("stdout = %q", buf.String())
	}
}

func TestExecuteReportsFailures(t *testing.T) {
	h := newHarness(t, defaultSpec())
	_ = h

	originalArgs := os.Args
	originalStderr := os.Stderr
	defer func() { os.Args, os.Stderr = originalArgs, originalStderr }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	os.Args = []string{"kctx", "ctx", "no-such-context"}

	execErr := Execute()
	w.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if execErr == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(buf.String(), "no-such-context") {
		t.Errorf("stderr = %q", buf.String())
	}
}

func TestWarnIfStale(t *testing.T) {
	var out bytes.Buffer
	a := &app{out: &out, errOut: &out, in: strings.NewReader("")}

	warnIfStale(a, namespaces.Result{Source: namespaces.SourceLive})
	if out.Len() != 0 {
		t.Errorf("a live result must not warn: %q", out.String())
	}

	warnIfStale(a, namespaces.Result{Source: namespaces.SourceCacheStale, Err: errors.New("timeout")})
	if !strings.Contains(out.String(), "stale") {
		t.Errorf("stderr = %q", out.String())
	}
}

func TestNamespaceOfFallsBackToDefault(t *testing.T) {
	cfg := testutil.Config(testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})

	if got := namespaceOf(cfg, "dev"); got != "default" {
		t.Errorf("namespaceOf = %q, want default", got)
	}
	if got := namespaceOf(cfg, "nope"); got != "default" {
		t.Errorf("namespaceOf(unknown) = %q, want default", got)
	}
}

func TestPickSurfacesConstructionErrors(t *testing.T) {
	original := newPicker
	newPicker = func(string) (*picker.Picker, func() error, error) {
		return nil, nil, errors.New("no terminal")
	}
	defer func() { newPicker = original }()

	if _, err := pick("context", []picker.Item{{Label: "dev"}}); err == nil {
		t.Error("expected the construction failure to surface")
	}
}

func TestCompleteNamespaces(t *testing.T) {
	h := newHarness(t, defaultSpec())
	seedNamespaceCache(t, "dev", "default", "kube-system")

	a := &app{out: &h.out, errOut: &h.errOut, in: h.in}

	got, _ := completeNamespaces(a)(nil, nil, "")
	if len(got) != 2 || got[0] != "default" {
		t.Errorf("completions = %v, want the cached namespaces", got)
	}

	// A second positional argument has nothing left to complete.
	if got, _ := completeNamespaces(a)(nil, []string{"default"}, ""); got != nil {
		t.Errorf("completions for a second arg = %v, want none", got)
	}
}

func TestCompleteNamespacesWithoutCurrentContext(t *testing.T) {
	h := newHarness(t, testutil.Spec{Contexts: []testutil.Ctx{{Name: "dev"}}})
	a := &app{out: &h.out, errOut: &h.errOut, in: h.in}

	if got, _ := completeNamespaces(a)(nil, nil, ""); got != nil {
		t.Errorf("completions = %v, want none without a current context", got)
	}
}

func TestAliasListRendersTable(t *testing.T) {
	h := newHarness(t, defaultSpec())

	for _, pair := range [][2]string{{"p", "prod"}, {"d", "dev"}} {
		if err := h.run("alias", pair[0], pair[1]); err != nil {
			t.Fatalf("alias %v: %v", pair, err)
		}
	}
	if err := h.run("alias"); err != nil {
		t.Fatalf("alias: %v", err)
	}

	out := h.stdout()
	if !strings.Contains(out, "ALIAS") || !strings.Contains(out, "CONTEXT") {
		t.Errorf("alias table missing headers:\n%s", out)
	}
	// Sorted by alias name.
	if strings.Index(out, "\nd") > strings.Index(out, "\np") {
		t.Errorf("aliases not sorted:\n%s", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("nonsense"); err == nil {
		t.Error("expected an error for an unknown subcommand")
	}
}

func TestBareCommandActsAsCtx(t *testing.T) {
	h := newHarness(t, defaultSpec())
	scriptPicker(t, "staging\r")

	if err := h.run(); err != nil {
		t.Fatalf("bare kctx: %v", err)
	}
	if got := h.config().CurrentContext; got != "staging" {
		t.Errorf("current = %q, want staging", got)
	}
}

func TestBareCommandWithoutTerminalLists(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run(); err != nil {
		t.Fatalf("bare kctx: %v", err)
	}
	if !strings.Contains(h.stdout(), "staging") {
		t.Errorf("stdout = %q", h.stdout())
	}
}
