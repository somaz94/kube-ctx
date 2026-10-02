package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/somaz94/kube-ctx/internal/testutil"
	"github.com/somaz94/kube-ctx/pkg/config"
	"github.com/somaz94/kube-ctx/pkg/guard"
	"github.com/somaz94/kube-ctx/pkg/render"
	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// guardConfirmConfig makes "prod" a danger context that demands retyping.
const guardConfirmConfig = "guards:\n  - match: 'prod'\n    level: danger\n    confirm: true\n"

// allowRun stubs the spawner for tests that expect to reach it, unlike
// captureRunNoop, and reports whether it was reached.
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
		{"export", "prod", "-y"},
	} {
		t.Run(args[0], func(t *testing.T) {
			h := newHarness(t, defaultSpec())
			writeUserConfig(t, guardConfirmConfig)
			allowRun(t)

			if err := h.run(args...); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
			// "to continue:" is the prompt's own wording — matching on "Type "
			// alone would also hit the subshell's "Type exit to leave." Both
			// streams: export asks on stderr, since stdout is the kubeconfig.
			if out := h.stdout() + h.stderr(); strings.Contains(out, "to continue:") {
				t.Errorf("-y still prompted: %q", out)
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
	// Over a buffer, with NO_COLOR set by the harness, color is off no matter
	// what -o says; a terminal has to be stood in for this to be able to fail.
	original := newPalette
	newPalette = func(_ io.Writer, forceNoColor bool) render.Palette { return render.NewEnabled(!forceNoColor) }
	t.Cleanup(func() { newPalette = original })

	if err := h.run("list"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(h.stdout(), "\x1b[") {
		t.Fatalf("the stand-in terminal got no color, so the checks below prove nothing: %q", h.stdout())
	}
	for _, args := range [][]string{{"list", "-o", "plain"}, {"list", "--no-color"}} {
		if err := h.run(args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(h.stdout(), "\x1b[") {
			t.Errorf("%v carries ANSI escapes: %q", args, h.stdout())
		}
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

// The context is read straight from current-context, so a name left behind by
// a deleted context still prints; only the namespace needs the context itself.
func TestCurrentPrintsADanglingContext(t *testing.T) {
	h := newHarness(t, testutil.Spec{Current: "gone", Contexts: []testutil.Ctx{{Name: "dev"}}})

	if err := h.run("current"); err != nil {
		t.Fatalf("current: %v", err)
	}
	if got := strings.TrimSpace(h.stdout()); got != "gone" {
		t.Errorf("stdout = %q, want gone", got)
	}
	if err := h.run("current", "-n"); err == nil {
		t.Error("current -n succeeded for a context that does not exist")
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

// The value of "kctx shell" is invisible — the prompt renders identically,
// because the session copy names the same context. Say so on the way in.
func TestShellPointsAtThePromptVariables(t *testing.T) {
	h := newHarness(t, defaultSpec())
	t.Setenv("SHELL", "/bin/bash")
	allowRun(t)

	if err := h.run("shell", "dev"); err != nil {
		t.Fatalf("shell: %v", err)
	}
	for _, want := range []string{EnvActive, "PS1"} {
		if !strings.Contains(h.stderr(), want) {
			t.Errorf("stderr = %q, want it to mention %s", h.stderr(), want)
		}
	}
}

// The snippet has to be in the syntax of the shell being entered; fish has no
// PS1 to add to.
func TestShellHintMatchesTheShell(t *testing.T) {
	h := newHarness(t, defaultSpec())
	t.Setenv("SHELL", "/opt/homebrew/bin/fish")
	allowRun(t)

	if err := h.run("shell", "dev"); err != nil {
		t.Fatalf("shell: %v", err)
	}
	if !strings.Contains(h.stderr(), "fish_prompt") {
		t.Errorf("stderr = %q, want the fish snippet", h.stderr())
	}
}

// Nesting already proved the point, and a hint that repeats is one people
// learn to skip.
func TestShellHintDoesNotRepeatWhenNesting(t *testing.T) {
	h := newHarness(t, defaultSpec())
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv(EnvShellID, "outer")
	t.Setenv("KUBE_CTX_DEPTH", "1")
	allowRun(t)

	if err := h.run("shell", "dev"); err != nil {
		t.Fatalf("shell: %v", err)
	}
	if strings.Contains(h.stderr(), "PS1") {
		t.Errorf("stderr = %q, want no repeated hint", h.stderr())
	}
}

// nsGuardConfirmConfig makes kube-ctx demand retyping before kube-system in a
// prod-* context, while leaving the context itself unguarded — the whole point
// of the second axis.
const nsGuardConfirmConfig = "guards:\n  - prefix: prod\n    namespaces: [kube-system]\n" +
	"    level: danger\n    confirm: true\n"

// The same reasoning that put the context guard on every route applies here:
// a namespace guard that only covered "kctx ns" would be bypassed by
// "kctx exec prod -n kube-system -- kubectl delete ...".
func TestNamespaceGuardCoversEveryRouteToANamespace(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"ns", []string{"ns", "kube-system"}},
		{"shell", []string{"shell", "prod", "-n", "kube-system"}},
		{"exec", []string{"exec", "prod", "-n", "kube-system", "--", "true"}},
		{"exec --all", []string{"exec", "-c", "prod", "-n", "kube-system", "--", "true"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, defaultSpec())
			writeUserConfig(t, nsGuardConfirmConfig)
			captureRunNoop(t)
			// "kctx ns" acts on the current context, so start in prod.
			if err := h.run("ctx", "prod"); err != nil {
				t.Fatalf("ctx prod: %v", err)
			}
			h.stdin("no\n")

			err := h.run(tt.args...)
			if code := ExitCode(err); code != ExitAborted {
				t.Errorf("ExitCode = %d, want %d (out %q, err %q)",
					code, ExitAborted, h.stdout(), h.stderr())
			}
			if got := h.config().Contexts["prod"].Namespace; got != "monitoring" {
				t.Errorf("namespace = %q; a declined guard must change nothing", got)
			}
		})
	}
}

func TestNamespaceGuardConfirmedSwitchApplies(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, nsGuardConfirmConfig)
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	h.stdin("kube-system\n")

	if err := h.run("ns", "kube-system"); err != nil {
		t.Fatalf("ns kube-system: %v", err)
	}
	if got := h.config().Contexts["prod"].Namespace; got != "kube-system" {
		t.Errorf("namespace = %q, want kube-system", got)
	}
}

// The prompt names the namespace, not the context: the namespace is what the
// rule is about and what has to be retyped.
func TestNamespaceGuardPromptAsksForTheNamespace(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, nsGuardConfirmConfig)
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	h.stdin("no\n")
	_ = h.run("ns", "kube-system")

	out := h.stdout()
	if !strings.Contains(out, `Type "kube-system" to continue`) {
		t.Errorf("prompt asked for the wrong phrase: %q", out)
	}
	if !strings.Contains(out, "kube-system in prod is classified danger") {
		t.Errorf("prompt did not name both halves: %q", out)
	}
}

// A namespace rule reaches a context switch only through the namespace it
// lands in. Here prod sits in "monitoring", so the rule about kube-system has
// nothing to say and the switch must not prompt — otherwise the second axis
// has quietly become the first.
func TestNamespaceGuardDoesNotBlockAnUnrelatedContextSwitch(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, nsGuardConfirmConfig)
	h.stdin("no\n")

	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("current = %q, want prod", got)
	}
	if strings.Contains(h.stdout(), "to continue") {
		t.Errorf("a namespace rule prompted on a context switch: %q", h.stdout())
	}
}

// Guarding only the -n flag would let a context whose own default is already
// the guarded namespace walk in unchecked.
func TestNamespaceGuardCoversTheContextsOwnNamespace(t *testing.T) {
	h := newHarness(t, testutil.Spec{
		Current:  "dev",
		Contexts: []testutil.Ctx{{Name: "dev"}, {Name: "prod", Namespace: "kube-system"}},
	})
	writeUserConfig(t, nsGuardConfirmConfig)
	captureRunNoop(t)
	h.stdin("no\n")

	err := h.run("exec", "prod", "--", "true")
	if code := ExitCode(err); code != ExitAborted {
		t.Errorf("ExitCode = %d, want %d; -n was never given, but the context "+
			"already pointed at the guarded namespace", code, ExitAborted)
	}
}

func TestUnguardedNamespaceIsUntouched(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, nsGuardConfirmConfig)
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}

	if err := h.run("ns", "payments"); err != nil {
		t.Fatalf("ns payments: %v", err)
	}
	if got := h.config().Contexts["prod"].Namespace; got != "payments" {
		t.Errorf("namespace = %q, want payments", got)
	}
	if strings.Contains(h.stdout(), "to continue") {
		t.Errorf("an unguarded namespace prompted: %q", h.stdout())
	}
}

