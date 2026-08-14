package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/config"
	"github.com/somaz94/kube-ctx/pkg/guard"
)

// newGuardCmd manages the rules that classify contexts.
func newGuardCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guard",
		Short: "Manage the rules that classify contexts",
		Long: "Manage the rules that classify contexts.\n\n" +
			"The built-in rules key off the name — prod, prd, production for danger,\n" +
			"stg, staging, uat for warn — and they only badge; switching is never\n" +
			"blocked until a rule sets confirm.\n\n" +
			"Names lie, though. The cluster that would hurt most to break is often\n" +
			"the one called cluster-7, and no pattern over \"prod\" will ever find it.\n" +
			"These commands name it directly, without hand-writing a regex.",
	}
	cmd.AddCommand(newGuardListCmd(a), newGuardAddCmd(a), newGuardRemoveCmd(a))
	return cmd
}

// newGuardListCmd shows the rules in effect.
func newGuardListCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the guard rules in effect",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGuardList(a)
		},
	}
}

// newGuardAddCmd appends a rule.
func newGuardAddCmd(a *app) *cobra.Command {
	var (
		level   string
		confirm bool
		label   string
		prefix  string
		suffix  string
		match   string
	)

	cmd := &cobra.Command{
		Use:   "add [context...]",
		Short: "Add a guard rule",
		Long: "Add a guard rule.\n\n" +
			"Context names given as arguments are matched exactly. Use --prefix,\n" +
			"--suffix or --match instead to cover a family of names. Exactly one of\n" +
			"the four forms may be used.\n\n" +
			"A new rule is prepended, so it wins over the built-in name patterns.",
		Example: "  kctx guard add cluster-7 --confirm\n" +
			"  kctx guard add --suffix -live --label PROD\n" +
			"  kctx guard add --prefix acme- --level warn\n" +
			"  kctx guard add --match '^eks-.*-main$' --confirm",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: completeContextList(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGuardAdd(a, args, guardSpec{
				level:   level,
				confirm: confirm,
				label:   label,
				prefix:  prefix,
				suffix:  suffix,
				match:   match,
			})
		},
	}
	cmd.Flags().StringVar(&level, "level", "danger", "safe, warn, or danger")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "require retyping the context name before switching")
	cmd.Flags().StringVar(&label, "label", "", "badge text to show instead of the level name")
	cmd.Flags().StringVar(&prefix, "prefix", "", "match context names starting with this")
	cmd.Flags().StringVar(&suffix, "suffix", "", "match context names ending with this")
	cmd.Flags().StringVar(&match, "match", "", "match context names against this regular expression")
	_ = cmd.RegisterFlagCompletionFunc("level", cobra.FixedCompletions(
		[]string{string(config.LevelSafe), string(config.LevelWarn), string(config.LevelDanger)},
		cobra.ShellCompDirectiveNoFileComp))
	return cmd
}

// newGuardRemoveCmd deletes a rule by its position in the list.
func newGuardRemoveCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "remove <n>",
		Aliases: []string{"rm"},
		Short:   "Remove the Nth guard rule, as numbered by \"kctx guard list\"",
		Args:    cobra.ExactArgs(1),
		// A bare index is exactly the kind of opaque argument completion is
		// for: offering "2  cluster-7 → danger" saves running list first.
		ValidArgsFunction: completeGuardPositions(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGuardRemove(a, args[0])
		},
	}
}

