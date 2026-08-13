package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/contexts"
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
			"pointing at a name that no longer exists.",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeContexts(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRename(a, args[0], args[1])
		},
	}
}

// runRename applies the rename and persists it.
func runRename(a *app, oldName, newName string) error {
	loader := a.loader()
	cfg, err := loader.Load()
	if err != nil {
		return err
	}

	if oldName == "." {
		if cfg.CurrentContext == "" {
			return fmt.Errorf("no current context to rename")
		}
		oldName = cfg.CurrentContext
	}
	if err := contexts.Rename(cfg, oldName, newName); err != nil {
		return err
	}
	if err := loader.Save(cfg, kubeconfig.WithBackup()); err != nil {
		return err
	}

	pal := a.palette()
	_, err = fmt.Fprintf(a.out, "Renamed context %s to %s.\n", pal.Dim(oldName), pal.Bold(newName))
	return err
}