// nsGuardBadgeConfig labels without blocking, the way the built-in context
// rules do.
const nsGuardBadgeConfig = "guards:\n  - namespaces: [kube-system]\n    level: danger\n"

func TestNamespaceBadgeOnSwitch(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, nsGuardBadgeConfig)
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}

	if err := h.run("ns", "kube-system"); err != nil {
		t.Fatalf("ns kube-system: %v", err)
	}
	// The badge sits next to the namespace it describes, not at the end of the
	// line where the context's own badge goes.
	if !strings.Contains(h.stdout(), "Namespace set to kube-system  DANGER in context prod") {
		t.Errorf("stdout = %q", h.stdout())
	}
}

// A guarded namespace under an unguarded context still has to announce itself,
// or exec would run there in silence.
func TestExecAnnouncesAGuardedNamespace(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, nsGuardBadgeConfig)
	allowRun(t)

	if err := h.run("exec", "dev", "-n", "kube-system", "--", "true"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if !strings.Contains(h.stderr(), "Running against dev, namespace kube-system  DANGER") {
		t.Errorf("stderr = %q", h.stderr())
	}
}

// An unremarkable namespace adds no noise to the line.
func TestExecDoesNotNameAnUnguardedNamespace(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardConfirmConfig)
	allowRun(t)
	h.stdin("prod\n")

	if err := h.run("exec", "prod", "--", "true"); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if strings.Contains(h.stderr(), "namespace") {
		t.Errorf("stderr named an unguarded namespace: %q", h.stderr())
	}
}

