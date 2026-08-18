package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/guard"
	"github.com/somaz94/kube-ctx/pkg/picker"
)

// newCtxCmd switches the current context.
func newCtxCmd(a *app) *cobra.Command {
	var back int

	cmd := &cobra.Command{
		Use:     "ctx [context|-|-N]",
		Aliases: []string{"c"},
		Short:   "Switch the current context",
		Long: "Switch the current context.\n\n" +
			"With no argument an interactive picker opens; with \"-\" or \"-N\" the\n" +
			"previous (or Nth previous) context is restored. Aliases defined in the\n" +
			"kube-ctx config file are accepted anywhere a context name is.\n\n" +
			"Contexts matching a guard rule are badged (DANGER for production, WARN\n" +
			"for staging). Badging is all the defaults do — switching is never\n" +
			"blocked. To make a production switch demand that you retype the context\n" +
			"name, set confirm on the rule in ~/.config/kube-ctx/config.yaml\n" +
			"($XDG_CONFIG_HOME/kube-ctx/config.yaml):\n\n" +
			"  guards:\n" +
			"    - match: '(^|[-_.])(prod|prd|production)([-_.]|$)'\n" +
			"      level: danger\n" +
			"      confirm: true\n\n" +
			"-y skips the prompt, for scripts.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCtx(a, args, back)
		},
		ValidArgsFunction: completeContexts(a),
	}
	cmd.Flags().IntVarP(&back, "back", "b", 0, "switch to the Nth previous context (same as -N)")
	return cmd
}

// runCtx resolves the requested context and switches to it.
func runCtx(a *app, args []string, back int) error {
	loader := a.loader()
	cfg, err := loader.Load()
	if err != nil {
		return err
	}

	target, err := resolveContextArg(a, cfg, args, back)
	switch {
	case errors.Is(err, picker.ErrAborted):
		return nil // the user changed their mind; nothing to report
	case errors.Is(err, errPickerUnavailable):
		// No terminal to prompt on: print the list, the way kubectx does when
		// it cannot go interactive.
		return printContextNames(a, cfg)
	case err != nil:
		return err
	}
	return switchContext(a, cfg, target)
}

// resolveContextArg turns the command line into a context name. It returns an
// empty string when the user gave no target and one must be picked
// interactively.
func resolveContextArg(a *app, cfg *clientcmdapi.Config, args []string, back int) (string, error) {
	if back = historyRef(args, back); back > 0 {
		history, err := contexts.NewHistory(historyScope())
		if err != nil {
			return "", err
		}
		return history.Lookup(back)
	}

	if len(args) == 0 {
		return pickContext(a, cfg)
	}
	return resolveContext(a, cfg, args[0])
}

// switchContext points current-context at target, records the previous one, and
// persists the change.
func switchContext(a *app, cfg *clientcmdapi.Config, target string) error {
	if !contexts.Exists(cfg, target) {
		return fmt.Errorf("no context named %q", target)
	}
	if err := requireGuardConfirmation(a, target); err != nil {
		return err
	}
	// The namespace the switch lands in counts as a route to it: nothing runs
	// here, but everything the user types next runs in whatever this leaves
	// them standing in. Skipping it left the most-travelled path — switch, then
	// a bare kubectl — as the one way past a namespace guard.
	namespace, err := contexts.Namespace(cfg, target)
	if err != nil {
		return err
	}
	if err := requireNamespaceGuardConfirmation(a, target, namespace); err != nil {
		return err
	}

	previous, err := contexts.Switch(cfg, target)
	if err != nil {
		return err
	}
	if err := saveSwitch(a, cfg, target); err != nil {
		return err
	}

	if previous != target {
		history, err := contexts.NewHistory(historyScope())
		if err != nil {
			return err
		}
		if err := history.Push(previous); err != nil {
			return err
		}
	}

	pal := a.palette()
	_, err = fmt.Fprintf(a.out, "Switched to context %s (namespace %s%s).%s\n",
		pal.Bold(target), pal.Cyan(namespace),
		namespaceGuardSuffix(a, target, namespace), guardSuffix(a, target))
	return err
}

// saveSwitch persists a switch, shell-locally when the hook is installed and
// globally otherwise.
func saveSwitch(a *app, cfg *clientcmdapi.Config, target string) error {
	shellLocal, err := startShellSession(a, cfg, target)
	if err != nil {
		return err
	}
	if shellLocal {
		// The change lives in this shell's private copy; the global kubeconfig
		// is deliberately left exactly as it was.
		return nil
	}
	return a.loader().Save(cfg)
}

// requireGuardConfirmation runs the guard prompt, if the rule asks for one,
// before anything is allowed to reach the context.
//
// Every route to a cluster goes through here — switching, opening a subshell,
// and running a single command. A guard that only covered "kctx ctx" would be
// bypassed by "kctx exec prod -- kubectl delete ...", which is the more
// dangerous of the two.
func requireGuardConfirmation(a *app, target string) error {
	ok, err := confirmGuard(a, target)
	return enforceGuard(a, ok, err)
}

