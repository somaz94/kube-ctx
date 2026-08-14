// Package guard classifies contexts by how much damage a mistake would do.
//
// The rules are regular expressions over the context name, because that is the
// only thing every cluster has in common — an EKS ARN, a kind cluster and a
// kubeadm context share no label, annotation or field that says "production".
package guard

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/somaz94/kube-ctx/pkg/config"
)

// Verdict is the classification of one context.
type Verdict struct {
	// Level is safe, warn, or danger.
	Level config.Level
	// Confirm reports whether switching requires retyping the context name.
	Confirm bool
	// Label is the short badge to show next to the context.
	Label string
	// Rule is the pattern that matched, so the user can see why.
	Rule string
}

// Dangerous reports whether the context is classified as production.
func (v Verdict) Dangerous() bool { return v.Level == config.LevelDanger }

// Style maps the verdict onto the badge styles the picker understands.
func (v Verdict) Style() string {
	switch v.Level {
	case config.LevelDanger:
		return "danger"
	case config.LevelWarn:
		return "warn"
	default:
		return ""
	}
}

// Classifier applies the compiled guard rules.
type Classifier struct {
	rules []rule
}

type rule struct {
	matches func(string) bool
	level   config.Level
	confirm bool
	label   string
	source  string
}

// New compiles the guard rules. The first rule that matches a name wins, so
// order in the config file is meaningful.
func New(guards []config.Guard) (*Classifier, error) {
	c := &Classifier{}
	for i, g := range guards {
		matches, err := compile(g)
		if err != nil {
			return nil, fmt.Errorf("guard %d: %w", i+1, err)
		}
		c.rules = append(c.rules, rule{
			matches: matches,
			level:   normalizeLevel(g.Level),
			confirm: g.Confirm,
			label:   labelFor(g),
			source:  g.Describe(),
		})
	}
	return c, nil
}

// Validate reports whether a single rule is usable, without the positional
// prefix New adds — the caller writing the rule already knows which one it is.
func Validate(g config.Guard) error {
	_, err := compile(g)
	return err
}

// compile turns one rule's matcher into a predicate.
//
// A rule with no matcher is rejected rather than treated as match-everything:
// the failure mode of the latter is every context in the kubeconfig suddenly
// classified danger, from a rule the user thought was incomplete.
func compile(g config.Guard) (func(string) bool, error) {
	set := g.Matchers()
	switch {
	case len(set) == 0:
		return nil, fmt.Errorf("has no matcher; set one of match, contexts, prefix or suffix")
	case len(set) > 1:
		return nil, fmt.Errorf("sets more than one matcher (%s); a rule may only have one", strings.Join(set, ", "))
	}

	switch set[0] {
	case "contexts":
		allowed := make(map[string]struct{}, len(g.Contexts))
		for _, name := range g.Contexts {
			allowed[name] = struct{}{}
		}
		return func(name string) bool { _, ok := allowed[name]; return ok }, nil
	case "prefix":
		return func(name string) bool { return strings.HasPrefix(name, g.Prefix) }, nil
	case "suffix":
		return func(name string) bool { return strings.HasSuffix(name, g.Suffix) }, nil
	default:
		re, err := regexp.Compile(g.Match)
		if err != nil {
			return nil, fmt.Errorf("invalid match pattern %q: %w", g.Match, err)
		}
		return re.MatchString, nil
	}
}

// Classify returns the verdict for a context name.
func (c *Classifier) Classify(name string) Verdict {
	for _, r := range c.rules {
		if r.matches(name) {
			return Verdict{Level: r.level, Confirm: r.confirm, Label: r.label, Rule: r.source}
		}
	}
	return Verdict{Level: config.LevelSafe}
}

// normalizeLevel defaults an unset or unknown level to safe, so a typo in the
// config downgrades rather than silently promoting a context to dangerous.
func normalizeLevel(level config.Level) config.Level {
	switch config.Level(strings.ToLower(string(level))) {
	case config.LevelDanger:
		return config.LevelDanger
	case config.LevelWarn:
		return config.LevelWarn
	default:
		return config.LevelSafe
	}
}

// labelFor picks the badge text: the rule's own label, else the level name.
func labelFor(g config.Guard) string {
	if g.Label != "" {
		return g.Label
	}
	switch normalizeLevel(g.Level) {
	case config.LevelDanger:
		return "DANGER"
	case config.LevelWarn:
		return "WARN"
	default:
		return ""
	}
}