// The picker is where a namespace is most easily mis-selected, so the badge
// has to reach the list — and the guard still runs on what comes back out.
func TestNamespacePickerShowsTheBadgeAndStillGuards(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, nsGuardConfirmConfig)
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	seedNamespaceCache(t, "prod", "monitoring", "kube-system")
	frames := scriptPicker(t, "kube\r")
	h.stdin("no\n")

	err := h.run("ns")
	if code := ExitCode(err); code != ExitAborted {
		t.Errorf("ExitCode = %d, want %d; the picker must not be a way past the guard",
			code, ExitAborted)
	}
	if !strings.Contains(frames.String(), "DANGER") {
		t.Errorf("the picker did not badge the guarded namespace: %q", frames.String())
	}
	if !strings.Contains(frames.String(), "current") {
		t.Errorf("the picker lost the current-namespace marker: %q", frames.String())
	}
}

// bind --apply refuses a confirm-guarded context so that a cd is never a
// prompt. The namespace axis has to be refused for the same reason, or the
// binding walks the shell into kube-system without asking.
func TestBindApplyRefusesANamespaceGuardedContext(t *testing.T) {
	h := newHarness(t, testutil.Spec{
		Current:  "dev",
		Contexts: []testutil.Ctx{{Name: "dev"}, {Name: "prod", Namespace: "kube-system"}},
	})
	writeUserConfig(t, nsGuardConfirmConfig)

	dir := t.TempDir()
	if err := h.run("bind", "prod", "--path", dir); err != nil {
		t.Fatalf("bind: %v", err)
	}
	t.Setenv(shellenv.EnvBound, "")
	if err := h.run("bind", "--apply", "--path", dir); err != nil {
		t.Fatalf("bind --apply: %v", err)
	}

	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q; a cd must not enter a namespace-guarded context", got)
	}
	if !strings.Contains(h.stderr(), "is guarded") {
		t.Errorf("stderr = %q, want it to say why the binding was not followed", h.stderr())
	}
}

