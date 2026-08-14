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
// The variables are passed as assignment prefixes rather than through env(1):
// "command" is a shell builtin, and env can only exec a real binary, so
// "env VAR=x command kctx" fails with "env: command: No such file".
//
// The shell name travels with the call because the file this function sources
// has to be written in this shell's syntax, and only the hook knows which
// shell that is.
func posixHook(sh Shell, name string) string {
	return fmt.Sprintf(`# kube-ctx shell hook (%[2]s)
# Makes context and namespace switches local to this shell.
%[1]s() {
  local __kctx_env __kctx_status
  __kctx_env="$(mktemp "${TMPDIR:-/tmp}/kube-ctx.XXXXXXXX")" || return 1
  %[3]s="$__kctx_env" %[4]s=%[2]s command %[1]s "$@"
  __kctx_status=$?
  if [ -s "$__kctx_env" ]; then
    . "$__kctx_env"
  fi
  rm -f "$__kctx_env"
  return $__kctx_status
}

# Applies directory bindings (kctx bind) when the working directory changes.
__kctx_chpwd() {
  %[1]s bind --apply
}
%[5]s
# Bindings are resolved once for the directory the shell starts in, since a
# terminal opened inside a bound repository never fires a change event.
__kctx_chpwd
`, name, sh, EnvFile, EnvShell, chpwdInstall(sh))
}

// chpwdInstall renders the shell-specific way of running __kctx_chpwd on a
// directory change.
//
// zsh has a first-class hook for it. bash has none, so PROMPT_COMMAND stands in
// — it fires before every prompt rather than on every cd, which is why the
// function compares $PWD itself. Appending is guarded because sourcing the hook
// twice (a nested shell, a re-sourced rc file) would otherwise run it twice per
// prompt.
func chpwdInstall(sh Shell) string {
	if sh == Zsh {
		return `typeset -ag chpwd_functions
if [[ -z ${chpwd_functions[(r)__kctx_chpwd]} ]]; then
  chpwd_functions+=(__kctx_chpwd)
fi`
	}
	return `__kctx_last_pwd="$PWD"
__kctx_prompt_command() {
  if [ "$PWD" != "$__kctx_last_pwd" ]; then
    __kctx_last_pwd="$PWD"
    __kctx_chpwd
  fi
}
case "${PROMPT_COMMAND:-}" in
  *__kctx_prompt_command*) ;;
  *) PROMPT_COMMAND="__kctx_prompt_command${PROMPT_COMMAND:+;$PROMPT_COMMAND}" ;;
esac`
}

// fishHook renders the fish wrapper.
//
// env(1) is used here rather than an assignment prefix because it also
// bypasses the function being defined: env execs the binary found on PATH, so
// the wrapper cannot call itself.
//
// The shell name travels with the call for the same reason as in posixHook:
// $SHELL is the login shell, and a fish user who has not run chsh would
// otherwise get bash syntax written into the file this function sources.
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
    env %[2]s=$__kctx_env %[3]s=fish %[1]s $argv
    set -l __kctx_status $status
    if test -s $__kctx_env
        source $__kctx_env
    end
    rm -f $__kctx_env
    return $__kctx_status
end

# Applies directory bindings (kctx bind) when the working directory changes.
# fish has no chpwd hook; watching $PWD is the documented equivalent, and it
# fires once for the directory the shell starts in as well.
function __kctx_chpwd --on-variable PWD
    %[1]s bind --apply
end
__kctx_chpwd
`, name, EnvFile, EnvShell)
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
