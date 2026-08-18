// Package cli wires the kube-ctx command tree.
//
// Commands are built by constructor functions rather than package-level vars so
// each test can build a fresh, isolated tree with its own input and output
// streams. The shared app struct carries everything a subcommand needs.
package cli

import (
	"bufio"
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
	"github.com/somaz94/kube-ctx/pkg/shellenv"
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

	// prompts buffers stdin across every question one command asks.
	//
	// A fresh bufio.Reader per prompt reads ahead and then throws away
	// whatever it buffered past the first newline, so the second question of
	// a command would always see EOF and read a decline. That is invisible at
	// a terminal, which delivers one line per Read, and fatal to anything
	// piping its answers in — and a command asking twice is now ordinary,
	// since a guarded context and a guarded namespace are asked separately.
	//
	// A pointer, so promptingOnStderr's copy of the app shares the position
	// rather than restarting from a drained reader.
	prompts *bufio.Reader

	// compiled memoizes the guard rules for the life of one command.
	//
	// Building them reads and parses the config file and compiles every
	// pattern, and the callers are per-item rather than per-command: a
	// fan-out over 40 contexts asks twice per target to answer the guards and
	// once more per result to badge the output. Nothing writes the config and
	// then classifies within the same command, so one compile is enough.
	compiled *guard.Classifier
}

// stdin returns the shared buffered reader, creating it on first use.
func (a *app) stdin() *bufio.Reader {
	if a.prompts == nil {
		a.prompts = bufio.NewReader(a.in)
	}
	return a.prompts
}

// loader returns a kubeconfig loader honoring --kubeconfig.
func (a *app) loader() *kubeconfig.Loader {
	return kubeconfig.New(a.opts.kubeconfig)
}

// Output formats accepted by -o.
const (
	outputColor = "color"
	outputPlain = "plain"
	outputJSON  = "json"
)

// palette returns the color palette for stdout.
//
// "-o plain" is the same request as --no-color; treating them separately is
// how the flag came to be documented and do nothing.
func (a *app) palette() render.Palette {
	return render.New(a.out, a.opts.noColor || a.opts.output == outputPlain)
}

// userConfig loads kube-ctx's own config file.
func (a *app) userConfig() (*config.Config, error) {
	return config.Load()
}

// jsonOutput reports whether the user asked for machine-readable output.
func (a *app) jsonOutput() bool { return a.opts.output == outputJSON }

// validateOutput rejects an unknown -o value.
//
// Falling back to the default is the wrong failure mode for the flag that
// carries the machine-readable contract: a script asking for "-o jsno" would
// silently receive a human table and parse it as data.
func validateOutput(format string) error {
	switch format {
	case outputColor, outputPlain, outputJSON:
		return nil
	}
	return fmt.Errorf("unknown output format %q; want one of %s, %s, %s",
		format, outputColor, outputPlain, outputJSON)
}

// classifier compiles the guard rules from the user's config.
func (a *app) classifier() (*guard.Classifier, error) {
	if a.compiled != nil {
		return a.compiled, nil
	}
	userCfg, err := a.userConfig()
	if err != nil {
		return nil, err
	}
	compiled, err := guard.New(userCfg.Guards)
	if err != nil {
		return nil, err
	}
	a.compiled = compiled
	return compiled, nil
}

// NewRootCmd builds the command tree writing to the given streams.
func NewRootCmd(out, errOut io.Writer, in io.Reader) *cobra.Command {
	a := &app{out: out, errOut: errOut, in: in}
	var rootBack int

	root := &cobra.Command{
		Use:   "kctx",
		Short: "Switch Kubernetes contexts and namespaces, safely",
		Long: "kctx — a kubectx/kubens replacement with per-terminal context isolation,\n" +
			"production guards, a built-in fuzzy picker, and a cluster health check.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Runs for every subcommand, so no command has to remember to check.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Running at all is proof this shell is alive, which is what keeps
			// the age-based sweep from deleting the kubeconfig out from under a
			// terminal that has been open a long time without switching.
			// Best-effort: a session that cannot be touched is not a reason to
			// refuse the command.
			_ = shellenv.Touch()
			return validateOutput(a.opts.output)
		},
		// Bare "kctx" is the most common thing to type, so it does what
		// "kctx ctx" does: open the picker, or list when there is no terminal.
		// A name goes straight through to the switch, which is the form
		// kubectx trained everyone's fingers on — and until it did, "kctx
		// staging" answered a context that plainly exists with "unknown
		// command", the least useful thing it could have said.
		//
		// A name that collides with a subcommand loses to it: cobra resolves
		// the tree before this runs. That is the right way round — "kctx list"
		// must keep listing — and "kctx ctx list" is the escape hatch, the
		// same shape as the "@" that forces the alias reading of a name.
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeContexts(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCtx(a, args, rootBack)
		},
	}
	// "-" and "-N" walk back through history here too. normalizeArgs already
	// rewrites "-N" into "--back=N" before cobra sees it, so without this flag
	// the root answered "kctx -2" with "unknown flag: --back".
	root.Flags().IntVarP(&rootBack, "back", "b", 0,
		"switch to the Nth previous context (same as -N)")
	root.SetOut(out)
	root.SetErr(errOut)
	root.SetIn(in)

	f := root.PersistentFlags()
	f.StringVar(&a.opts.kubeconfig, "kubeconfig", "", "path to a kubeconfig file (overrides $KUBECONFIG)")
	f.StringVarP(&a.opts.output, "output", "o", "color", "output format: color, plain, json")
	f.BoolVar(&a.opts.noColor, "no-color", false, "disable color output")
	f.BoolVarP(&a.opts.assumeYes, "yes", "y", false, "skip confirmation prompts")
	_ = root.RegisterFlagCompletionFunc("output", cobra.FixedCompletions(
		[]string{outputColor, outputPlain, outputJSON}, cobra.ShellCompDirectiveNoFileComp))

	root.AddCommand(
		newCtxCmd(a),
		newNsCmd(a),
		newCurrentCmd(a),
		newListCmd(a),
		newRenameCmd(a),
		newDeleteCmd(a),
		newImportCmd(a),
		newExportCmd(a),
		newAliasCmd(a),
		newBindCmd(a),
		newGuardCmd(a),
		newDoctorCmd(a),
		newShellCmd(a),
		newSessionsCmd(a),
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

// Exit statuses kube-ctx produces on its own behalf.
//
// They are distinct because the interesting uses of this tool are in shell
// one-liners: "kctx ctx prod && deploy" must not deploy when the guard was
// declined, and "kctx doctor prod || page" must not page because --kubeconfig
// was misspelled.
const (
	// ExitFailure is any error kube-ctx itself hit: unreadable kubeconfig,
	// unknown context, a bad guard rule.
	ExitFailure = 1
	// ExitUnhealthy is doctor's "the clusters answered, and some are sick".
	ExitUnhealthy = 2
	// ExitAborted is the user declining a confirmation or closing the picker.
	// 130 is the shell's convention for a command ended by the user.
	ExitAborted = 130
)

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
	return ExitFailure
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
