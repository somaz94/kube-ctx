package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// runCommand executes a prepared command. It is a variable so tests can run the
// shell and exec commands without spawning anything.
var runCommand = func(cmd *exec.Cmd) error { return cmd.Run() }

// newShellCmd opens a subshell pinned to one context.
func newShellCmd(a *app) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "shell [context]",
		Short: "Open a subshell pinned to a context",
		Long: "Open a subshell whose kubeconfig is a private copy pinned to one context.\n\n" +
			"Nothing outside this shell changes: the global kubeconfig is never\n" +
			"written, so other terminals keep the context they were on. The copy is\n" +
			"deleted when the shell exits.\n\n" +
			"$KUBE_CTX_ACTIVE names the context inside the shell, for prompts.",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeContexts(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShell(a, args, namespace)
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace to start in")
	registerNamespaceFlagCompletion(a, cmd)
	return cmd
}

// runShell prepares a session kubeconfig and spawns the user's shell on it.
func runShell(a *app, args []string, namespace string) error {
	cfg, target, err := sessionConfig(a, args, namespace)
	if err != nil {
		return err
	}

	if err := requireGuardConfirmation(a, target); err != nil {
		return err
	}
	// The namespace the shell opens in, whether -n named it or the context
	// already pointed there. Guarding only the flag would mean a context whose
	// own default is kube-system walks in unguarded.
	ns := namespaceOf(cfg, target)
	if err := requireNamespaceGuardConfirmation(a, target, ns); err != nil {
		return err
	}

	session, err := shellenv.New(cfg, target)
	if err != nil {
		return err
	}
	defer func() { _ = session.Remove() }()

	// Sweep session files left behind by shells that were killed rather than
	// exited. Best-effort: never fail the command the user asked for.
	_ = shellenv.GC(shellenv.DefaultMaxAge)

	shellPath := os.Getenv("SHELL")
	if shellPath == "" {
		shellPath = "/bin/sh"
	}

	pal := a.palette()
	fmt.Fprintf(a.out, "Entering a shell pinned to %s (namespace %s%s). Type exit to leave.%s\n",
		pal.Bold(target), pal.Cyan(ns), namespaceGuardSuffix(a, target, ns), guardSuffix(a, target))
	hintPrompt(a, shellPath)

	cmd := exec.Command(shellPath)
	// Pinned: this shell asked for one context and is entitled to keep it, so a
	// directory binding must not switch it out from under the user on a cd.
	cmd.Env = append(os.Environ(), append(session.Env(shellenv.Depth()+1), shellenv.EnvPinned+"=1")...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	// The child owns the terminal while it runs. Ctrl-C reaches the whole
	// foreground process group, so kube-ctx has to ignore it or it would exit
	// and orphan the shell the user is typing into.
	stop := ignoreInterrupts()
	err = runCommand(cmd)
	stop()

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// The shell's own exit status is not a kube-ctx failure. Leaving a
			// shell with Ctrl-D after a failed command is entirely normal.
			return nil
		}
		return fmt.Errorf("start shell %s: %w", shellPath, err)
	}
	return nil
}

// execOptions holds the flags of the exec command.
type execOptions struct {
	namespace string
	contexts  []string
	all       bool
	parallel  int
}