// Switching runs nothing, but everything typed afterwards runs in whatever the
// switch left you standing in — so it is a route to the namespace like any
// other, and the most travelled one.
func TestNamespaceGuardCoversTheContextSwitch(t *testing.T) {
	h := newHarness(t, testutil.Spec{
		Current:  "dev",
		Contexts: []testutil.Ctx{{Name: "dev"}, {Name: "prod", Namespace: "kube-system"}},
	})
	writeUserConfig(t, nsGuardConfirmConfig)
	h.stdin("no\n")

	err := h.run("ctx", "prod")
	if code := ExitCode(err); code != ExitAborted {
		t.Errorf("ExitCode = %d, want %d", code, ExitAborted)
	}
	if got := h.config().CurrentContext; got != "dev" {
		t.Errorf("current = %q; a declined guard must change nothing", got)
	}
}

// Both answers arrive on one pipe. A reader per prompt read ahead, swallowed
// the second, and the second prompt read EOF as a decline.
func TestBothGuardsPromptSeparatelyOnASwitch(t *testing.T) {
	h := newHarness(t, testutil.Spec{
		Current:  "dev",
		Contexts: []testutil.Ctx{{Name: "dev"}, {Name: "prod", Namespace: "kube-system"}},
	})
	writeUserConfig(t, guardConfirmConfig+"  - namespaces: [kube-system]\n"+
		"    level: danger\n    confirm: true\n")

	// One answer per prompt, context first.
	h.stdin("prod\nkube-system\n")
	if err := h.run("ctx", "prod"); err != nil {
		t.Fatalf("ctx prod: %v", err)
	}
	if got := h.config().CurrentContext; got != "prod" {
		t.Errorf("current = %q, want prod", got)
	}

	out := h.stdout()
	if !strings.Contains(out, `Type "prod" to continue`) {
		t.Errorf("the context prompt did not run: %q", out)
	}
	if !strings.Contains(out, `Type "kube-system" to continue`) {
		t.Errorf("the namespace prompt did not run: %q", out)
	}
}

// guardExactConfig guards "prod" by name rather than by pattern.
const guardExactConfig = "guards:\n  - contexts: [prod]\n    level: danger\n    confirm: true\n"

// A guard naming the context exactly would otherwise stay behind on the old
// name, and the renamed production context would switch without a prompt.
func TestRenameCarriesAnExactNameGuard(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardExactConfig)

	if err := h.run("rename", "prod", "live"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !strings.Contains(h.stdout(), "Guard rules naming prod now name live") {
		t.Errorf("stdout = %q, want the carried guard reported", h.stdout())
	}
	if strings.Contains(h.stderr(), "warning") {
		t.Errorf("stderr = %q; a carried guard is not a lost one", h.stderr())
	}
	userCfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := userCfg.Guards[0].Contexts; len(got) != 1 || got[0] != "live" {
		t.Errorf("guard contexts = %v, want [live]", got)
	}

	h.stdin("no\n")
	if code := ExitCode(h.run("ctx", "live")); code != ExitAborted {
		t.Errorf("ctx live: ExitCode = %d, want %d; the guard did not follow the rename", code, ExitAborted)
	}
}

func TestRenameThatFailsToSaveLeavesTheContextGuarded(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardExactConfig)
	// A state dir that is a file makes the kubeconfig backup, and so the
	// kubeconfig write, fail after the guards have been written once.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", blocker)

	if err := h.run("rename", "prod", "live"); err == nil {
		t.Fatal("rename succeeded; the test did not make the write fail")
	}
	if _, ok := h.config().Contexts["prod"]; !ok {
		t.Fatal("the kubeconfig changed despite the failed write")
	}
	userCfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	classifier, err := guard.New(userCfg.Guards)
	if err != nil {
		t.Fatalf("guard.New: %v", err)
	}
	if !classifier.Classify("prod").Confirm {
		t.Errorf("guards = %+v; prod is no longer guarded after a failed rename", userCfg.Guards)
	}
	// Both names: the carry reached the file before the kubeconfig write.
	if got := userCfg.Guards[0].Contexts; len(got) != 2 || got[1] != "live" {
		t.Errorf("guard contexts = %v, want [prod live]", got)
	}
}

// The other half of the ordering: guards that cannot be written stop the
// rename before the kubeconfig changes, or the context would end up unguarded.
func TestRenameStopsBeforeTheKubeconfigWhenGuardsCannotBeSaved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file modes")
	}
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardExactConfig)
	// The directory, not the file: a save replaces config.yaml by renaming a
	// new file over it, which a read-only file does not stop.
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "kube-ctx")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := h.run("rename", "prod", "live"); err == nil {
		t.Fatal("rename succeeded; the config write did not fail")
	}
	if _, ok := h.config().Contexts["prod"]; !ok {
		t.Error("the kubeconfig was renamed although the guards could not follow")
	}
}

