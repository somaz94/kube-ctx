// Package cli wires the kube-ctx command tree.
//
// Commands are built by constructor functions rather than package-level vars so
// each test can build a fresh, isolated tree with its own input and output
// streams. The shared app struct carries everything a subcommand needs.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/somaz94/kube-ctx/pkg/config"
	"github.com/somaz94/kube-ctx/pkg/guard"
	"github.com/somaz94/kube-ctx/pkg/kubeconfig"
	"github.com/somaz94/kube-ctx/pkg/render"
)

// options holds the persistent flags of the root command.
type options struct {
	kubeconfig string
	output     string
	noColor    bool
	assumeYes  bool
}

// app is the runtime context every subcommand shares.
type app struct {
	opts   options
	out    io.Writer
	errOut io.Writer
	in     io.Reader
}

// loader returns a kubeconfig loader honoring --kubeconfig.
func (a *app) loader() *kubeconfig.Loader {
	return kubeconfig.New(a.opts.kubeconfig)
}

// palette returns the color palette for stdout.
func (a *app) palette() render.Palette {
	return render.New(a.out, a.opts.noColor)
}

// userConfig loads kube-ctx's own config file.
func (a *app) userConfig() (*config.Config, error) {
	return config.Load()
}

// jsonOutput reports whether the user asked for machine-readable output.
func (a *app) jsonOutput() bool { return a.opts.output == "json" }

// classifier compiles the guard rules from the user's config.
func (a *app) classifier() (*guard.Classifier, error) {
	userCfg, err := a.userConfig()
	if err != nil {
		return nil, err
	}
	return guard.New(userCfg.Guards)
}

// NewRootCmd builds the command tree writing to the given streams.
func NewRootCmd(out, errOut io.Writer, in io.Reader) *cobra.Command {
	a := &app{out: out, errOut: errOut, in: in}

	root := &cobra.Command{
		Use:   "kctx",
		Short: "Switch Kubernetes contexts and namespaces, safely",
		Long: "kctx — a kubectx/kubens replacement with per-terminal context isolation,\n" +
			"production guards, a built-in fuzzy picker, and a cluster health check.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetIn(in)

	f := root.PersistentFlags()
	f.StringVar(&a.opts.kubeconfig, "kubeconfig", "", "path to a kubeconfig file (overrides $KUBECONFIG)")
	f.StringVarP(&a.opts.output, "output", "o", "color", "output format: color, plain, json")
	f.BoolVar(&a.opts.noColor, "no-color", false, "disable color output")
	f.BoolVarP(&a.opts.assumeYes, "yes", "y", false, "skip confirmation prompts")

	root.AddCommand(
		newCtxCmd(a),
		newNsCmd(a),
		newListCmd(a),
		newRenameCmd(a),
		newDeleteCmd(a),
		newAliasCmd(a),
		newDoctorCmd(a),
		newShellCmd(a),
		newExecCmd(a),
		newInitCmd(a),
		newVersionCmd(a),
	)
	return root
}

// historyArgPattern matches the "-2" style shorthand for walking back through
// context history.
var historyArgPattern = regexp.MustCompile(`^-([0-9]+)$`)

// normalizeArgs rewrites "-N" into "--back=N".
//
// Cobra parses "-2" as an unknown shorthand flag and fails before the command
// ever sees it, so the ergonomic form has to be translated first. A bare "-"
// is left alone: it is not flag-shaped, and the commands accept it directly.
func normalizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		if m := historyArgPattern.FindStringSubmatch(arg); m != nil {
			out = append(out, "--back="+m[1])
			continue
		}
		out = append(out, arg)
	}
	return out
}

// exitError carries a process exit status with no message of its own: the
// command has already told the user everything relevant.
type exitError struct{ code int }

func (e *exitError) Error() string { return "" }

// ExitCode maps an Execute error onto a process exit status.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *exitError
	if errors.As(err, &e) {
		return e.code
	}
	return 1
}

// Execute runs the root command against the process streams.
func Execute() error {
	root := NewRootCmd(os.Stdout, os.Stderr, os.Stdin)
	root.SetArgs(normalizeArgs(os.Args[1:]))

	if err := root.Execute(); err != nil {
		// A silent error carries an exit status only; the command already
		// printed everything the user needs.
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		return err
	}
	return nil
}
