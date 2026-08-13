package guard

import (
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