// completeGuardPositions offers each rule's number, described.
func completeGuardPositions(a *app) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		userCfg, err := a.userConfig()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		guards := userCfg.Guards
		if len(guards) == 0 {
			guards = config.DefaultGuards()
		}

		out := make([]string, 0, len(guards))
		for i, g := range guards {
			out = append(out, fmt.Sprintf("%d\t%s → %s", i+1, g.Describe(), g.Level))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// guardSpec is the rule described on the command line.
type guardSpec struct {
	level   string
	confirm bool
	label   string
	prefix  string
	suffix  string
	match   string
}

// runGuardList prints every rule, in the order they are consulted.
func runGuardList(a *app) error {
	userCfg, err := a.userConfig()
	if err != nil {
		return err
	}

	guards := userCfg.Guards
	if len(guards) == 0 {
		guards = config.DefaultGuards()
	}

	rows := make([][]string, 0, len(guards))
	for i, g := range guards {
		confirm := "no"
		if g.Confirm {
			confirm = "yes"
		}
		rows = append(rows, []string{
			fmt.Sprintf("%d", i+1),
			g.Describe(),
			string(g.Level),
			confirm,
		})
	}
	if err := renderOutput(a, []string{"#", "MATCH", "LEVEL", "CONFIRM"}, rows, guards); err != nil {
		return err
	}

	if !userCfg.HasGuards() && !a.jsonOutput() {
		_, err = fmt.Fprintf(a.errOut,
			"\nThese are the built-in defaults; no rules are configured yet.\n"+
				"Adding one writes them all to the config file, where they can be edited.\n")
		return err
	}
	return nil
}

// runGuardAdd validates the requested rule and prepends it.
func runGuardAdd(a *app, args []string, spec guardSpec) error {
	rule := config.Guard{
		Contexts: args,
		Prefix:   spec.prefix,
		Suffix:   spec.suffix,
		Match:    spec.match,
		Level:    config.Level(strings.ToLower(spec.level)),
		Confirm:  spec.confirm,
		Label:    spec.label,
	}

	switch set := rule.Matchers(); {
	case len(set) == 0:
		return fmt.Errorf("give a context name, or one of --prefix, --suffix or --match")
	case len(set) > 1:
		return fmt.Errorf("use only one of %s", strings.Join(set, ", "))
	}
	if !validLevel(rule.Level) {
		return fmt.Errorf("level %q is not one of safe, warn, danger", spec.level)
	}
	// Compiling now turns a bad regex into an error on the command that wrote
	// it, rather than on every later command that reads the config.
	if err := guard.Validate(rule); err != nil {
		return err
	}

	// An exact name that matches no context is almost always a typo, and a
	// guard rule that silently covers nothing is worse than no rule at all.
	// Resolving also expands aliases, so guarding the context you made an alias
	// for does not require spelling it out again.
	if len(rule.Contexts) > 0 {
		kubeCfg, err := a.loader().Load()
		if err != nil {
			return err
		}
		resolved, err := resolveContexts(a, kubeCfg, rule.Contexts)
		if err != nil {
			return err
		}
		rule.Contexts = resolved
	}

	userCfg, err := a.userConfig()
	if err != nil {
		return err
	}
	userCfg.AddGuard(rule)
	if err := userCfg.Save(); err != nil {
		return err
	}

	pal := a.palette()
	suffix := ""
	if rule.Confirm {
		suffix = " (confirm)"
	}
	_, err = fmt.Fprintf(a.out, "Guard added: %s → %s%s\n",
		pal.Bold(rule.Describe()), pal.Cyan(string(rule.Level)), suffix)
	return err
}

// runGuardRemove deletes the rule at the given 1-based position.
func runGuardRemove(a *app, arg string) error {
	userCfg, err := a.userConfig()
	if err != nil {
		return err
	}

	removed, err := userCfg.RemoveGuard(arg)
	if err != nil {
		return err
	}
	if err := userCfg.Save(); err != nil {
		return err
	}
	_, err = fmt.Fprintf(a.out, "Removed guard %s.\n", a.palette().Bold(removed.Describe()))
	return err
}

// validLevel reports whether the level is one kube-ctx understands.
//
// Checked here rather than left to the classifier, which deliberately
// downgrades anything unknown to safe: silently writing a rule that guards
// nothing is exactly the outcome this command exists to prevent.
func validLevel(level config.Level) bool {
	switch level {
	case config.LevelSafe, config.LevelWarn, config.LevelDanger:
		return true
	}
	return false
}