// newExecCmd runs one command against one context, or against many at once.
func newExecCmd(a *app) *cobra.Command {
	var opts execOptions

	cmd := &cobra.Command{
		Use:   "exec <context> -- <command> [args...]",
		Short: "Run one command against a context without switching",
		Long: "Run a single command with its kubeconfig pinned to one context.\n\n" +
			"The global kubeconfig is never written, so this is the safe way to look\n" +
			"at production from a terminal that is working on something else.\n\n" +
			"--all and --context run against several contexts at once:\n\n" +
			"  kctx exec --all -- kubectl get nodes\n" +
			"  kctx exec -c dev,staging -- kubectl get deploy -n api\n\n" +
			"Which one you use decides how the command runs, not how many contexts\n" +
			"it ends up with. A named context streams: stdin, stdout and stderr are\n" +
			"the terminal's, so \"kctx exec prod -- kubectl logs -f\" works. --all and\n" +
			"--context capture the output and print it per context once each command\n" +
			"finishes, and pass no stdin — several children cannot share a terminal,\n" +
			"and interleaved lines from four clusters are unreadable.\n\n" +
			"Every guard is answered before anything runs. The exit status is the\n" +
			"command's own; where several ran, it is the first non-zero one.",
		// Checked in RunE rather than here: what counts as enough arguments
		// depends on whether the contexts came from a flag, and cobra's own
		// "requires at least 1 arg(s)" says nothing about which one is missing.
		Args: cobra.ArbitraryArgs,
		// Only the first argument is ours; everything after it belongs to the
		// command being run, and guessing at it would be wrong.
		ValidArgsFunction: completeContexts(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExecCmd(a, cmd, args, opts)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&opts.namespace, "namespace", "n", "", "namespace to run in")
	f.StringSliceVarP(&opts.contexts, "context", "c", nil, "run against these contexts at once (repeatable)")
	f.BoolVar(&opts.all, "all", false, "run against every context at once")
	f.IntVarP(&opts.parallel, "parallel", "p", defaultFanoutParallel, "how many contexts to run at once")
	cmd.MarkFlagsMutuallyExclusive("all", "context")
	registerNamespaceFlagCompletion(a, cmd)
	_ = cmd.RegisterFlagCompletionFunc("context", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return contextCandidates(a, nil)
	})
	return cmd
}

// runExecCmd splits the command line and dispatches to the single-context or
// the fan-out path.
func runExecCmd(a *app, cmd *cobra.Command, args []string, opts execOptions) error {
	if !opts.all && len(opts.contexts) == 0 {
		if len(args) < 2 {
			return fmt.Errorf("exec needs a context and a command, as in: kctx exec prod -- kubectl get pods")
		}
		return runExec(a, args[0], args[1:], opts.namespace)
	}

	// In fan-out the contexts came from the flags, so everything left is the
	// command. A positional name here means the two ways of choosing contexts
	// were mixed, and picking one silently would run against the wrong set.
	if dash := cmd.ArgsLenAtDash(); dash > 0 {
		return fmt.Errorf("--all and --context choose the contexts themselves; drop %q from before --",
			strings.Join(args[:dash], " "))
	}
	if len(args) == 0 {
		return fmt.Errorf("no command given, as in: kctx exec --all -- kubectl get nodes")
	}

	cfg, err := a.loader().Load()
	if err != nil {
		return err
	}
	targets, err := fanoutTargets(a, cfg, opts)
	if err != nil {
		return err
	}
	return runFanout(a, cfg, targets, args, opts)
}

// fanoutTargets resolves the contexts a fan-out runs against, deduplicated and
// in the order the user named them.
func fanoutTargets(a *app, cfg *clientcmdapi.Config, opts execOptions) ([]string, error) {
	if opts.all {
		targets := contexts.Names(cfg)
		if len(targets) == 0 {
			return nil, fmt.Errorf("no contexts found in the kubeconfig")
		}
		return targets, nil
	}
	resolved, err := resolveContexts(a, cfg, opts.contexts)
	if err != nil {
		return nil, err
	}
	return dedupe(resolved), nil
}

