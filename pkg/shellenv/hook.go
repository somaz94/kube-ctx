package shellenv

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Shell identifies a supported interactive shell.
type Shell string

const (
	// Bash is GNU bash.
	Bash Shell = "bash"
	// Zsh is Z shell.
	Zsh Shell = "zsh"
	// Fish is the friendly interactive shell.
	Fish Shell = "fish"
)

// Shells lists every supported shell, for help text and completion.
var Shells = []Shell{Bash, Zsh, Fish}

// ParseShell resolves a shell name. An empty name falls back to $SHELL, which
// is passed in rather than read here so the caller stays in control of the
// environment.
func ParseShell(name, shellEnv string) (Shell, error) {
	if name == "" {
		name = filepath.Base(shellEnv)
	}
	switch Shell(strings.ToLower(name)) {
	case Bash:
		return Bash, nil
	case Zsh:
		return Zsh, nil
	case Fish:
		return Fish, nil
	}
	return "", fmt.Errorf("unsupported shell %q; supported: bash, zsh, fish", name)
}

// exportLine renders one environment assignment in sh's syntax.
func exportLine(sh Shell, key, value string) string {
	if sh == Fish {
		return fmt.Sprintf("set -gx %s %s", key, quote(sh, value))
	}
	return fmt.Sprintf("export %s=%s", key, quote(sh, value))
}

// quote makes value safe to embed in a shell script. Single quotes disable
// every expansion, and an embedded single quote is closed, escaped, reopened.
func quote(sh Shell, value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// Hook returns the shell function that makes "kctx ctx" affect only the current
// terminal.
//
// A child process cannot change its parent's environment, so the binary writes
// the exports it wants to a file and this wrapper sources them. The file path
// is passed in an environment variable rather than parsed out of stdout: the
// picker draws on the same terminal, and mixing UI with code to evaluate is how
// that kind of integration breaks.
func Hook(sh Shell, binary string) string {
	if binary == "" {
		binary = "kctx"
	}
	name := filepath.Base(binary)

	switch sh {
	case Fish:
		return fishHook(name)
	default:
		return posixHook(sh, name)
	}
}

// posixHook renders the bash and zsh wrapper.
//
// The variable is passed as an assignment prefix rather than through env(1):
// "command" is a shell builtin, and env can only exec a real binary, so
// "env VAR=x command kctx" fails with "env: command: No such file".
func posixHook(sh Shell, name string) string {
	return fmt.Sprintf(`# kube-ctx shell hook (%[2]s)
# Makes context and namespace switches local to this shell.
%[1]s() {
  local __kctx_env __kctx_status
  __kctx_env="$(mktemp "${TMPDIR:-/tmp}/kube-ctx.XXXXXXXX")" || return 1
  %[3]s="$__kctx_env" command %[1]s "$@"
  __kctx_status=$?
  if [ -s "$__kctx_env" ]; then
    . "$__kctx_env"
  fi
  rm -f "$__kctx_env"
  return $__kctx_status
}
`, name, sh, EnvFile)
}

// fishHook renders the fish wrapper.
//
// env(1) is used here rather than an assignment prefix because it also
// bypasses the function being defined: env execs the binary found on PATH, so
// the wrapper cannot call itself.
func fishHook(name string) string {
	return fmt.Sprintf(`# kube-ctx shell hook (fish)
# Makes context and namespace switches local to this shell.
function %[1]s
    set -l __kctx_dir $TMPDIR
    test -n "$__kctx_dir"; or set __kctx_dir /tmp
    set -l __kctx_env (mktemp $__kctx_dir/kube-ctx.XXXXXXXX)
    if test -z "$__kctx_env"
        return 1
    end
    env %[2]s=$__kctx_env %[1]s $argv
    set -l __kctx_status $status
    if test -s $__kctx_env
        source $__kctx_env
    end
    rm -f $__kctx_env
    return $__kctx_status
end
`, name, EnvFile)
}

// PromptHint returns a one-line snippet the user can paste to show the active
// context in their prompt. It is documentation, not something kube-ctx installs
// on its own.
func PromptHint(sh Shell) string {
	switch sh {
	case Fish:
		return fmt.Sprintf(`# Add to fish_prompt: test -n "$%[1]s"; and echo -n "[$%[1]s] "`, EnvActive)
	default:
		return fmt.Sprintf(`# Add to PS1: ${%[1]s:+[$%[1]s] }`, EnvActive)
	}
}
