package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/config"
	"github.com/somaz94/kube-ctx/pkg/contexts"
)

// newAliasCmd manages short names for contexts.
func newAliasCmd(a *app) *cobra.Command {
	var remove string

	cmd := &cobra.Command{
		Use:   "alias [name] [context]",
		Short: "Manage context aliases",
		Long: "Manage context aliases.\n\n" +
			"With no arguments the current aliases are listed. An alias may be used\n" +
			"anywhere a context name is accepted; prefix it with \"@\" to force the\n" +
			"alias reading when a context of the same name also exists.",
		Args: cobra.MaximumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAlias(a, args, remove)
		},
	}
	cmd.Flags().StringVarP(&remove, "delete", "d", "", "delete an alias")
	return cmd
}

// runAlias lists, sets, or deletes an alias.
func runAlias(a *app, args []string, remove string) error {
	userCfg, err := a.userConfig()
	if err != nil {
		return err
	}

	switch {
	case remove != "":
		if err := userCfg.DeleteAlias(remove); err != nil {
			return err
		}
		if err := userCfg.Save(); err != nil {
			return err
		}
		_, err := fmt.Fprintf(a.out, "Deleted alias %s.\n", a.palette().Bold(remove))
		return err

	case len(args) == 2:
		kubeCfg, err := a.loader().Load()
		if err != nil {
			return err
		}
		if !contexts.Exists(kubeCfg, args[1]) {
			return fmt.Errorf("no context named %q", args[1])
		}
		if err := userCfg.SetAlias(args[0], args[1]); err != nil {
			return err
		}
		if err := userCfg.Save(); err != nil {
			return err
		}
		pal := a.palette()
		_, err = fmt.Fprintf(a.out, "Alias %s now points at %s.\n", pal.Bold(args[0]), pal.Cyan(args[1]))
		return err

	case len(args) == 1:
		return fmt.Errorf("give both an alias and a context, or use --delete <alias>")

	default:
		return listAliases(a, userCfg.AliasList())
	}
}

// listAliases prints the alias table.
func listAliases(a *app, pairs []config.AliasPair) error {
	if len(pairs) == 0 {
		_, err := fmt.Fprintf(a.errOut, "No aliases defined. Add one with \"kctx alias p prod-cluster\".\n")
		return err
	}

	rows := make([][]string, 0, len(pairs))
	for _, p := range pairs {
		rows = append(rows, []string{p.Alias, p.Target})
	}
	return renderTable(a, []string{"ALIAS", "CONTEXT"}, rows)
}