// runExec builds a throwaway session and runs argv inside it.
func runExec(a *app, target string, argv []string, nsFlag string) error {
	cfg, target, err := sessionConfig(a, []string{target}, nsFlag)
	if err != nil {
		return err
	}

	if err := requireGuardConfirmation(a, target); err != nil {
		return err
	}
	// Where the command will actually run, whether -n named it or the context
	// already pointed there: guarding only the flag would let a context whose
	// own default is kube-system through unguarded.
	ns := namespaceOf(cfg, target)
	if err := requireNamespaceGuardConfirmation(a, target, ns); err != nil {
		return err
	}

	session, err := shellenv.New(cfg, target)
	if err != nil {
		return err
	}
	defer func() { _ = session.Remove() }()

	// The badge that "kctx ctx" prints on a switch has no equivalent here, and
	// running a command against production with no indication of where it is
	// going is the thing this tool exists to stop. The namespace is named only
	// when it is the guarded half, so an unremarkable one adds no noise.
	pal := a.palette()
	ctxBadge, nsBadge := guardSuffix(a, target), namespaceGuardSuffix(a, target, ns)
	if ctxBadge != "" || nsBadge != "" {
		where := pal.Bold(target) + ctxBadge
		if nsBadge != "" {
			where += ", namespace " + pal.Bold(ns) + nsBadge
		}
		fmt.Fprintf(a.errOut, "Running against %s\n", where)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), session.Env(shellenv.Depth())...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, a.out, a.errOut

	// Ctrl-C reaches the whole foreground process group. Without this, dying
	// alongside the child skips the deferred Remove and strands a copy of the
	// merged kubeconfig — every cluster, token and cert — on disk until the GC
	// sweep. "kctx exec prod -- kubectl logs -f" is interrupted routinely.
	stop := ignoreInterrupts()
	err = runCommand(cmd)
	stop()

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Pass the command's own status through, unwrapped: "kctx exec ...
			// kubectl get pods" must exit the way kubectl did.
			return &exitError{code: waitStatusCode(exitErr)}
		}
		return fmt.Errorf("run %s: %w", argv[0], err)
	}
	return nil
}

// waitStatusCode turns a child's wait status into an exit code.
//
// ExitCode reports -1 when the child was terminated by a signal, and exiting
// with -1 becomes 255 — which is a real exit code some other command could
// have returned. Shells report 128+signal for this, so kube-ctx does too.
func waitStatusCode(exitErr *exec.ExitError) int {
	if code := exitErr.ExitCode(); code >= 0 {
		return code
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return 1
}

// hintPrompt says how to make a managed shell visible in the prompt.
//
// The whole value of this command is that the shell is isolated, and nothing
// about it shows: the prompt renders identically because the session copy
// names the same context. kube-ctx exports the variables a prompt needs but
// cannot install them — reaching into $PS1 would fight whatever theme the user
// already runs — so it says where they are instead.
//
// Only on the way into the first managed shell. Nesting already proved the
// point, and a hint that repeats is a hint people learn to skip.
func hintPrompt(a *app, shellPath string) {
	if shellenv.Depth() > 0 {
		return
	}
	sh, err := shellenv.ParseShell("", shellPath)
	if err != nil {
		sh = shellenv.Bash
	}
	fmt.Fprintf(a.errOut, "Your prompt will look the same in here. %s\n%s\n",
		a.palette().Dim("$"+EnvActive+" and $"+shellenv.EnvDepth+" are exported for it:"),
		a.palette().Dim("  "+shellenv.PromptHint(sh)))
}

// sessionConfig resolves the requested context and returns a config pinned to
// it, ready to be written to a session file.
func sessionConfig(a *app, args []string, namespace string) (*clientcmdapi.Config, string, error) {
	cfg, err := a.loader().Load()
	if err != nil {
		return nil, "", err
	}

	target := cfg.CurrentContext
	if len(args) == 1 && args[0] != "" {
		target, err = resolveContext(a, cfg, args[0])
		if err != nil {
			return nil, "", err
		}
	}
	if target == "" {
		return nil, "", fmt.Errorf("no context given and no current context is set")
	}
	if !contexts.Exists(cfg, target) {
		return nil, "", fmt.Errorf("no context named %q", target)
	}

	cfg.CurrentContext = target
	if namespace != "" {
		if err := contexts.SetNamespace(cfg, target, namespace); err != nil {
			return nil, "", err
		}
	}
	return cfg, target, nil
}

// namespaceOf returns the namespace a context points at, or "default".
func namespaceOf(cfg *clientcmdapi.Config, ctxName string) string {
	ns, err := contexts.Namespace(cfg, ctxName)
	if err != nil {
		return "default"
	}
	return ns
}

// ignoreInterrupts suppresses SIGINT and SIGQUIT until the returned function is
// called.
func ignoreInterrupts() func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGQUIT)
	return func() { signal.Stop(ch) }
}
