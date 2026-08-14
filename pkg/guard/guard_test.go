package guard

import (
	"strings"
	"testing"

	"github.com/somaz94/kube-ctx/pkg/config"
)

func TestDefaultGuardsClassifyCommonNames(t *testing.T) {
	c, err := New(config.DefaultGuards())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		name string
		want config.Level
	}{
		{"prod-eks-apne2", config.LevelDanger},
		{"eks-prod", config.LevelDanger},
		{"my_prd_cluster", config.LevelDanger},
		{"production", config.LevelDanger},
		{"staging-gke", config.LevelWarn},
		{"uat", config.LevelWarn},
		{"dev", config.LevelSafe},
		{"kind-local", config.LevelSafe},
		// "reproducible" contains "prod" but not as a word — it must not trip
		// the production guard.
		{"reproducible-lab", config.LevelSafe},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Classify(tt.name).Level; got != tt.want {
				t.Errorf("Classify(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestDefaultGuardsDoNotBlock(t *testing.T) {
	c, err := New(config.DefaultGuards())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.Classify("prod-eks").Confirm {
		t.Error("the built-in guards must not require confirmation")
	}
}

func TestFirstMatchingRuleWins(t *testing.T) {
	c, err := New([]config.Guard{
		{Match: "^prod-eks$", Level: config.LevelWarn, Label: "special"},
		{Match: "prod", Level: config.LevelDanger, Confirm: true},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	v := c.Classify("prod-eks")
	if v.Level != config.LevelWarn {
		t.Errorf("Level = %v, want warn from the first rule", v.Level)
	}
	if v.Label != "special" {
		t.Errorf("Label = %q, want special", v.Label)
	}
	if v.Confirm {
		t.Error("Confirm should come from the first matching rule only")
	}
	if v.Rule != "^prod-eks$" {
		t.Errorf("Rule = %q", v.Rule)
	}

	if v := c.Classify("prod-gke"); !v.Dangerous() || !v.Confirm {
		t.Errorf("second rule verdict = %+v", v)
	}
}

func TestUnmatchedIsSafe(t *testing.T) {
	c, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	v := c.Classify("anything")
	if v.Level != config.LevelSafe || v.Dangerous() || v.Confirm || v.Label != "" {
		t.Errorf("verdict = %+v, want an empty safe verdict", v)
	}
}

func TestInvalidPattern(t *testing.T) {
	if _, err := New([]config.Guard{{Match: "([unclosed", Level: config.LevelDanger}}); err == nil {
		t.Error("expected a compile error")
	}
}

func TestLevelNormalization(t *testing.T) {
	c, err := New([]config.Guard{
		{Match: "^a$", Level: "DANGER"},
		{Match: "^b$", Level: "Warn"},
		{Match: "^c$", Level: "nonsense"},
		{Match: "^d$"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := map[string]config.Level{
		"a": config.LevelDanger,
		"b": config.LevelWarn,
		"c": config.LevelSafe, // a typo must downgrade, never promote
		"d": config.LevelSafe,
	}
	for name, want := range tests {
		if got := c.Classify(name).Level; got != want {
			t.Errorf("Classify(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestDefaultLabelsAndStyles(t *testing.T) {
	c, err := New([]config.Guard{
		{Match: "^a$", Level: config.LevelDanger},
		{Match: "^b$", Level: config.LevelWarn},
		{Match: "^c$", Level: config.LevelSafe},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tests := []struct {
		name        string
		label, styl string
	}{
		{"a", "DANGER", "danger"},
		{"b", "WARN", "warn"},
		{"c", "", ""},
	}
	for _, tt := range tests {
		v := c.Classify(tt.name)
		if v.Label != tt.label {
			t.Errorf("Classify(%q).Label = %q, want %q", tt.name, v.Label, tt.label)
		}
		if v.Style() != tt.styl {
			t.Errorf("Classify(%q).Style() = %q, want %q", tt.name, v.Style(), tt.styl)
		}
	}
}

// A rule may carry contexts, prefix or suffix instead of a regex — the names
// that most need guarding are the ones no pattern over "prod" will find.
func TestNonRegexMatchers(t *testing.T) {
	tests := []struct {
		name    string
		guard   config.Guard
		matches []string
		misses  []string
	}{
		{
			name:    "exact contexts",
			guard:   config.Guard{Contexts: []string{"cluster-7", "acme-main"}, Level: config.LevelDanger},
			matches: []string{"cluster-7", "acme-main"},
			misses:  []string{"cluster-70", "acme", "dev"},
		},
		{
			name:    "prefix",
			guard:   config.Guard{Prefix: "acme-", Level: config.LevelDanger},
			matches: []string{"acme-live", "acme-"},
			misses:  []string{"my-acme-live", "acme"},
		},
		{
			name:    "suffix",
			guard:   config.Guard{Suffix: "-live", Level: config.LevelDanger},
			matches: []string{"acme-live", "-live"},
			misses:  []string{"live", "acme-live-2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New([]config.Guard{tt.guard})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			for _, name := range tt.matches {
				if got := c.Classify(name); got.Level != config.LevelDanger {
					t.Errorf("Classify(%q) = %q, want danger", name, got.Level)
				}
			}
			for _, name := range tt.misses {
				if got := c.Classify(name); got.Level != config.LevelSafe {
					t.Errorf("Classify(%q) = %q, want safe", name, got.Level)
				}
			}
		})
	}
}

// A rule with no matcher must be rejected rather than matching everything: the
// failure mode of the latter is a whole kubeconfig silently classified danger.
func TestRuleWithoutAMatcherIsRejected(t *testing.T) {
	_, err := New([]config.Guard{{Level: config.LevelDanger}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "no matcher") {
		t.Errorf("error = %v", err)
	}
}

// Two matchers on one rule is a typo. Honouring either one silently would
// leave the user believing a context is guarded when it is not.
func TestRuleWithTwoMatchersIsRejected(t *testing.T) {
	_, err := New([]config.Guard{{Prefix: "a", Suffix: "b", Level: config.LevelDanger}})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "more than one matcher") {
		t.Errorf("error = %v", err)
	}
}

func TestValidateHasNoPositionalPrefix(t *testing.T) {
	err := Validate(config.Guard{Match: "[bad(", Level: config.LevelDanger})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.HasPrefix(err.Error(), "guard ") {
		t.Errorf("Validate should not number the rule: %v", err)
	}
}
