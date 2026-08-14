package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// invokedName returns the name kube-ctx was called as. The hook defines a shell
// function under it and calls back through it, so it has to be a name the
// caller's $PATH resolves.
//
// argv[0] rather than os.Executable(): on Linux the latter reads
// /proc/self/exe, which resolves symlinks. krew installs a plugin as a symlink
// named kubectl-<plugin> pointing into its own store, and puts only that
// symlink on $PATH — so the resolved path carries the store's filename and the
// hook would define a function nothing can call. macOS keeps the symlink and
// happens to be fine, which is exactly how this would have shipped broken on
// the platform most kubectl users are on.
//
// A variable so tests can pin it; under `go test` argv[0] is the test binary.
var invokedName = func() string {
	if len(os.Args) > 0 && os.Args[0] != "" {
		return filepath.Base(os.Args[0])
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		return filepath.Base(exe)
	}
	return "kctx"
}

// newInitCmd prints the shell integration snippet.
func newInitCmd(a *app) *cobra.Command {
	var noCompletion bool

	cmd := &cobra.Command{
		Use:       "init [bash|zsh|fish]",
		Short:     "Print the shell integration to eval in your rc file",
		ValidArgs: shellNames(),
		Long: "Print the shell integration for the given shell, defaulting to $SHELL.\n\n" +
			"Add this to your rc file:\n\n" +
			"    eval \"$(kctx init zsh)\"        # ~/.zshrc\n" +
			"    eval \"$(kctx init bash)\"       # ~/.bashrc\n" +
			"    kctx init fish | source        # ~/.config/fish/config.fish\n\n" +
			"With the hook installed, a switch applies to the current terminal only:\n" +
			"kube-ctx gives this shell a private copy of the kubeconfig and exports\n" +
			"$KUBECONFIG to point at it. Other terminals are untouched. Without the\n" +
			"hook, kube-ctx edits the global kubeconfig, the way kubectx does.\n\n" +
			"Completions are included unless --no-completion is given.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runInit(a, cmd, name, noCompletion)
		},
	}
	cmd.Flags().BoolVar(&noCompletion, "no-completion", false, "omit the completion script")
	return cmd
}

// runInit writes the hook, the prompt hint, and optionally the completions.
func runInit(a *app, cmd *cobra.Command, name string, noCompletion bool) error {
	sh, err := shellenv.ParseShell(name, os.Getenv("SHELL"))
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintln(a.out, shellenv.Hook(sh, invokedName())); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(a.out, shellenv.PromptHint(sh)); err != nil {
		return err
	}
	if noCompletion {
		return nil
	}

	root := cmd.Root()
	switch sh {
	case shellenv.Bash:
		return root.GenBashCompletionV2(a.out, true)
	case shellenv.Zsh:
		return root.GenZshCompletion(a.out)
	case shellenv.Fish:
		return root.GenFishCompletion(a.out, true)
	}
	return nil
}

// shellNames returns the supported shells as strings, for completion.
func shellNames() []string {
	names := make([]string, 0, len(shellenv.Shells))
	for _, sh := range shellenv.Shells {
		names = append(names, string(sh))
	}
	return names
}