func TestRenameCarriesTheGuardOfAnAliasedContext(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardExactConfig+"aliases:\n  p: prod\n")

	if err := h.run("rename", "p", "live"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	userCfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := userCfg.Guards[0].Contexts; len(got) != 1 || got[0] != "live" {
		t.Errorf("guard contexts = %v, want [live]", got)
	}
}

// With no config file the defaults are in effect, and a rename must not freeze
// them into a file the user never asked for.
func TestRenameWithoutAConfigFileWritesNone(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("rename", "prod", "live"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !strings.Contains(h.stderr(), "kctx guard add live\n") {
		t.Errorf("stderr = %q, want the default pattern's loss reported", h.stderr())
	}
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "kube-ctx", "config.yaml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("stat config.yaml: %v; want it not created", err)
	}
}

// The restore rule is prepended and wins first-match, so restoring only the
// lost confirm at the old level would lower a context that is now danger.
func TestRenameRestoreLineNeverLowersTheLevel(t *testing.T) {
	h := newHarness(t, testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}, {Name: "prod-a"}}})
	writeUserConfig(t, "guards:\n"+
		"  - suffix: -a\n    level: warn\n    confirm: true\n"+
		"  - match: prod\n    level: danger\n")

	if err := h.run("rename", "prod-a", "prod-b"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if !strings.Contains(h.stderr(), "kctx guard add prod-b --confirm\n") {
		t.Errorf("stderr = %q, want a danger + confirm restore line", h.stderr())
	}
}

func TestRestoreCommandQuotesAndKeepsACustomLabel(t *testing.T) {
	got := restoreCommand("my ctx", "kube-system",
		guard.Verdict{Level: config.LevelWarn, Confirm: true, Label: "PROD EU"})
	want := "kctx guard add 'my ctx' -n kube-system --level warn --confirm --label 'PROD EU'"
	if got != want {
		t.Errorf("restoreCommand = %q\nwant           %q", got, want)
	}
	if got := restoreCommand("prod", "", guard.Verdict{Level: config.LevelDanger, Label: "DANGER"}); got != "kctx guard add prod" {
		t.Errorf("a default label must not be spelled out: %q", got)
	}
}

func TestShellWord(t *testing.T) {
	tests := map[string]string{
		"arn:aws:eks:ap-northeast-2:123456789012:cluster/prod": "arn:aws:eks:ap-northeast-2:123456789012:cluster/prod",
		"gke_proj_zone_name": "gke_proj_zone_name",
		"it's":               `'it'\''s'`,
		"a b":                "'a b'",
		"":                   "''",
	}
	for in, want := range tests {
		if got := shellWord(in); got != want {
			t.Errorf("shellWord(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenameWarnsWhenAPatternGuardStopsMatching(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardConfirmConfig)

	if err := h.run("rename", "prod", "live"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	for _, want := range []string{
		`warning: prod was danger, confirm (rule "prod"); as live it is safe`,
		"kctx guard add live --confirm",
	} {
		if !strings.Contains(h.stderr(), want) {
			t.Errorf("stderr = %q, want %q", h.stderr(), want)
		}
	}
	if _, ok := h.config().Contexts["live"]; !ok {
		t.Error("the warning must not undo the rename")
	}
}

func TestRenameWarnsWhenANamespaceGuardStopsMatching(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, "guards:\n  - prefix: prod\n    namespaces: [kube-system]\n    level: warn\n    confirm: true\n")

	if err := h.run("rename", "prod", "live"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	for _, want := range []string{
		"warning: kube-system in prod was warn, confirm",
		"kctx guard add live -n kube-system --level warn --confirm",
	} {
		if !strings.Contains(h.stderr(), want) {
			t.Errorf("stderr = %q, want %q", h.stderr(), want)
		}
	}
}

func TestRenameOfAnUnguardedContextSaysNothingAboutGuards(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardExactConfig)

	if err := h.run("rename", "dev", "development"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if strings.Contains(h.stdout(), "Guard") || h.stderr() != "" {
		t.Errorf("stdout = %q, stderr = %q", h.stdout(), h.stderr())
	}
}
