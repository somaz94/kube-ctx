package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/namespaces"
	"github.com/somaz94/kube-ctx/pkg/picker"
)

// nsHistoryPrefix keeps namespace history separate per context: "back one
// namespace" in the dev cluster must not offer a namespace that only exists in
// prod.
const nsHistoryPrefix = "ns-"

// newNsCmd switches the namespace of the current context.
func newNsCmd(a *app) *cobra.Command {
	var (
		back    int
		refresh bool
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:     "ns [namespace|-|-N]",
		Aliases: []string{"n", "namespace"},
		Short:   "Switch the namespace of the current context",
		Long: "Switch the default namespace of the current context.\n\n" +
			"With no argument the namespaces are listed; \"-\" or \"-N\" restores the\n" +
			"previous (or Nth previous) namespace of this context. The list comes from\n" +
			"the API server and is cached, so it still works when the cluster is\n" +
			"briefly unreachable.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNs(a, args, back, refresh, timeout)
		},
		ValidArgsFunction: completeNamespaces(a),
	}
	cmd.Flags().IntVarP(&back, "back", "b", 0, "switch to the Nth previous namespace (same as -N)")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "bypass the namespace cache")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "how long to wait for the API server")
	return cmd
}

// runNs resolves the requested namespace and applies it to the current context.
func runNs(a *app, args []string, back int, refresh bool, timeout time.Duration) error {
	loader := a.loader()
	cfg, err := loader.Load()
	if err != nil {
		return err
	}
	if cfg.CurrentContext == "" {
		return fmt.Errorf("no current context is set; run \"kctx ctx <name>\" first")
	}

	target, err := resolveNamespaceArg(cfg, args, back)
	if err != nil {
		return err
	}
	if target == "" {
		target, err = chooseNamespace(a, cfg, refresh, timeout)
		switch {
		case errors.Is(err, picker.ErrAborted):
			return nil // the user changed their mind; nothing to report
		case err != nil:
			return err
		case target == "":
			return nil // already listed, because there was no terminal
		}
	}
	if err := requireNamespaceGuardConfirmation(a, cfg.CurrentContext, target); err != nil {
		return err
	}
	return switchNamespace(a, cfg, target)
}

// chooseNamespace opens the picker over the namespaces of the current context.
// When no terminal is available it prints the list instead and returns "".
func chooseNamespace(a *app, cfg *clientcmdapi.Config, refresh bool, timeout time.Duration) (string, error) {
	result, err := fetchNamespaces(a, cfg, refresh, timeout)
	if err != nil {
		return "", err
	}
	warnIfStale(a, result)

	current, err := contexts.Namespace(cfg, "")
	if err != nil {
		return "", err
	}

	target, err := pickNamespace(a, cfg.CurrentContext, current, result.Namespaces)
	if errors.Is(err, errPickerUnavailable) {
		return "", printNamespaces(a, result.Namespaces, current)
	}
	return target, err
}

// resolveNamespaceArg turns the command line into a namespace name, or "" when
// the user gave no target.
func resolveNamespaceArg(cfg *clientcmdapi.Config, args []string, back int) (string, error) {
	if back = historyRef(args, back); back > 0 {
		history, err := nsHistory(cfg.CurrentContext)
		if err != nil {
			return "", err
		}
		return history.Lookup(back)
	}
	if len(args) == 0 {
		return "", nil
	}
	return args[0], nil
}

// switchNamespace pins target as the current context's namespace.
func switchNamespace(a *app, cfg *clientcmdapi.Config, target string) error {
	previous, err := contexts.Namespace(cfg, "")
	if err != nil {
		return err
	}
	if err := contexts.SetNamespace(cfg, "", target); err != nil {
		return err
	}
	if err := saveSwitch(a, cfg, cfg.CurrentContext); err != nil {
		return err
	}

	if previous != target {
		history, err := nsHistory(cfg.CurrentContext)
		if err != nil {
			return err
		}
		if err := history.Push(previous); err != nil {
			return err
		}
	}

	// The badge belongs here too: moving between namespaces inside production
	// is still moving around inside production, and this line already names the
	// context it is happening in.
	pal := a.palette()
	_, err = fmt.Fprintf(a.out, "Namespace set to %s%s in context %s.%s\n",
		pal.Cyan(target), namespaceGuardSuffix(a, cfg.CurrentContext, target),
		pal.Bold(cfg.CurrentContext), guardSuffix(a, cfg.CurrentContext))
	return err
}

// printNamespaces writes one namespace per line, marking the active one.
func printNamespaces(a *app, names []string, current string) error {
	if len(names) == 0 {
		// A cluster can answer with an empty list when RBAC forbids listing
		// namespaces. Printing nothing and exiting 0 reads as a broken binary,
		// the same way it would for "kctx" with no contexts.
		_, err := fmt.Fprintln(a.errOut,
			"No namespaces returned; the credential may not be allowed to list them.")
		return err
	}

	pal := a.palette()
	for _, name := range names {
		line := name
		if name == current {
			line = pal.Bold(name)
		}
		if _, err := fmt.Fprintln(a.out, line); err != nil {
			return err
		}
	}
	return nil
}

// warnIfStale tells the user when the namespace list came from an expired
// cache, so a missing namespace is not mistaken for a deleted one.
func warnIfStale(a *app, result namespaces.Result) {
	if result.Source == namespaces.SourceCacheStale {
		fmt.Fprintf(a.errOut, "warning: showing a stale cache; the API server was unreachable (%v)\n", result.Err)
	}
}

// fetchNamespaces retrieves the namespace list for the current context.
func fetchNamespaces(a *app, cfg *clientcmdapi.Config, refresh bool, timeout time.Duration) (namespaces.Result, error) {
	rc, err := a.loader().RestConfig(cfg.CurrentContext)
	if err != nil {
		return namespaces.Result{}, err
	}

	ctx, cancel := contextWithTimeout(timeout)
	defer cancel()

	result := namespaces.Fetch(ctx, cfg.CurrentContext, namespaces.Live(rc), namespaces.Options{Refresh: refresh})
	if len(result.Namespaces) == 0 && result.Err != nil {
		return result, fmt.Errorf("list namespaces: %w", result.Err)
	}
	return result, nil
}

// nsHistory returns the namespace history stack for one context.
func nsHistory(ctxName string) (*contexts.History, error) {
	scope := nsHistoryPrefix + ctxName
	if shell := historyScope(); shell != "" {
		scope = shell + "-" + scope
	}
	return contexts.NewHistory(scope)
}

// registerNamespaceFlagCompletion wires -n to the namespace list.
//
// Best-effort: a completion that cannot be registered is not worth failing a
// command over, and cobra only errors here when the flag does not exist.
func registerNamespaceFlagCompletion(a *app, cmd *cobra.Command) {
	_ = cmd.RegisterFlagCompletionFunc("namespace", completeNamespaces(a))
}

// completeNamespaces provides shell completion from the namespace cache.
func completeNamespaces(a *app) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		cfg, err := a.loader().Load()
		if err != nil || cfg.CurrentContext == "" {
			return nil, cobra.ShellCompDirectiveError
		}
		result, err := fetchNamespaces(a, cfg, false, 2*time.Second)
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		return result.Namespaces, cobra.ShellCompDirectiveNoFileComp
	}
}
