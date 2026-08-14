package cli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/somaz94/kube-ctx/internal/testutil"
	"github.com/somaz94/kube-ctx/pkg/config"
)

// guardConfirmConfig makes "prod" a danger context that demands retyping.
const guardConfirmConfig = "guards:\n  - match: 'prod'\n    level: danger\n    confirm: true\n"

// allowRun stubs the spawner for tests where reaching it is the expected
// outcome, and reports whether it was reached.
//
// The sibling captureRunNoop fails the test on any spawn; these cases need the
// opposite, since "the guard let it through" is what they assert.
func allowRun(t *testing.T) *bool {
	t.Helper()
	var ran bool
	original := runCommand
	runCommand = func(cmd *exec.Cmd) error { ran = true; return nil }
	t.Cleanup(func() { runCommand = original })
	return &ran
}

// A guard that only covered "kctx ctx" would be bypassed by the two commands
// that reach a cluster without switching to it — and "exec" is the more
// dangerous of the two, since it runs a command rather than just arriving.
func TestGuardCoversEveryRouteToACluster(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"ctx", []string{"ctx", "prod"}},
		{"shell", []string{"shell", "prod"}},
		{"exec", []string{"exec", "prod", "--", "true"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, defaultSpec())
			writeUserConfig(t, guardConfirmConfig)
			captureRunNoop(t)
			h.stdin("no\n")

			err := h.run(tt.args...)
			if !strings.Contains(h.stdout(), "Aborted") {
				t.Errorf("%s ran without asking: %q", tt.name, h.stdout())
			}
			if code := ExitCode(err); code != ExitAborted {
				t.Errorf("ExitCode = %d, want %d", code, ExitAborted)
			}
			if got := h.config().CurrentContext; got != "dev" {
				t.Errorf("current = %q; a declined guard must change nothing", got)
			}
		})
	}
}

// Retyping the name lets the command through.
func TestGuardConfirmedExecRuns(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardConfirmConfig)
	h.stdin("prod\n")
	ran := allowRun(t)

	if err := h.run("exec", "prod", "--", "true"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !*ran {
		t.Error("the command did not run after the guard was satisfied")
	}
}

// Running against production with no indication of where the command is going
// is the thing this tool exists to stop.
func TestExecAnnouncesAGuardedContext(t *testing.T) {
	h := newHarness(t, defaultSpec())
	allowRun(t)

	if err := h.run("exec", "prod", "--", "true"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(h.stderr(), "DANGER") {
		t.Errorf("stderr = %q, want the guard badge", h.stderr())
	}
	// A context nothing classifies must stay quiet.
	h2 := newHarness(t, defaultSpec())
	allowRun(t)
	if err := h2.run("exec", "dev", "--", "true"); err != nil {
		t.Fatalf("exec dev: %v", err)
	}
	if strings.Contains(h2.stderr(), "Running against") {
		t.Errorf("stderr = %q, want nothing for an unguarded context", h2.stderr())
	}
}

// Moving between namespaces inside production is still moving around inside
// production.
func TestNsCarriesTheGuardBadge(t *testing.T) {
	h := newHarness(t, defaultSpec())
	h.stdin("prod\n")
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}

	if err := h.run("ns", "kube-system"); err != nil {
		t.Fatalf("ns: %v", err)
	}
	if !strings.Contains(h.stdout(), "DANGER") {
		t.Errorf("stdout = %q, want the guard badge on the namespace switch", h.stdout())
	}
}

// -y is the documented escape hatch for scripts, and it has to work on every
// route the guard now covers.
func TestAssumeYesSkipsTheGuardEverywhere(t *testing.T) {
	for _, args := range [][]string{
		{"ctx", "prod", "-y"},
		{"shell", "prod", "-y"},
		{"exec", "prod", "-y", "--", "true"},
	} {
		t.Run(args[0], func(t *testing.T) {
			h := newHarness(t, defaultSpec())
			writeUserConfig(t, guardConfirmConfig)
			allowRun(t)

			if err := h.run(args...); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			// "to continue:" is the prompt's own wording — matching on "Type "
			// alone would also hit the subshell's "Type exit to leave."
			if strings.Contains(h.stdout(), "to continue:") {
				t.Errorf("-y still prompted: %q", h.stdout())
			}
		})
	}
}

