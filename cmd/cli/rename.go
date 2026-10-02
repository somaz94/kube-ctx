package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/config"
	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/guard"
	"github.com/somaz94/kube-ctx/pkg/kubeconfig"
)

// newRenameCmd renames a context.
func newRenameCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename a context",
		Long: "Rename a context.\n\n" +
			"Passing \".\" as <old> renames the current context. current-context is\n" +
			"carried over automatically, so the rename never leaves the kubeconfig\n" +
			"pointing at a name that no longer exists.\n\n" +
			"Guard rules that list <old> by name are moved to <new>. A rule matching by\n" +
			"pattern cannot follow the rename; if one stops covering the context, the\n" +
			"rename says so and prints the rule that would restore it.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeContexts(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRename(a, args[0], args[1])
		},
	}
}

// runRename applies the rename and persists it.
func runRename(a *app, oldName, newName string) error {
	if err := guardSessionScoped("rename"); err != nil {
		return err
	}

	loader := a.loader()
	cfg, err := loader.Load()
	if err != nil {
		return err
	}

	oldName, err = resolveContext(a, cfg, oldName)
	if err != nil {
		return err
	}
	if err := contexts.Rename(cfg, oldName, newName); err != nil {
		return err
	}

	userCfg, err := a.userConfig()
	if err != nil {
		return err
	}
	before, err := guard.New(userCfg.Guards)
	if err != nil {
		return err
	}

	// The new name joins the guards before the kubeconfig is written and the
	// old one leaves after, so no failure in between unguards the context.
	carried := userCfg.CarryGuards(oldName, newName)
	if carried > 0 {
		if err := userCfg.Save(); err != nil {
			return err
		}
	}
	if err := loader.Save(cfg, kubeconfig.WithBackup()); err != nil {
		return err
	}

	pal := a.palette()
	if _, err := fmt.Fprintf(a.out, "Renamed context %s to %s.\n", pal.Dim(oldName), pal.Bold(newName)); err != nil {
		return err
	}
	if carried > 0 {
		userCfg.DropCarriedName(oldName, newName)
		if err := userCfg.Save(); err != nil {
			fmt.Fprintf(a.errOut, "warning: %v; the guard rules still list %s as well\n", err, oldName)
		} else {
			fmt.Fprintf(a.out, "Guard rules naming %s now name %s.\n", oldName, pal.Bold(newName))
		}
	}

	after, err := guard.New(userCfg.Guards)
	if err != nil {
		return err
	}
	warnLostGuards(a, before, after, oldName, newName)
	return nil
}

// warnLostGuards reports every verdict the rename weakened. It warns rather than
// refuses: renaming something out of production can be deliberate.
func warnLostGuards(a *app, before, after *guard.Classifier, oldName, newName string) {
	was, now := before.Classify(oldName), after.Classify(newName)
	if now.WeakerThan(was) {
		fmt.Fprintf(a.errOut, "warning: %s was %s (rule %q); as %s it is %s. To guard it again:\n  %s\n",
			oldName, describeVerdict(was), was.Rule, newName, describeVerdict(now),
			restoreCommand(newName, "", was.Join(now)))
	}
	for _, ns := range before.Namespaces() {
		was, now := before.ClassifyNamespace(oldName, ns), after.ClassifyNamespace(newName, ns)
		if now.WeakerThan(was) {
			fmt.Fprintf(a.errOut, "warning: %s in %s was %s (rule %q); in %s it is %s. To guard it again:\n  %s\n",
				ns, oldName, describeVerdict(was), was.Rule, newName, describeVerdict(now),
				restoreCommand(newName, ns, was.Join(now)))
		}
	}
}

// describeVerdict renders a verdict as "danger, confirm".
func describeVerdict(v guard.Verdict) string {
	if v.Confirm {
		return string(v.Level) + ", confirm"
	}
	return string(v.Level)
}

// restoreCommand renders the guard add line that gives name verdict v. The rule
// it adds is prepended, so v has to be at least the verdict name has now, or
// restoring one axis would lower the other.
func restoreCommand(name, namespace string, v guard.Verdict) string {
	parts := []string{"kctx guard add", shellWord(name)}
	if namespace != "" {
		parts = append(parts, "-n", shellWord(namespace))
	}
	if v.Level != config.LevelDanger {
		parts = append(parts, "--level", string(v.Level))
	}
	if v.Confirm {
		parts = append(parts, "--confirm")
	}
	if v.Label != "" && v.Label != strings.ToUpper(string(v.Level)) {
		parts = append(parts, "--label", shellWord(v.Label))
	}
	return strings.Join(parts, " ")
}

// shellWord single-quotes s unless every character is safe bare, which is the
// common case for context names. The quoting reads the same in bash, zsh and
// fish.
func shellWord(s string) string {
	safe := s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			strings.ContainsRune("-_./:@%+=,", r))
	})
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
