package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/config"
	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// bindOptions holds the flags of the bind command.
type bindOptions struct {
	path   string
	delete bool
	apply  bool
}

// newBindCmd manages the directory-to-context bindings.
func newBindCmd(a *app) *cobra.Command {
	var opts bindOptions

	cmd := &cobra.Command{
		Use:   "bind [context]",
		Short: "Bind a directory to a context, and switch on entering it",
		Long: "Bind a directory to the context you work in there.\n\n" +
			"  kctx bind                    list every binding\n" +
			"  kctx bind prod-eks           bind the current directory\n" +
			"  kctx bind . --path ~/work    bind another directory to the current context\n" +
			"  kctx bind --delete           remove the binding on this directory\n\n" +
			"A directory inherits the binding of its nearest bound ancestor, so binding\n" +
			"a repository root covers everything in it. The deepest binding wins.\n\n" +
			"With the shell hook installed, entering a bound directory switches this\n" +
			"terminal to its context — and only this terminal. It fires once per tree:\n" +
			"moving around inside a bound repository does not keep re-switching, so a\n" +
			"context you pick by hand in there stays picked. Leaving the directory does\n" +
			"not switch back; a binding chooses a context, it does not own the shell.\n\n" +
			"A context a guard rule gates with confirm is never entered automatically —\n" +
			"a prompt on every cd is unusable, and arriving in production by walking\n" +
			"into a directory is the accident this tool exists to prevent. kube-ctx says\n" +
			"so once and leaves you where you were.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeContexts(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBind(a, args, opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.path, "path", "", "act on this directory instead of the current one")
	f.BoolVarP(&opts.delete, "delete", "d", false, "remove the binding on the directory")
	f.BoolVar(&opts.apply, "apply", false, "switch to the bound context; what the shell hook runs on every directory change")
	cmd.MarkFlagsMutuallyExclusive("delete", "apply")

	return cmd
}

// runBind dispatches to the list, set, delete or apply behavior.
func runBind(a *app, args []string, opts bindOptions) error {
	if opts.apply {
		return runBindApply(a, opts)
	}

	userCfg, err := a.userConfig()
	if err != nil {
		return err
	}
	dir, err := bindDir(opts)
	if err != nil {
		return err
	}

	switch {
	case opts.delete:
		if err := userCfg.DeleteBinding(dir); err != nil {
			return err
		}
		if err := userCfg.Save(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(a.out, "Unbound %s.\n", a.palette().Dim(dir))
		return err

	case len(args) == 1:
		cfg, err := a.loader().Load()
		if err != nil {
			return err
		}
		target, err := resolveContext(a, cfg, args[0])
		if err != nil {
			return err
		}
		if err := userCfg.SetBinding(dir, target); err != nil {
			return err
		}
		if err := userCfg.Save(); err != nil {
			return err
		}
		pal := a.palette()
		_, err = fmt.Fprintf(a.out, "Bound %s to %s.%s\n",
			pal.Dim(dir), pal.Bold(target), guardSuffix(a, target))
		return err

	default:
		return listBindings(a, userCfg)
	}
}

// listBindings renders every binding.
func listBindings(a *app, userCfg *config.Config) error {
	list := userCfg.BindingList()
	if len(list) == 0 && !a.jsonOutput() {
		_, err := fmt.Fprintln(a.errOut, "No directories are bound. Bind this one with \"kctx bind <context>\".")
		return err
	}

	pal := a.palette()
	rows := make([][]string, 0, len(list))
	for _, pair := range list {
		rows = append(rows, []string{pair.Directory, pal.Bold(pair.Target)})
	}
	return renderOutput(a, []string{"DIRECTORY", "CONTEXT"}, rows, list)
}

// bindDir returns the directory to act on.
func bindDir(opts bindOptions) (string, error) {
	if opts.path != "" {
		return opts.path, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determine the current directory: %w", err)
	}
	return dir, nil
}

// runBindApply switches this terminal to the context bound to the current
// directory, and is the one path here that has to stay silent and cheap: the
// shell hook runs it on every directory change.
func runBindApply(a *app, opts bindOptions) error {
	// Pinned shells opted out. "kctx shell prod" promised to stay on prod, and
	// a cd is not a request to break that.
	if os.Getenv(shellenv.EnvPinned) != "" {
		return nil
	}

	userCfg, err := a.userConfig()
	if err != nil {
		return err
	}
	// The overwhelmingly common case, and the one that has to cost nothing: no
	// bindings at all, so nothing is read and no kubeconfig is loaded.
	if len(userCfg.Bindings) == 0 {
		return nil
	}

	dir, err := bindDir(opts)
	if err != nil {
		return err
	}
	target, _, ok := userCfg.ResolveBinding(dir)
	// Leaving a bound tree is not a request to switch back: the binding chose a
	// context, it does not own the shell.
	if !ok || target == os.Getenv(shellenv.EnvBound) {
		return nil
	}

	cfg, err := a.loader().Load()
	if err != nil {
		return err
	}
	if !contexts.Exists(cfg, target) {
		// A binding outliving its context is normal — the context was renamed or
		// deleted. Say so once rather than failing every cd.
		return announceBound(a, target, fmt.Sprintf("no context named %q is bound to this directory any more", target))
	}

	classifier, err := a.classifier()
	if err != nil {
		return err
	}
	// The namespace counts as well as the context: a directory bound to a
	// context whose own default is kube-system would otherwise drop the shell
	// into the guarded namespace on a cd, and walking into a directory is no
	// more consent to that than it is to being in production.
	if classifier.Classify(target).Confirm ||
		classifier.ClassifyNamespace(target, namespaceOf(cfg, target)).Confirm {
		return announceBound(a, target, fmt.Sprintf("%s is guarded; switch to it explicitly with \"kctx ctx %s\"",
			a.palette().Bold(target), target))
	}

	// The switch is announced on stderr: this one was triggered by a cd, not
	// typed, and a "kctx bind --apply" in a script must not have a sentence
	// about it land in whatever was capturing stdout.
	if err := switchContext(promptingOnStderr(a), cfg, target); err != nil {
		return err
	}
	return markBound(a, target)
}

// announceBound reports why the binding was not followed and records that it
// was considered, so the message appears once on entering the tree rather than
// on every directory change inside it.
func announceBound(a *app, target, message string) error {
	if _, err := fmt.Fprintln(a.errOut, message); err != nil {
		return err
	}
	return markBound(a, target)
}

// markBound appends the binding this shell has acted on to the file the hook
// sources. Without the hook there is nowhere to put it, and the binding is
// re-evaluated next time — which is correct, since the switch was global.
func markBound(a *app, target string) error {
	envFile := os.Getenv(shellenv.EnvFile)
	if envFile == "" {
		return nil
	}
	sh, err := shellenv.ParseShell(os.Getenv(shellenv.EnvShell), os.Getenv("SHELL"))
	if err != nil {
		sh = shellenv.Bash
	}

	f, err := os.OpenFile(envFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("write shell environment: %w", err)
	}
	if _, err := fmt.Fprintln(f, shellenv.ExportLine(sh, shellenv.EnvBound, target)); err != nil {
		_ = f.Close()
		return fmt.Errorf("write shell environment: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write shell environment: %w", err)
	}
	return nil
}
