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

	// prompts buffers stdin across every question one command asks. A reader per
	// prompt reads ahead and drops what it buffered past the first newline, so a
	// second question saw EOF and read a decline: hidden at a terminal, which
	// delivers one line per Read, fatal to piped answers. A pointer, so
	// promptingOnStderr's copy of the app shares the position.
	prompts *bufio.Reader

	// sessionID is the session this command opened on a hooked shell's first
	// switch. The shell adopts it only once the command exits, so until then
	// $KUBE_CTX_SHELL_ID is empty, and the history the switch records would
	// land in the global stack while the next "-" reads the session's.
	sessionID string

	// compiled memoizes the guard rules for one command: callers classify per
	// item (a fan-out asks per target and again per result). It is never
	// invalidated, so rename, which rewrites the rules, compiles its own.
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
	return newPalette(a.out, a.opts.noColor || a.opts.output == outputPlain)
}

// newPalette is a variable so a test can stand in a terminal: a buffer never
// is one, and every palette built over it is already off.
var newPalette = render.New

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
			// Keeps a long-open terminal's session from being swept; best-effort,
			// so a session that cannot be touched never fails the command.
			_ = shellenv.Touch()
			return validateOutput(a.opts.output)
		},
		// Bare "kctx" and "kctx <name>" act as "kctx ctx" does, the form kubectx
		// trained everyone on. A name colliding with a subcommand loses to it, since
		// cobra resolves the tree before this runs: "kctx list" must keep listing,
		// and "kctx ctx list" is the escape hatch.
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
		newExpiryCmd(a),
		newShellCmd(a),
		newSessionsCmd(a),
		newExecCmd(a),
		newInitCmd(a),
		newVersionCmd(a),
	)
	return root
}

// historyArgPattern matches the "-N" history shorthand.
var historyArgPattern = regexp.MustCompile(`^-([0-9]+)$`)

// normalizeArgs rewrites "-N" into "--back=N", up to the argument terminator.
//
// Cobra parses "-2" as an unknown shorthand flag and fails before the command
// ever sees it, so the ergonomic form has to be translated first. A bare "-"
// is left alone: it is not flag-shaped, and the commands accept it directly.
//
// Everything after the first "--" is the child's argv and is copied verbatim:
// rewriting there turned "kctx exec dev -- kubectl logs --tail -1" into a
// --tail of "--back=1", a shorthand only kube-ctx knows.
func normalizeArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i, arg := range args {
		if arg == "--" {
			return append(out, args[i:]...)
		}
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
// Any non-zero keeps "kctx ctx prod && deploy" from deploying past a declined
// guard; the values differ so a script can tell a sick cluster (2) from
// kube-ctx failing (1) by checking $?, which "||" alone cannot do.
const (
	// ExitFailure is any error kube-ctx itself hit: unreadable kubeconfig,
	// unknown context, a bad guard rule.
	ExitFailure = 1
	// ExitUnhealthy is doctor's "the clusters answered, and some are sick", and
	// expiry's "something is due, or a context could not be read".
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
		// An exitError is silent: the command already printed what the user needs.
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		return err
	}
	return nil
}