// -o carries the machine-readable contract, so an unknown value has to be an
// error: falling back to the default hands a script a human table to parse.
func TestOutputFormatIsValidated(t *testing.T) {
	for _, format := range []string{"bogus", "yaml", "jsno", ""} {
		h := newHarness(t, defaultSpec())
		err := h.run("list", "-o", format)
		if err == nil {
			t.Errorf("-o %q was accepted", format)
			continue
		}
		if !strings.Contains(err.Error(), "unknown output format") {
			t.Errorf("-o %q: error = %v", format, err)
		}
	}
	for _, format := range []string{"color", "plain", "json"} {
		h := newHarness(t, defaultSpec())
		if err := h.run("list", "-o", format); err != nil {
			t.Errorf("-o %q was rejected: %v", format, err)
		}
	}
}

// "-o plain" is the same request as --no-color; it was documented for a long
// time while doing nothing at all.
func TestPlainOutputHasNoEscapes(t *testing.T) {
	h := newHarness(t, defaultSpec())
	if err := h.run("list", "-o", "plain"); err != nil {
		t.Fatalf("list -o plain: %v", err)
	}
	if strings.Contains(h.stdout(), "\x1b[") {
		t.Errorf("plain output carries ANSI escapes: %q", h.stdout())
	}
}

// A prompt calls this on every keystroke: it must print the bare name, change
// nothing, and never open a picker.
func TestCurrent(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("current"); err != nil {
		t.Fatalf("current: %v", err)
	}
	if got := strings.TrimSpace(h.stdout()); got != "dev" {
		t.Errorf("stdout = %q, want the bare context name", got)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current must not switch, got %q", got)
	}
}

func TestCurrentNamespace(t *testing.T) {
	h := newHarness(t, defaultSpec())
	if err := h.run("current", "-n"); err != nil {
		t.Fatalf("current -n: %v", err)
	}
	if got := strings.TrimSpace(h.stdout()); got != "default" {
		t.Errorf("stdout = %q", got)
	}
}

func TestCurrentJSON(t *testing.T) {
	h := newHarness(t, defaultSpec())
	if err := h.run("current", "-o", "json"); err != nil {
		t.Fatalf("current -o json: %v", err)
	}
	for _, want := range []string{`"context": "dev"`, `"namespace": "default"`} {
		if !strings.Contains(h.stdout(), want) {
			t.Errorf("stdout = %q, want %s", h.stdout(), want)
		}
	}
}

// Silent rather than noisy: a prompt integration should not paint an error
// into the terminal on every keystroke.
func TestCurrentWithNothingSet(t *testing.T) {
	h := newHarness(t, testutil.Spec{})

	err := h.run("current")
	if code := ExitCode(err); code != ExitFailure {
		t.Errorf("ExitCode = %d, want %d", code, ExitFailure)
	}
	if h.stdout() != "" {
		t.Errorf("stdout = %q, want nothing", h.stdout())
	}
}

// Completion offers aliases for every context-taking command, so every one of
// them has to accept an alias — suggesting an input and then rejecting it is
// worse than not suggesting it.
func TestAliasesResolveEverywhere(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"ctx", []string{"ctx", "p"}},
		{"rename", []string{"rename", "p", "renamed"}},
		{"shell", []string{"shell", "p"}},
		{"exec", []string{"exec", "p", "--", "true"}},
		{"guard add", []string{"guard", "add", "p"}},
		{"doctor", []string{"doctor", "p", "--timeout", "50ms"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, defaultSpec())
			allowRun(t)
			if err := h.run("alias", "p", "prod"); err != nil {
				t.Fatalf("alias: %v", err)
			}

			// doctor exits non-zero on an unreachable cluster; that is a
			// healthy answer here, "no context named" is not.
			if err := h.run(tt.args...); err != nil && strings.Contains(err.Error(), "no context named") {
				t.Errorf("%s rejected the alias: %v", tt.name, err)
			}
		})
	}
}

