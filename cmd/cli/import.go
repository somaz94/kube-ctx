package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/kubeconfig"
	"github.com/somaz94/kube-ctx/pkg/transfer"
)

// importOptions holds the flags of the import command.
type importOptions struct {
	contexts  []string
	prefix    string
	as        string
	overwrite bool
	prune     bool
	dryRun    bool
}

// newImportCmd merges contexts from other kubeconfig files into the user's.
func newImportCmd(a *app) *cobra.Command {
	var opts importOptions

	cmd := &cobra.Command{
		Use:   "import <file>...",
		Short: "Merge contexts from another kubeconfig into yours",
		Long: "Merge contexts from another kubeconfig into yours.\n\n" +
			"The named files are read on their own — $KUBECONFIG is not consulted for\n" +
			"them — and every selected context is copied over with the cluster and user\n" +
			"stanzas it references. Nothing is activated: the current context is left\n" +
			"exactly where it was.\n\n" +
			"A context whose name is already taken is refused rather than replaced.\n" +
			"Import it under another name with --prefix or --as, or pass --overwrite to\n" +
			"replace it. Re-importing a file whose contexts are already present is a\n" +
			"no-op reported as \"unchanged\", so the command is safe to repeat.\n\n" +
			"Cluster and user names collide far more often than context names do —\n" +
			"every kubeadm cluster is called \"kubernetes\" — and a colliding stanza\n" +
			"whose contents differ is imported under a suffixed name instead of\n" +
			"replacing the one already there, which would repoint the contexts that\n" +
			"share it at a different API server.\n\n" +
			"Replacing a context with --overwrite can leave the cluster and user it used\n" +
			"to point at unreferenced. The report names those, and only those — not the\n" +
			"stanzas your kubeconfig was already carrying. --prune removes them, and\n" +
			"like \"kctx delete --prune\" it takes every unreferenced entry with them.\n\n" +
			"The kubeconfig is backed up before the write.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runImport(a, args, opts)
		},
	}

	f := cmd.Flags()
	f.StringSliceVar(&opts.contexts, "context", nil, "import only these contexts (repeatable)")
	f.StringVar(&opts.prefix, "prefix", "", "prepend this to every imported context name")
	f.StringVar(&opts.as, "as", "", "import a single context under this name")
	f.BoolVar(&opts.overwrite, "overwrite", false, "replace contexts that already exist")
	f.BoolVar(&opts.prune, "prune", false, "also remove cluster and user entries this import leaves unreferenced")
	f.BoolVar(&opts.dryRun, "dry-run", false, "report what would be imported and change nothing")
	cmd.MarkFlagsMutuallyExclusive("as", "prefix")
	_ = cmd.RegisterFlagCompletionFunc("context", completeSourceContexts)

	return cmd
}

// runImport merges every source file into the kubeconfig and persists the
// result.
func runImport(a *app, files []string, opts importOptions) error {
	if err := guardSessionScoped("import"); err != nil {
		return err
	}

	loader := a.loader()
	cfg, err := loader.Load()
	if err != nil {
		return err
	}

	// Snapshotted before the first merge so the report can name what this import
	// orphaned rather than what the kubeconfig was already carrying.
	orphansBefore := contexts.FindOrphans(cfg)

	var result transfer.Result
	for _, file := range files {
		src, err := kubeconfig.ReadFile(file)
		if err != nil {
			return err
		}
		// Merged into the same cfg one file after another, so the second file
		// collides with what the first one brought in rather than overwriting it.
		partial, err := transfer.Merge(cfg, src, transfer.Options{
			Contexts:  opts.contexts,
			Prefix:    opts.prefix,
			Rename:    opts.as,
			Overwrite: opts.overwrite,
		})
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		result.Entries = append(result.Entries, partial.Entries...)
	}

	orphans := contexts.FindOrphans(cfg)
	// Reported: only what this import left behind. A kubeconfig in use for a year
	// usually carries a few unreferenced stanzas already, and blaming those on
	// the command just run is how a note becomes noise people skip.
	orphaned := orphans.Without(orphansBefore)

	pruned := opts.prune && !opts.dryRun
	if pruned {
		// Removed: every unreferenced entry, the same as "kctx delete --prune".
		// Scoping the removal to this import's leftovers would make the hint
		// below a lie — by the time the user re-runs with --prune, the stanzas it
		// named are no longer new, and the command would skip them.
		contexts.PruneOrphans(cfg, orphans)
	}

	// Nothing is written when no context moved, which keeps a repeated import
	// from rotating a backup generation for a no-op — but a prune that has
	// something to remove is itself a change worth saving.
	if !opts.dryRun && (result.Changed() || (pruned && !orphans.Empty())) {
		if err := loader.Save(cfg, kubeconfig.WithBackup()); err != nil {
			return err
		}
	}
	if err := reportImport(a, result, opts.dryRun); err != nil {
		return err
	}
	if !orphaned.Empty() && !pruned {
		// stderr, so the hint does not land in a "-o json" consumer's payload.
		_, err := fmt.Fprintf(a.errOut, "note: %s are now unreferenced; re-run with --prune to remove them\n",
			describeOrphans(orphaned))
		return err
	}
	return nil
}

// reportImport renders the per-context table and the summary line.
func reportImport(a *app, result transfer.Result, dryRun bool) error {
	headers := []string{"CONTEXT", "ACTION", "CLUSTER", "USER", "SOURCE"}
	rows := make([][]string, 0, len(result.Entries))

	pal := a.palette()
	for _, e := range result.Entries {
		action := string(e.Action)
		switch e.Action {
		case transfer.ActionOverwritten:
			action = pal.Yellow(action)
		case transfer.ActionUnchanged:
			action = pal.Dim(action)
		}
		// Blank unless the name changed: repeating the context name in every row
		// of a column headed SOURCE says nothing.
		source := ""
		if e.Renamed() {
			source = pal.Dim(e.Source)
		}
		rows = append(rows, []string{pal.Bold(e.Name), action, e.Cluster, e.User, source})
	}

	if err := renderOutput(a, headers, rows, result.Entries); err != nil {
		return err
	}
	if a.jsonOutput() {
		return nil
	}

	if !result.Changed() {
		_, err := fmt.Fprintln(a.errOut, "Nothing to import; every context is already present.")
		return err
	}
	verb := "Imported"
	if dryRun {
		verb = "Would import"
	}
	_, err := fmt.Fprintf(a.out, "%s %d context(s). Switch to one with %s.\n",
		verb, len(result.Entries), pal.Bold("kctx ctx <name>"))
	return err
}

// completeSourceContexts completes --context from the files already typed on
// the command line.
//
// Completion has to read the source, not the user's kubeconfig: the whole point
// of --context is naming something that is not in there yet, so completing from
// the merged config would offer exactly the wrong set.
func completeSourceContexts(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var names []string
	for _, file := range args {
		src, err := kubeconfig.ReadFile(file)
		if err != nil {
			continue // a half-typed path is the normal case while completing
		}
		for name := range src.Contexts {
			names = append(names, name)
		}
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
