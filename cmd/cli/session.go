package cli

import (
	"fmt"
	"os"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// Environment variables kube-ctx sets for shells it manages. They are
// re-exported here so the command layer does not have to reach into shellenv
// for a name it only prints.
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
// In one of those, $KUBECONFIG points at a private copy that is deleted when
// the shell exits, so the edit would report success and then leave with the
// copy. A context switch being shell-local is the whole point; an edit meant to
// outlive the shell is not.
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
	// Sweep copies left behind by shells that were killed rather than exited.
	// Best-effort: never fail the switch the user asked for.
	_ = shellenv.GC(shellenv.DefaultMaxAge)

	// The hook sources this file in the calling shell, which is the only way a
	// child process can change its parent's environment.
	//
	// The hook says which shell it is; $SHELL is only a fallback, because it
	// names the login shell rather than the one that is about to source this.
	// Getting it wrong is silent: bash sourcing "set -gx" reports success and
	// changes nothing.
	sh, err := shellenv.ParseShell(os.Getenv(shellenv.EnvShell), os.Getenv("SHELL"))
	if err != nil {
		sh = shellenv.Bash
	}
	if err := os.WriteFile(envFile, []byte(session.Exports(sh, shellenv.Depth()+1)), 0o600); err != nil {
		_ = session.Remove()
		return false, fmt.Errorf("write shell environment: %w", err)
	}
	return true, nil
}
