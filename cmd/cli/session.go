package cli

import (
	"fmt"
	"os"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// Environment variables kube-ctx sets for the shells it manages.
const (
	// EnvShellID identifies one kube-ctx-managed shell session.
	EnvShellID = shellenv.EnvShellID
	// EnvActive names the context a managed shell is on, for prompts.
	EnvActive = shellenv.EnvActive
)

// historyScope returns the history partition to use.
//
// Inside a kube-ctx-managed shell each terminal has its own current context, so
// "back one context" must mean "back one in *this* terminal". Outside one, the
// context is global and so is the history.
func historyScope() string {
	return os.Getenv(EnvShellID)
}

// guardSessionScoped refuses a durable kubeconfig edit attempted from inside a
// kube-ctx-managed shell.
//
// In one of those, $KUBECONFIG points at a private copy nothing reads once the
// shell exits, so the edit would report success and then be lost. A switch
// being shell-local is the whole point; an edit meant to outlive it is not.
func guardSessionScoped(op string) error {
	if !shellenv.Active() {
		return nil
	}
	return fmt.Errorf("%s would edit this shell's private kubeconfig copy (session %s), "+
		"which is discarded when the shell exits; leave the kube-ctx shell first",
		op, os.Getenv(EnvShellID))
}

// startShellSession redirects a switch into a per-shell kubeconfig copy when
// the shell hook is installed, and reports whether it did.
//
// It fires only on the first switch in a shell. Afterwards $KUBECONFIG already
// points at the copy, so an ordinary save lands in the right place — there is
// no reason to make a second one.
func startShellSession(a *app, cfg *clientcmdapi.Config, target string) (bool, error) {
	envFile := os.Getenv(shellenv.EnvFile)
	if envFile == "" || shellenv.Active() {
		return false, nil
	}

	session, err := shellenv.New(cfg, target)
	if err != nil {
		return false, err
	}
	// Sweep idle copies: no hooked terminal removes its own on exit, and a killed
	// "kctx shell" skips its Remove. Best-effort: never fail the switch over it.
	_ = shellenv.GC(shellenv.DefaultMaxAge)

	if err := os.WriteFile(envFile, []byte(session.Exports(hookShell(), shellenv.Depth()+1)), 0o600); err != nil {
		_ = session.Remove()
		return false, fmt.Errorf("write shell environment: %w", err)
	}
	return true, nil
}

// refreshActive re-exports $KUBE_CTX_ACTIVE, which startShellSession writes only
// on a shell's first switch. The switch has already landed, so a failure only
// warns. A --kubeconfig switch saved a file this shell's kubectl does not read.
func refreshActive(a *app, target string) {
	if !shellenv.Active() || a.opts.kubeconfig != "" {
		return
	}
	if err := appendExport(shellenv.EnvActive, target); err != nil {
		fmt.Fprintf(a.errOut, "warning: %v; $%s still names the previous context\n", err, shellenv.EnvActive)
	}
}

// appendExport adds one export to the file the shell hook sources after this
// command. Without the hook there is no such file, and nothing to do.
func appendExport(key, value string) error {
	envFile := os.Getenv(shellenv.EnvFile)
	if envFile == "" {
		return nil
	}
	f, err := os.OpenFile(envFile, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("write shell environment: %w", err)
	}
	if _, err := fmt.Fprintln(f, shellenv.ExportLine(hookShell(), key, value)); err != nil {
		_ = f.Close()
		return fmt.Errorf("write shell environment: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write shell environment: %w", err)
	}
	return nil
}

// hookShell is the shell the env file must be written for. $SHELL is only the
// fallback, being the login shell: wrong syntax fails to source, and the hook
// still reports kctx's success, so the switch is lost silently.
func hookShell() shellenv.Shell {
	sh, err := shellenv.ParseShell(os.Getenv(shellenv.EnvShell), os.Getenv("SHELL"))
	if err != nil {
		return shellenv.Bash
	}
	return sh
}
