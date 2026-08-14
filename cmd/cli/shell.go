package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
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
	fmt.Fprintf(a.out, "Entering a shell pinned to %s (namespace %s). Type exit to leave.%s\n",
		pal.Bold(target), pal.Cyan(namespaceOf(cfg, target)), guardSuffix(a, target))

	cmd := exec.Command(shellPath)
	cmd.Env = append(os.Environ(), session.Env(shellenv.Depth()+1)...)
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

// newExecCmd runs one command against a context without switching to it.
func newExecCmd(a *app) *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "exec <context> -- <command> [args...]",
		Short: "Run one command against a context without switching",
		Long: "Run a single command with its kubeconfig pinned to one context.\n\n" +
			"The global kubeconfig is never written, so this is the safe way to look\n" +
			"at production from a terminal that is working on something else.",
		Args: cobra.MinimumNArgs(2),
		// Only the first argument is ours; everything after it belongs to the
		// command being run, and guessing at it would be wrong.
		ValidArgsFunction: completeContexts(a),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExec(a, args[0], args[1:], namespace)
		},
	}
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace to run in")
	registerNamespaceFlagCompletion(a, cmd)
	return cmd
}

// runExec builds a throwaway session and runs argv inside it.
func runExec(a *app, target string, argv []string, namespace string) error {
	cfg, target, err := sessionConfig(a, []string{target}, namespace)
	if err != nil {
		return err
	}

	if err := requireGuardConfirmation(a, target); err != nil {
		return err
	}

	session, err := shellenv.New(cfg, target)
	if err != nil {
		return err
	}
	defer func() { _ = session.Remove() }()

	// The badge that "kctx ctx" prints on a switch has no equivalent here, and
	// running a command against production with no indication of where it is
	// going is the thing this tool exists to stop.
	if badge := guardSuffix(a, target); badge != "" {
		fmt.Fprintf(a.errOut, "Running against %s%s\n", a.palette().Bold(target), badge)
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
