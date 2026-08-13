package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/contexts"
)

// newListCmd prints every context as a table.
func newListCmd(a *app) *cobra.Command {
	var wide bool

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List every context",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(a, wide)
		},
	}
	cmd.Flags().BoolVarP(&wide, "wide", "w", false, "also show cluster, user and server")
	return cmd
}

// runList renders the context table.
func runList(a *app, wide bool) error {
	cfg, err := a.loader().Load()
	if err != nil {
		return err
	}
	list := contexts.List(cfg)

	if a.jsonOutput() {
		return json.NewEncoder(a.out).Encode(list)
	}

	classifier, err := a.classifier()
	if err != nil {
		return err
	}

	pal := a.palette()
	headers := []string{"", "NAME", "NAMESPACE", "GUARD"}
	if wide {
		headers = append(headers, "CLUSTER", "USER", "SERVER")
	}

	rows := make([][]string, 0, len(list))
	for _, c := range list {
		marker := " "
		name := c.Name
		if c.Current {
			marker = pal.Green("*")
			name = pal.Bold(name)
		}
		verdict := classifier.Classify(c.Name)
		badge := ""
		switch verdict.Style() {
		case "danger":
			badge = pal.Red(verdict.Label)
		case "warn":
			badge = pal.Yellow(verdict.Label)
		}

		row := []string{marker, name, c.Namespace, badge}
		if wide {
			row = append(row, c.Cluster, c.User, c.Server)
		}
		rows = append(rows, row)
	}

	if len(rows) == 0 {
		_, err := fmt.Fprintln(a.errOut, "No contexts found in the kubeconfig.")
		return err
	}
	return renderTable(a, headers, rows)
}
