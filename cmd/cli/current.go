package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/contexts"
)

// newCurrentCmd prints where you are without changing it.
//
// This is what every prompt integration shells out to — powerlevel10k, starship
// custom segments, tmux status lines — and it is the reason a kubectx user
// reaches for "kubectx -c" on day one. kube-ctx has a better answer than
// "kubectl config current-context": inside a managed shell that command reads
// the session copy only if $KUBECONFIG is honored, whereas this always reports
// the context this terminal is actually on.
func newCurrentCmd(a *app) *cobra.Command {
	var namespace bool

	cmd := &cobra.Command{
		Use:     "current",
		Aliases: []string{"cur"},
		Short:   "Print the current context, without switching",
		Long: "Print the current context and exit.\n\n" +
			"Nothing is changed and no picker opens, so this is safe to call from a\n" +
			"shell prompt. With -n the namespace is printed instead.\n\n" +
			"Exits non-zero when nothing is set, so a prompt can fall back quietly.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCurrent(a, namespace)
		},
	}
	cmd.Flags().BoolVarP(&namespace, "namespace", "n", false, "print the current namespace instead")
	return cmd
}

// runCurrent prints the current context or namespace.
func runCurrent(a *app, wantNamespace bool) error {
	cfg, err := a.loader().Load()
	if err != nil {
		return err
	}
	if cfg.CurrentContext == "" {
		// Silent: a prompt calling this on every keystroke should not paint an
		// error into the user's terminal, it should just print nothing.
		return &exitError{code: ExitFailure}
	}

	value := cfg.CurrentContext
	if wantNamespace {
		ns, err := contexts.Namespace(cfg, "")
		if err != nil {
			return err
		}
		if ns == "" {
			ns = "default"
		}
		value = ns
	}

	if a.jsonOutput() {
		ns, err := contexts.Namespace(cfg, "")
		if err != nil {
			return err
		}
		if ns == "" {
			ns = "default"
		}
		return writeJSON(a, struct {
			Context   string `json:"context"`
			Namespace string `json:"namespace"`
		}{cfg.CurrentContext, ns})
	}

	// Deliberately unadorned: this is substituted into a prompt string, so a
	// badge or a color would be pasted in with it.
	_, err = fmt.Fprintln(a.out, value)
	return err
}
