// Package guard classifies contexts by how much damage a mistake would do.
//
// The rules are regular expressions over the context name, because that is the
// only thing every cluster has in common — an EKS ARN, a kind cluster and a
// kubeadm context share no label, annotation or field that says "production".
//
// A rule that lists namespaces classifies those instead, inside the contexts
// it matches. The two axes are kept as separate lists rather than one, so that
// "the first matching rule wins" stays answerable: interleaved, a namespace
// rule sitting above a context rule would look like it shadowed it.
package guard

import (
	"errors"
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
	rules   []rule
	nsRules []rule
}

type rule struct {
	matches func(string) bool
	// namespaces is the set this rule is narrowed to, nil on a context rule.
	namespaces map[string]struct{}
	level      config.Level
	confirm    bool
	label      string
	source     string
}

// New compiles the guard rules. The first rule that matches a name wins, so
// order in the config file is meaningful — within each axis: a namespace rule
// never competes with a context rule for the same verdict.
func New(guards []config.Guard) (*Classifier, error) {
	c := &Classifier{}
	for i, g := range guards {
		compiled, err := compileRule(g)
		if err != nil {
			return nil, fmt.Errorf("guard %d: %w", i+1, err)
		}
		if g.ScopesNamespaces() {
			c.nsRules = append(c.nsRules, compiled)
			continue
		}
		c.rules = append(c.rules, compiled)
	}
	return c, nil
}

// Validate reports whether a single rule is usable, without the positional
// prefix New adds — the caller writing the rule already knows which one it is.
func Validate(g config.Guard) error {
	_, err := compileRule(g)
	return err
}

// compileRule turns one config rule into the form Classify walks.
func compileRule(g config.Guard) (rule, error) {
	matches, err := compile(g)
	if err != nil {
		return rule{}, err
	}
	r := rule{
		matches: matches,
		level:   normalizeLevel(g.Level),
		confirm: g.Confirm,
		label:   labelFor(g),
		source:  g.Describe(),
	}
	if g.ScopesNamespaces() {
		r.namespaces = make(map[string]struct{}, len(g.Namespaces))
		for _, ns := range g.Namespaces {
			// Trimmed, not just checked: "-n 'kube-system, istio-system'"
			// reaches here with the space still attached, since pflag splits
			// the CSV without trimming. Keyed raw, that rule would look
			// accepted and never match — a safety feature failing open.
			ns = strings.TrimSpace(ns)
			if ns == "" {
				return rule{}, errors.New("lists an empty namespace; remove it or name the namespace")
			}
			r.namespaces[ns] = struct{}{}
		}
	}
	return r, nil
}

// compile turns one rule's context matcher into a predicate.
//
// A rule with no matcher is rejected rather than treated as match-everything:
// the failure mode of the latter is every context in the kubeconfig suddenly
// classified danger, from a rule the user thought was incomplete.
//
// A namespace rule is the exception, and only because that reasoning does not
// reach it. Its namespace list is already a matcher, so an omitted context
// matcher over-classifies nothing — it guards kube-system and nothing else,
// everywhere, which is the most obvious rule anyone writes here. Demanding
// match: '.*' to say it would put a barrier in front of the safety feature.
func compile(g config.Guard) (func(string) bool, error) {
	set := g.Matchers()
	switch {
	case len(set) == 0 && g.ScopesNamespaces():
		return func(string) bool { return true }, nil
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
			return r.verdict()
		}
	}
	return Verdict{Level: config.LevelSafe}
}

// ClassifyNamespace returns the verdict for a namespace as reached through one
// context. Both halves have to match: kube-system in a kind cluster is not the
// kube-system a rule about production means.
func (c *Classifier) ClassifyNamespace(ctxName, namespace string) Verdict {
	for _, r := range c.nsRules {
		if _, ok := r.namespaces[namespace]; !ok {
			continue
		}
		if r.matches(ctxName) {
			return r.verdict()
		}
	}
	return Verdict{Level: config.LevelSafe}
}

// verdict renders a matched rule as its answer.
func (r rule) verdict() Verdict {
	return Verdict{Level: r.level, Confirm: r.confirm, Label: r.label, Rule: r.source}
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
