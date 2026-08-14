package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/kubeconfig"
	"github.com/somaz94/kube-ctx/pkg/transfer"
)

// exportOptions holds the flags of the export command.
type exportOptions struct {
	file    string
	all     bool
	flatten bool
	force   bool
}

// newExportCmd writes contexts out as a standalone kubeconfig.
func newExportCmd(a *app) *cobra.Command {
	var opts exportOptions

	cmd := &cobra.Command{
		Use:   "export [context]...",
		Short: "Write contexts out as a standalone kubeconfig",
		Long: "Write contexts out as a standalone kubeconfig.\n\n" +
			"With no argument the current context is exported; \".\" means the same, and\n" +
			"--all takes everything. Only the cluster and user stanzas the exported\n" +
			"contexts actually reference come along, so the result is the smallest\n" +
			"kubeconfig that still works. It goes to stdout unless -f names a file.\n\n" +
			"--flatten inlines the certificates and keys the contexts point at, which\n" +
			"is what makes the file portable to another machine; without it the export\n" +
			"still refers to paths that only exist on this one.\n\n" +
			"The output carries credentials. A file is written 0600 and an existing one\n" +
			"is never replaced without --force, and a context guarded with confirm asks\n" +
			"before it is exported — handing over a kubeconfig is handing over a route\n" +
			"to the cluster.",
		Args:              cobra.ArbitraryArgs,
		ValidArgsFunction: completeContextList(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExport(a, args, opts)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.file, "file", "f", "", "write to this file instead of stdout")
	f.BoolVar(&opts.all, "all", false, "export every context")
	f.BoolVar(&opts.flatten, "flatten", false, "inline the certificates and keys, making the result portable")
	f.BoolVar(&opts.force, "force", false, "replace the output file if it exists")

	return cmd
}

// runExport extracts the requested contexts and writes them out.
func runExport(a *app, names []string, opts exportOptions) error {
	cfg, err := a.loader().Load()
	if err != nil {
		return err
	}

	targets, err := exportTargets(a, cfg, names, opts.all)
	if err != nil {
		return err
	}
	for _, target := range targets {
		// The prompt goes to stderr: stdout is where the kubeconfig goes, and a
		// question printed there would end up at the top of the file the user
		// redirected it into.
		if err := requireGuardConfirmation(promptingOnStderr(a), target); err != nil {
			return err
		}
	}

	out, err := transfer.Extract(cfg, targets)
	if err != nil {
		return err
	}
	if opts.flatten {
		if err := kubeconfig.Flatten(out); err != nil {
			return err
		}
	}

	data, err := encodeExport(a, out)
	if err != nil {
		return err
	}
	if opts.file == "" {
		_, err := a.out.Write(data)
		return err
	}

	if err := kubeconfig.WriteFile(opts.file, data, opts.force); err != nil {
		return err
	}
	// On stderr so that "kctx export -f x && cat x" stays quiet on stdout, and
	// because the warning matters more than the count: this file is a credential.
	_, err = fmt.Fprintf(a.errOut, "Wrote %d context(s) to %s (0600). It carries credentials.\n",
		len(targets), opts.file)
	return err
}

// exportTargets resolves the contexts to export, deduplicated and in the order
// the user named them.
func exportTargets(a *app, cfg *clientcmdapi.Config, names []string, all bool) ([]string, error) {
	if all {
		if len(names) > 0 {
			return nil, fmt.Errorf("--all exports every context; drop the context arguments")
		}
		targets := contexts.Names(cfg)
		if len(targets) == 0 {
			return nil, fmt.Errorf("no contexts found in the kubeconfig")
		}
		return targets, nil
	}

	// No argument means the current context, the same default "kctx current" and
	// "kctx ns" use.
	if len(names) == 0 {
		names = []string{"."}
	}
	resolved, err := resolveContexts(a, cfg, names)
	if err != nil {
		return nil, err
	}
	return dedupe(resolved), nil
}

// encodeExport serializes the extracted config in the requested format.
//
// It does not go through renderOutput: the payload is a kubeconfig rather than a
// table of rows, and "-o json" here means the same document in JSON, not a
// different one.
func encodeExport(a *app, cfg *clientcmdapi.Config) ([]byte, error) {
	if a.jsonOutput() {
		return kubeconfig.EncodeJSON(cfg)
	}
	return kubeconfig.Encode(cfg)
}
