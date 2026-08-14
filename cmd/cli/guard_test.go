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