// requireNamespaceGuardConfirmation runs the same prompt for a namespace rule,
// before anything is allowed to reach the namespace through this context.
//
// It sits beside the context check rather than inside it because the two are
// asked at different moments: the context is known as soon as a command names
// one, while the namespace is only settled once the -n flag and the context's
// own default have been reconciled.
func requireNamespaceGuardConfirmation(a *app, target, namespace string) error {
	ok, err := confirmNamespaceGuard(a, target, namespace)
	return enforceGuard(a, ok, err)
}

// enforceGuard turns a confirmation answer into the command's outcome.
func enforceGuard(a *app, ok bool, err error) error {
	if err != nil {
		return err
	}
	if !ok {
		if _, err := fmt.Fprintln(a.out, "Aborted."); err != nil {
			return err
		}
		// Declining has to be distinguishable from success, or "kctx ctx prod
		// && ./deploy.sh" deploys against whatever context you were already on.
		return &exitError{code: ExitAborted}
	}
	return nil
}

// confirmGuard asks for confirmation when a guard rule demands it before
// reaching a context.
func confirmGuard(a *app, target string) (bool, error) {
	classifier, err := a.classifier()
	if err != nil {
		return false, err
	}
	verdict := classifier.Classify(target)
	if !verdict.Confirm {
		return true, nil
	}

	pal := a.palette()
	prompt := fmt.Sprintf("%s %s is classified %s by the guard rule %s.",
		pal.Red("!"), pal.Bold(target), pal.Red(string(verdict.Level)), pal.Dim(verdict.Rule))
	return confirmPhrase(a, prompt, target)
}

// confirmNamespaceGuard asks for confirmation when a namespace rule demands it
// before reaching a namespace through target.
//
// The phrase to retype is the namespace, not the context: the namespace is
// what the rule is about, and it is the word the user is being asked to look
// at twice.
func confirmNamespaceGuard(a *app, target, namespace string) (bool, error) {
	classifier, err := a.classifier()
	if err != nil {
		return false, err
	}
	verdict := classifier.ClassifyNamespace(target, namespace)
	if !verdict.Confirm {
		return true, nil
	}

	pal := a.palette()
	prompt := fmt.Sprintf("%s %s in %s is classified %s by the guard rule %s.",
		pal.Red("!"), pal.Bold(namespace), pal.Bold(target),
		pal.Red(string(verdict.Level)), pal.Dim(verdict.Rule))
	return confirmPhrase(a, prompt, namespace)
}

// guardSuffix renders the badge appended to the switch confirmation line.
func guardSuffix(a *app, target string) string {
	classifier, err := a.classifier()
	if err != nil {
		return ""
	}
	return badgeFor(a, classifier.Classify(target))
}

// namespaceGuardSuffix renders the badge for a namespace as reached through one
// context.
func namespaceGuardSuffix(a *app, target, namespace string) string {
	classifier, err := a.classifier()
	if err != nil {
		return ""
	}
	return badgeFor(a, classifier.ClassifyNamespace(target, namespace))
}

// badgeFor colorizes a verdict's label, or returns "" when it has none.
func badgeFor(a *app, verdict guard.Verdict) string {
	if verdict.Label == "" {
		return ""
	}

	pal := a.palette()
	switch verdict.Style() {
	case "danger":
		return "  " + pal.Red(verdict.Label)
	case "warn":
		return "  " + pal.Yellow(verdict.Label)
	default:
		return "  " + pal.Dim(verdict.Label)
	}
}

// printContextNames writes one context per line, marking the current one.
func printContextNames(a *app, cfg *clientcmdapi.Config) error {
	names := contexts.Names(cfg)
	if len(names) == 0 {
		// Bare "kctx" is the first thing a user types on a new machine.
		// Printing nothing and exiting 0 reads as a broken binary; say what
		// "kctx list" says.
		_, err := fmt.Fprintln(a.errOut, "No contexts found in the kubeconfig.")
		return err
	}

	pal := a.palette()
	for _, name := range names {
		line := boldIfCurrent(pal, name, cfg.CurrentContext)
		if _, err := fmt.Fprintln(a.out, line); err != nil {
			return err
		}
	}
	return nil
}

// completeContexts completes the first context argument only, for commands
// that take exactly one.
func completeContexts(a *app) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return contextCandidates(a, nil)
	}
}

// completeContextList completes every positional argument, minus the ones
// already typed.
//
// "delete", "doctor" and "guard add" all take a list; wiring them to the
// single-argument version meant the second name onwards completed nothing.
func completeContextList(a *app) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return contextCandidates(a, args)
	}
}

// contextCandidates lists context names and aliases, skipping any in used.
func contextCandidates(a *app, used []string) ([]string, cobra.ShellCompDirective) {
	taken := make(map[string]struct{}, len(used))
	for _, name := range used {
		taken[name] = struct{}{}
	}
	cfg, err := a.loader().Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	var names []string
	for _, name := range contexts.Names(cfg) {
		if _, ok := taken[name]; !ok {
			names = append(names, name)
		}
	}
	if userCfg, err := a.userConfig(); err == nil {
		for _, pair := range userCfg.AliasList() {
			if _, ok := taken[pair.Alias]; !ok {
				names = append(names, pair.Alias+"\t→ "+pair.Target)
			}
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