// delete resolves aliases too, checked separately because it prompts.
func TestDeleteResolvesAlias(t *testing.T) {
	h := newHarness(t, defaultSpec())
	if err := h.run("alias", "p", "prod"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	h.stdin("y\n")

	if err := h.run("delete", "p"); err != nil {
		t.Fatalf("delete p: %v", err)
	}
	if _, ok := h.config().Contexts["prod"]; ok {
		t.Error("the aliased context was not deleted")
	}
}

// A cluster can answer with an empty list when RBAC forbids listing
// namespaces; printing nothing and exiting 0 reads as a broken binary.
func TestNsWithNoNamespacesExplainsItself(t *testing.T) {
	h := newHarness(t, defaultSpec())
	seedNamespaceCache(t, "dev")

	if err := h.run("ns"); err != nil {
		t.Fatalf("ns: %v", err)
	}
	if !strings.Contains(h.stderr(), "No namespaces") {
		t.Errorf("stderr = %q, want an explanation", h.stderr())
	}
}

func TestAliasAndGuardSupportJSON(t *testing.T) {
	h := newHarness(t, defaultSpec())
	if err := h.run("alias", "p", "prod"); err != nil {
		t.Fatalf("alias: %v", err)
	}

	if err := h.run("alias", "-o", "json"); err != nil {
		t.Fatalf("alias -o json: %v", err)
	}
	if !strings.Contains(h.stdout(), `"alias": "p"`) {
		t.Errorf("alias json = %q", h.stdout())
	}

	h2 := newHarness(t, defaultSpec())
	if err := h2.run("guard", "list", "-o", "json"); err != nil {
		t.Fatalf("guard list -o json: %v", err)
	}
	if !strings.Contains(h2.stdout(), `"level": "danger"`) {
		t.Errorf("guard json = %q", h2.stdout())
	}
	// The human-only hint must not contaminate the machine output.
	if strings.Contains(h2.stdout(), "built-in defaults") {
		t.Errorf("json output carries the hint: %q", h2.stdout())
	}
}

// A bare index is the kind of opaque argument completion exists for.
func TestCompleteGuardPositions(t *testing.T) {
	h := newHarness(t, defaultSpec())
	a := &app{out: &h.out, errOut: &h.errOut, in: h.in}

	// With nothing configured the built-in rules are still numbered.
	got, _ := completeGuardPositions(a)(nil, nil, "")
	if len(got) != len(config.DefaultGuards()) {
		t.Fatalf("completions = %v, want one per default rule", got)
	}
	if !strings.HasPrefix(got[0], "1\t") {
		t.Errorf("completions are not numbered from 1: %v", got)
	}
	if !strings.Contains(got[0], "danger") {
		t.Errorf("completion %q does not describe the rule", got[0])
	}
	// Only the one argument the command takes.
	if got, _ := completeGuardPositions(a)(nil, []string{"1"}, ""); got != nil {
		t.Errorf("completions for a second arg = %v, want none", got)
	}
}

func TestCompleteAliases(t *testing.T) {
	h := newHarness(t, defaultSpec())
	if err := h.run("alias", "p", "prod"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	a := &app{out: &h.out, errOut: &h.errOut, in: h.in}

	got, _ := completeAliases(a)(nil, nil, "")
	if len(got) != 1 || !strings.HasPrefix(got[0], "p\t") {
		t.Errorf("completions = %v, want the defined alias", got)
	}
}

// A nil slice must serialize as [], not null: a consumer piping into jq should
// get an empty list rather than a value it has to special-case.
func TestJSONEmptyListIsAnArray(t *testing.T) {
	h := newHarness(t, testutil.Spec{})

	if err := h.run("list", "-o", "json"); err != nil {
		t.Fatalf("list -o json: %v", err)
	}
	if got := strings.TrimSpace(h.stdout()); got != "[]" {
		t.Errorf("stdout = %q, want []", got)
	}

	h2 := newHarness(t, defaultSpec())
	if err := h2.run("alias", "-o", "json"); err != nil {
		t.Fatalf("alias -o json: %v", err)
	}
	if got := strings.TrimSpace(h2.stdout()); got != "[]" {
		t.Errorf("alias stdout = %q, want []", got)
	}
}

// current reads the namespace of whatever context is active, so a switch has
// to be reflected without a second lookup by the caller.
func TestCurrentFollowsTheActiveContext(t *testing.T) {
	h := newHarness(t, defaultSpec())
	h.stdin("prod\n")
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}

	if err := h.run("current", "-n"); err != nil {
		t.Fatalf("current -n: %v", err)
	}
	if !strings.Contains(h.stdout(), "monitoring") {
		t.Errorf("stdout = %q, want prod's namespace", h.stdout())
	}
}
