package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/contexts"
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
	if back == 0 && len(args) == 1 {
		if n := contexts.ParseRef(args[0]); n > 0 {
			back = n
		}
	}
	if back > 0 {
		history, err := contexts.NewHistory(historyScope())
		if err != nil {
			return "", err
		}
		return history.Lookup(back)
	}

	if len(args) == 0 {
		return pickContext(a, cfg)
	}

	userCfg, err := a.userConfig()
	if err != nil {
		return "", err
	}
	return userCfg.ResolveAlias(args[0]), nil
}

// switchContext points current-context at target, records the previous one, and
// persists the change.
func switchContext(a *app, cfg *clientcmdapi.Config, target string) error {
	if !contexts.Exists(cfg, target) {
		return fmt.Errorf("no context named %q", target)
	}
	ok, err := confirmGuard(a, target)
	if err != nil {
		return err
	}
	if !ok {
		_, err := fmt.Fprintln(a.out, "Aborted.")
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
	namespace, err := contexts.Namespace(cfg, target)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(a.out, "Switched to context %s (namespace %s).%s\n",
		pal.Bold(target), pal.Cyan(namespace), guardSuffix(a, target))
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

// confirmGuard asks for confirmation when a guard rule demands it before
// switching to a context.
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

// guardSuffix renders the badge appended to the switch confirmation line.
func guardSuffix(a *app, target string) string {
	classifier, err := a.classifier()
	if err != nil {
		return ""
	}
	verdict := classifier.Classify(target)
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
		line := name
		if name == cfg.CurrentContext {
			line = pal.Bold(name)
		}
		if _, err := fmt.Fprintln(a.out, line); err != nil {
			return err
		}
	}
	return nil
}

// completeContexts provides shell completion for context names and aliases.
func completeContexts(a *app) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		cfg, err := a.loader().Load()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		names := contexts.Names(cfg)

		if userCfg, err := a.userConfig(); err == nil {
			for _, pair := range userCfg.AliasList() {
				names = append(names, pair.Alias+"\t→ "+pair.Target)
			}
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
}
