package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/kubeconfig"
)

// newDeleteCmd removes contexts from the kubeconfig.
func newDeleteCmd(a *app) *cobra.Command {
	var pruneOrphans bool

	cmd := &cobra.Command{
		Use:     "delete <context>...",
		Aliases: []string{"rm", "del"},
		Short:   "Delete one or more contexts",
		Long: "Delete one or more contexts.\n\n" +
			"Passing \".\" deletes the current context. The cluster and user entries a\n" +
			"deleted context referenced are left in place unless --prune is given —\n" +
			"they are frequently shared, and removing credentials as a side effect of\n" +
			"dropping one context is rarely what anyone wants.\n\n" +
			"The kubeconfig is backed up before the write.",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: completeContextList(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(a, args, pruneOrphans)
		},
	}
	cmd.Flags().BoolVar(&pruneOrphans, "prune", false, "also remove cluster and user entries left unreferenced")
	return cmd
}

// runDelete removes the named contexts after confirmation.
func runDelete(a *app, names []string, pruneOrphans bool) error {
	if err := guardSessionScoped("delete"); err != nil {
		return err
	}

	loader := a.loader()
	cfg, err := loader.Load()
	if err != nil {
		return err
	}

	targets, err := resolveContexts(a, cfg, names)
	if err != nil {
		return err
	}

	pal := a.palette()
	ok, err := confirm(a, fmt.Sprintf("Delete context %s?", pal.Bold(strings.Join(targets, ", "))))
	if err != nil {
		return err
	}
	if !ok {
		_, err := fmt.Fprintln(a.out, "Aborted.")
		return err
	}

	orphans, err := contexts.Delete(cfg, targets...)
	if err != nil {
		return err
	}
	if pruneOrphans {
		contexts.PruneOrphans(cfg, orphans)
	}
	if err := loader.Save(cfg, kubeconfig.WithBackup()); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(a.out, "Deleted %d context(s).\n", len(targets)); err != nil {
		return err
	}
	if !orphans.Empty() && !pruneOrphans {
		fmt.Fprintf(a.errOut, "note: %s are now unreferenced; re-run with --prune to remove them\n",
			describeOrphans(orphans))
	}
	return nil
}

// describeOrphans renders the orphan summary for the hint line.
func describeOrphans(o contexts.Orphans) string {
	var parts []string
	if len(o.Clusters) > 0 {
		parts = append(parts, "cluster(s) "+strings.Join(o.Clusters, ", "))
	}
	if len(o.Users) > 0 {
		parts = append(parts, "user(s) "+strings.Join(o.Users, ", "))
	}
	return strings.Join(parts, " and ")
}
