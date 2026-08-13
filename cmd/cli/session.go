package cli

import "os"

// Environment variables kube-ctx sets for shells it manages.
const (
	// EnvShellID identifies one kube-ctx-managed shell session. It is set by
	// "kctx shell" and by the shell hook installed with "kctx init".
	EnvShellID = "KUBE_CTX_SHELL_ID"
	// EnvActive names the context a managed shell was opened on, for prompts.
	EnvActive = "KUBE_CTX_ACTIVE"
	// EnvDepth counts nested managed shells.
	EnvDepth = "KUBE_CTX_DEPTH"
)

// historyScope returns the history partition to use.
//
// Inside a kube-ctx-managed shell each terminal has its own current context, so
// "back one context" must mean "back one in *this* terminal". Outside one, the
// context is global and so is the history.
func historyScope() string {
	return os.Getenv(EnvShellID)
}
