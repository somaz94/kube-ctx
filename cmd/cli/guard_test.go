package cli

import (
	"strings"
	"testing"
)

// The point of "kctx guard add <name>" is the cluster whose name no pattern
// over "prod" will ever find.
func TestGuardAddNamesAContextDirectly(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("guard", "add", "prod", "--confirm", "--label", "PROD"); err != nil {
		t.Fatalf("guard add: %v", err)
	}
	if !strings.Contains(h.stdout(), "prod") {
		t.Errorf("stdout = %q", h.stdout())
	}

	if err := h.run("guard", "list"); err != nil {
		t.Fatalf("guard list: %v", err)
	}
	if !strings.Contains(h.stdout(), "CONFIRM") {
		t.Errorf("list did not render: %q", h.stdout())
	}
}

func TestGuardAddRejectsBadInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no matcher", []string{"guard", "add"}, "give a context name"},
		{"two matchers", []string{"guard", "add", "prod", "--prefix", "x"}, "only one of"},
		{"unknown context", []string{"guard", "add", "nosuchctx"}, "no context named"},
		{"bad level", []string{"guard", "add", "prod", "--level", "nope"}, "not one of"},
		{"bad regex", []string{"guard", "add", "--match", "[bad("}, "invalid match pattern"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, defaultSpec())
			err := h.run(tt.args...)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to mention %q", err, tt.want)
			}
		})
	}
}

// Numbering comes from "kctx guard list", so a position outside it has to say
// so rather than silently removing the wrong rule.
func TestGuardRemoveRejectsBadPosition(t *testing.T) {
	for _, arg := range []string{"0", "99", "abc"} {
		h := newHarness(t, defaultSpec())
		if err := h.run("guard", "remove", arg); err == nil {
			t.Errorf("guard remove %q succeeded", arg)
		}
	}
}

func TestGuardRemoveDeletesTheNamedRule(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("guard", "add", "prod", "--label", "MINE"); err != nil {
		t.Fatalf("guard add: %v", err)
	}
	if err := h.run("guard", "remove", "1"); err != nil {
		t.Fatalf("guard remove: %v", err)
	}
	if !strings.Contains(h.stdout(), "Removed guard") {
		t.Errorf("stdout = %q", h.stdout())
	}
}

// With nothing configured the built-in rules are shown, and said to be
// built-in — otherwise the list looks like a config the user wrote.
func TestGuardListSaysWhenRulesAreDefaults(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("guard", "list"); err != nil {
		t.Fatalf("guard list: %v", err)
	}
	if !strings.Contains(h.stderr(), "built-in defaults") {
		t.Errorf("stderr = %q", h.stderr())
	}
}

// The most obvious rule anyone writes here is "kube-system is dangerous,
// period" — it must not require a context matcher to say it.
func TestGuardAddNamespaceRuleWithoutAContextMatcher(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("guard", "add", "-n", "kube-system", "--confirm"); err != nil {
		t.Fatalf("guard add: %v", err)
	}
	if !strings.Contains(h.stdout(), "any context / kube-system") {
		t.Errorf("stdout = %q", h.stdout())
	}

	if err := h.run("guard", "list"); err != nil {
		t.Fatalf("guard list: %v", err)
	}
	if !strings.Contains(h.stdout(), "any context / kube-system") {
		t.Errorf("list did not show the rule's namespace half: %q", h.stdout())
	}
}

func TestGuardAddNamespaceRuleScopedToContexts(t *testing.T) {
	h := newHarness(t, defaultSpec())

	err := h.run("guard", "add", "--prefix", "prod", "-n", "kube-system,istio-system", "--confirm")
	if err != nil {
		t.Fatalf("guard add: %v", err)
	}
	if !strings.Contains(h.stdout(), "prod* / kube-system, istio-system") {
		t.Errorf("stdout = %q", h.stdout())
	}

	// The rule has to survive the round trip through the config file, or the
	// second axis exists only until the next command.
	if err := h.run("guard", "list", "-o", "json"); err != nil {
		t.Fatalf("guard list: %v", err)
	}
	if !strings.Contains(h.stdout(), `"namespaces"`) {
		t.Errorf("json dropped the namespaces: %q", h.stdout())
	}
}

// The comma-separated form the docs tell users to type reaches pflag's CSV
// split without trimming. Keyed raw, the second name would never match — the
// rule would look accepted and the guard would fail open.
func TestGuardAddTrimsNamespacesAfterAComma(t *testing.T) {
	h := newHarness(t, defaultSpec())

	err := h.run("guard", "add", "--prefix", "prod", "-n", "kube-system, istio-system", "--confirm")
	if err != nil {
		t.Fatalf("guard add: %v", err)
	}
	if !strings.Contains(h.stdout(), "prod* / kube-system, istio-system") {
		t.Errorf("stdout kept the stray space: %q", h.stdout())
	}

	if err := h.run("guard", "list", "-o", "json"); err != nil {
		t.Fatalf("guard list: %v", err)
	}
	if strings.Contains(h.stdout(), `" istio-system"`) {
		t.Errorf("the config recorded an untrimmed namespace: %q", h.stdout())
	}

	// The rule has to actually fire on the name behind the comma.
	h.stdin("no\n")
	if code := ExitCode(h.run("exec", "prod", "-n", "istio-system", "--", "true")); code != ExitAborted {
		t.Errorf("ExitCode = %d, want %d; the second namespace never matched", code, ExitAborted)
	}
}

func TestGuardAddRejectsBadNamespaceRules(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		// An empty -n is not a matcher, so the rule is simply incomplete.
		{"empty -n", []string{"guard", "add", "-n", ""}, "give a context name"},
		{"blank namespace", []string{"guard", "add", "-n", ","}, "empty namespace"},
		{"two matchers", []string{"guard", "add", "--prefix", "p", "--suffix", "s", "-n", "kube-system"}, "only one of"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, defaultSpec())
			err := h.run(tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}
