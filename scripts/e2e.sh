#!/usr/bin/env bash
set -euo pipefail

# End-to-end suite for kctx, run against a real Kubernetes API server.
#
# Everything checked here needs something the unit suite deliberately cannot
# have: a kubeconfig on disk that kubectl also reads back, a cluster that
# answers, and real bash/zsh/fish processes sourcing the shell hook. The unit
# tests stub the picker, the process spawner and the API — this is where those
# stubs are cashed in.
#
# Usage:
#   make e2e-cluster    # kind create cluster
#   make e2e
#   make e2e-cluster-clean
#
# The caller's own kubeconfig is never written to. The suite copies the single
# context it is pointed at into a throwaway workspace and redirects $KUBECONFIG
# and the three XDG directories there for the whole run.
#
# Environment:
#   KCTX            path to the binary under test (default ./bin/kctx)
#   E2E_CONTEXT     context to copy (default: the current one)
#   KCTX_E2E_KEEP   set to keep the workspace directory for debugging
#   NO_COLOR        set to disable color

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
KCTX_BIN="${KCTX:-$REPO_ROOT/bin/kctx}"

# Context names the suite creates. They are fixed rather than derived from the
# source cluster so every assertion below can name them literally, and they are
# chosen to exercise the built-in guard patterns: prod- is DANGER, staging- is
# WARN, and live- matches nothing.
LIVE="live-e2e"
STAGING="staging-e2e"
PROD="prod-e2e"

RED='\033[31m'
GREEN='\033[32m'
YELLOW='\033[33m'
CYAN='\033[36m'
BOLD='\033[1m'
RESET='\033[0m'
if [ -n "${NO_COLOR:-}" ]; then
  RED='' GREEN='' YELLOW='' CYAN='' BOLD='' RESET=''
fi

PASSED=0
FAILED=0
SKIPPED=0
E2E_STATUS=0
E2E_OUTPUT=""
HOOK_SESSIONS=""
HOOK_SHELLS=0

# --- reporting ---------------------------------------------------------------

die() {
  printf "${RED}e2e: %s${RESET}\n" "$1" >&2
  exit 1
}

section() { printf "\n${BOLD}${CYAN}== %s${RESET}\n" "$1"; }
info() { printf "   %s\n" "$1"; }

pass() {
  PASSED=$((PASSED + 1))
  printf "  ${GREEN}PASS${RESET} %s\n" "$1"
}

skip() {
  SKIPPED=$((SKIPPED + 1))
  printf "  ${YELLOW}SKIP${RESET} %s — %s\n" "$1" "$2"
}

fail() {
  FAILED=$((FAILED + 1))
  printf "  ${RED}FAIL${RESET} %s\n" "$1"
  printf "       %s\n" "$2"
  if [ -n "$E2E_OUTPUT" ]; then
    printf '%b\n' "       ${RED}--- last command output ---${RESET}"
    printf '%s\n' "$E2E_OUTPUT" | sed 's/^/       /'
  fi
}

# --- running and asserting ---------------------------------------------------

# capture runs a command, recording its combined output and exit status without
# tripping set -e. Never call it from a pipeline: a pipeline element runs in a
# subshell and the recorded values would be discarded with it. Feed input by
# redirecting a file instead.
capture() {
  E2E_STATUS=0
  E2E_OUTPUT="$("$@" 2>&1)" || E2E_STATUS=$?
}

assert_status() {
  if [ "$E2E_STATUS" -eq "$1" ]; then
    pass "$2"
  else
    fail "$2" "expected exit status $1, got $E2E_STATUS"
  fi
}

assert_contains() {
  case "$E2E_OUTPUT" in
  *"$1"*) pass "$2" ;;
  *) fail "$2" "expected the output to contain: $1" ;;
  esac
}

assert_not_contains() {
  case "$E2E_OUTPUT" in
  *"$1"*) fail "$2" "expected the output NOT to contain: $1" ;;
  *) pass "$2" ;;
  esac
}

assert_eq() {
  if [ "$1" = "$2" ]; then
    pass "$3"
  else
    fail "$3" "expected \"$1\", got \"$2\""
  fi
}

# --- kubeconfig probes -------------------------------------------------------

current_context() { kubectl config current-context 2>/dev/null || true; }

context_names() { kubectl config view -o jsonpath='{.contexts[*].name}'; }

context_namespace() {
  kubectl config view -o jsonpath="{.contexts[?(@.name==\"$1\")].context.namespace}"
}

# state_count counts the files kube-ctx has written under one state directory.
# The directory is created lazily — backups/ does not exist until the first
# destructive edit — and under "set -o pipefail" a find that cannot open it
# would fail the whole script rather than answering zero.
state_count() {
  local dir="$XDG_STATE_HOME/kube-ctx/$1"
  if [ ! -d "$dir" ]; then
    echo 0
    return 0
  fi
  find "$dir" -type f | wc -l | tr -d ' '
}
session_count() { state_count shells; }
backup_count() { state_count backups; }

# has_tty reports whether a controlling terminal is reachable. The picker opens
# /dev/tty directly, so the commands that fall back to plain listing only do so
# where there is no terminal — in CI. Run from a real shell they would open the
# picker and block forever, so those checks are skipped instead.
has_tty() { (exec 3<>/dev/tty) 2>/dev/null; }

# --- setup -------------------------------------------------------------------

setup() {
  command -v kubectl >/dev/null 2>&1 || die "kubectl is required"
  command -v jq >/dev/null 2>&1 || die "jq is required"
  [ -x "$KCTX_BIN" ] || die "no kctx binary at $KCTX_BIN — run: make build"

  local source_context
  source_context="${E2E_CONTEXT:-$(kubectl config current-context 2>/dev/null || true)}"
  [ -n "$source_context" ] || die "no current context — create one with: make e2e-cluster"

  # The kubeconfig is only ever copied, but the copy still points at the real
  # cluster and the suite calls it. Running "make e2e" with production current
  # is the easy mistake, so refuse the contexts kube-ctx's own default rules
  # would badge DANGER. The pattern is the danger rule from
  # pkg/config.DefaultGuards.
  if printf '%s' "$source_context" | grep -Eq '(^|[-_.])(prod|prd|production)([-_.]|$)'; then
    [ -n "${E2E_ALLOW_DANGER:-}" ] || die "refusing to run against \"$source_context\": the default guard rules
    classify it as production, and this suite calls a live API server. Point it
    at a throwaway cluster instead:

      make e2e-cluster && E2E_CONTEXT=kind-kctx-e2e make e2e

    Set E2E_ALLOW_DANGER=1 if you really mean this one."
  fi

  WORK="$(mktemp -d "${TMPDIR:-/tmp}/kctx-e2e.XXXXXXXX")"
  if [ -n "${KCTX_E2E_KEEP:-}" ]; then
    trap 'printf "\nworkspace kept at %s\n" "$WORK"' EXIT
  else
    trap 'rm -rf "$WORK"' EXIT
  fi

  mkdir -p "$WORK/bin" "$WORK/config" "$WORK/cache" "$WORK/state"

  # The hook defines a shell function named after the binary and calls it
  # through PATH, so the binary has to be reachable as "kctx" — not as
  # ./bin/kctx. This also makes os.Executable() resolve to the right name.
  ln -s "$KCTX_BIN" "$WORK/bin/kctx"
  PATH="$WORK/bin:$PATH"
  export PATH
  export XDG_CONFIG_HOME="$WORK/config"
  export XDG_CACHE_HOME="$WORK/cache"
  export XDG_STATE_HOME="$WORK/state"

  # $SHELL decides what "kctx shell" spawns. Pin it so the suite does not
  # depend on the developer's login shell.
  export SHELL=/bin/bash

  kubectl config view --raw --minify --context "$source_context" >"$WORK/kubeconfig"
  chmod 600 "$WORK/kubeconfig"
  export KUBECONFIG="$WORK/kubeconfig"

  kubectl config rename-context "$source_context" "$LIVE" >/dev/null
  local cluster user
  cluster="$(kubectl config view -o jsonpath="{.contexts[?(@.name==\"$LIVE\")].context.cluster}")"
  user="$(kubectl config view -o jsonpath="{.contexts[?(@.name==\"$LIVE\")].context.user}")"
  [ -n "$cluster" ] && [ -n "$user" ] || die "could not read the cluster and user of $source_context"

  kubectl config set-context "$LIVE" --namespace=default >/dev/null
  # Points at the same live cluster: switching to it has to keep working.
  kubectl config set-context "$STAGING" --cluster="$cluster" --user="$user" --namespace=default >/dev/null
  # Deliberately unreachable, so doctor has something to be unhealthy about.
  kubectl config set-cluster offline-e2e --server="https://127.0.0.1:1" >/dev/null
  kubectl config set-context "$PROD" --cluster=offline-e2e --user="$user" --namespace=default >/dev/null
  kubectl config use-context "$LIVE" >/dev/null

  : >"$WORK/no-answer"

  info "binary:    $KCTX_BIN"
  info "source:    $source_context"
  info "workspace: $WORK"
}

# --- checks ------------------------------------------------------------------

check_basics() {
  section "Reading a real kubeconfig"

  capture kctx version
  assert_status 0 "version runs"

  capture kctx list
  assert_status 0 "list exits 0"
  assert_contains "$LIVE" "list shows the live context"
  assert_contains "DANGER" "list badges $PROD as DANGER"
  assert_contains "WARN" "list badges $STAGING as WARN"

  capture kctx list -o json
  assert_status 0 "list -o json exits 0"
  local names
  names="$(printf '%s' "$E2E_OUTPUT" | jq -r '[.[].name] | sort | join(",")')"
  assert_eq "$LIVE,$PROD,$STAGING" "$names" "list -o json keys are lowerCamel"

  capture kctx list -o jsno
  assert_status 1 "an unknown -o value is an error, not a silent fallback"
  assert_contains "unknown output format" "... and names the offending value"

  capture kctx current
  assert_eq "$LIVE" "$E2E_OUTPUT" "current prints the current context"

  capture kctx current -n
  assert_eq "default" "$E2E_OUTPUT" "current -n prints the namespace"

  capture kctx current -o json
  assert_eq "$LIVE" "$(printf '%s' "$E2E_OUTPUT" | jq -r .context)" "current -o json reports the context"
}

check_switching() {
  section "Switching, written back through clientcmd"

  capture kctx ctx "$STAGING"
  assert_status 0 "ctx switches"
  assert_contains "WARN" "... and badges a guarded context"
  assert_eq "$STAGING" "$(current_context)" "kubectl reads back the switch"

  capture kctx ctx -
  assert_status 0 "ctx - walks back through history"
  assert_eq "$LIVE" "$(current_context)" "... to the previous context"

  capture kctx ns kube-system
  assert_status 0 "ns switches namespace"
  assert_eq "kube-system" "$(context_namespace "$LIVE")" "kubectl reads back the namespace"

  capture kctx ns -
  assert_status 0 "ns - restores the previous namespace"
  assert_eq "default" "$(context_namespace "$LIVE")" "... and kubectl agrees"

  if has_tty; then
    skip "no-argument ctx and ns fall back to listing" "a terminal is present; the picker would open"
  else
    capture kctx ctx
    assert_status 0 "ctx with no argument and no terminal lists contexts"
    assert_contains "$PROD" "... naming every context"

    capture kctx ns
    assert_status 0 "ns with no argument lists namespaces from the live API server"
    assert_contains "kube-system" "... including kube-system"

    if [ -d "$XDG_CACHE_HOME/kube-ctx" ]; then
      pass "the namespace list is cached under XDG_CACHE_HOME"
    else
      fail "the namespace list is cached under XDG_CACHE_HOME" "no cache directory was written"
    fi
  fi
}

check_doctor() {
  section "doctor, against a cluster that answers"

  capture kctx doctor "$LIVE"
  assert_status 0 "doctor exits 0 for a healthy cluster"
  assert_contains "ok" "... and reports it ok"

  capture kctx doctor "$LIVE" -o json
  assert_status 0 "doctor -o json exits 0"
  local reachable version
  reachable="$(printf '%s' "$E2E_OUTPUT" | jq -r '.[0].reachable')"
  version="$(printf '%s' "$E2E_OUTPUT" | jq -r '.[0].serverVersion // ""')"
  assert_eq "true" "$reachable" "the live cluster is reported reachable"
  if [ -n "$version" ]; then
    pass "the server version came back from the API ($version)"
  else
    fail "the server version came back from the API" "serverVersion was empty"
  fi

  capture kctx doctor --timeout 5s
  assert_status 2 "doctor exits 2 when a context is unhealthy"
  assert_contains "$PROD" "... naming the unreachable context"

  capture kctx doctor --unhealthy --timeout 5s -o json
  local unhealthy
  unhealthy="$(printf '%s' "$E2E_OUTPUT" | jq -r '[.[].context] | sort | join(",")')"
  assert_eq "$PROD" "$unhealthy" "--unhealthy reports only the sick context"
}

check_exec() {
  section "exec, isolated from the global kubeconfig"

  capture kctx exec "$STAGING" -- kubectl config current-context
  assert_status 0 "exec runs the command"
  assert_contains "$STAGING" "... against the named context"
  assert_eq "$LIVE" "$(current_context)" "... while the global kubeconfig stays put"

  capture kctx exec "$LIVE" -- kubectl get namespace kube-system
  assert_status 0 "exec reaches the live API server"

  capture kctx exec "$LIVE" -- sh -c "exit 7"
  assert_status 7 "exec passes the command's own exit status through"

  assert_eq "0" "$(session_count)" "exec leaves no session kubeconfig behind"
}

check_shell() {
  section "shell, a subshell pinned to one context"

  cat >"$WORK/shell-input" <<EOF
echo "active=\$KUBE_CTX_ACTIVE"
echo "depth=\$KUBE_CTX_DEPTH"
echo "kubeconfig=\$KUBECONFIG"
echo "session=\$(kubectl config current-context)"
kctx delete $LIVE --yes
echo "delete-status=\$?"
exit 0
EOF

  capture kctx shell "$STAGING" <"$WORK/shell-input"
  assert_status 0 "shell spawns and exits cleanly"
  assert_contains "active=$STAGING" "... exporting KUBE_CTX_ACTIVE"
  assert_contains "depth=1" "... and KUBE_CTX_DEPTH"
  assert_contains "/kube-ctx/shells/" "... with KUBECONFIG pointed at a session copy"
  assert_contains "session=$STAGING" "... whose current-context is the pinned one"
  assert_contains "delete-status=1" "a durable edit inside the session is refused"
  assert_contains "discarded when the shell exits" "... and says why"
  assert_eq "$LIVE" "$(current_context)" "the global kubeconfig is untouched by the session"
  assert_eq "0" "$(session_count)" "the session copy is removed on exit"
}

# hook_probe writes and runs a script that installs the shell hook in a real
# shell, switches context, and reports what the shell ended up with.
hook_probe() {
  local sh="$1"
  local script="$WORK/hook-$sh"

  if [ "$sh" = "fish" ]; then
    cat >"$script" <<EOF
kctx init fish --no-completion | source
kctx ctx $STAGING >/dev/null
echo "kubeconfig=\$KUBECONFIG"
echo "active=\$KUBE_CTX_ACTIVE"
echo "depth=\$KUBE_CTX_DEPTH"
echo "context="(kubectl config current-context)
EOF
  else
    cat >"$script" <<EOF
eval "\$(kctx init $sh --no-completion)"
kctx ctx $STAGING >/dev/null
echo "kubeconfig=\$KUBECONFIG"
echo "active=\$KUBE_CTX_ACTIVE"
echo "depth=\$KUBE_CTX_DEPTH"
echo "context=\$(kubectl config current-context)"
EOF
  fi

  capture "$sh" "$script"
}

check_hook() {
  section "The shell hook, in real bash, zsh and fish"

  local sh session
  for sh in bash zsh fish; do
    if ! command -v "$sh" >/dev/null 2>&1; then
      skip "$sh hook keeps the switch inside the shell" "$sh is not installed"
      continue
    fi

    hook_probe "$sh"
    assert_status 0 "$sh: the hook shell exits cleanly"
    assert_contains "active=$STAGING" "$sh: the hook exports KUBE_CTX_ACTIVE"
    assert_contains "depth=1" "$sh: the hook exports KUBE_CTX_DEPTH"
    # The one that matters. When the exports are written in the wrong shell's
    # syntax, sourcing them fails, $KUBECONFIG keeps pointing at the global
    # file, and the command still reports a successful switch — the v0.1.0 bug.
    assert_contains "/kube-ctx/shells/" "$sh: KUBECONFIG is repointed at a session copy"
    assert_contains "context=$STAGING" "$sh: kubectl inside the shell sees the new context"
    assert_eq "$LIVE" "$(current_context)" "$sh: the global kubeconfig is untouched"

    session="$(printf '%s\n' "$E2E_OUTPUT" | sed -n 's/^kubeconfig=//p')"
    HOOK_SESSIONS="$HOOK_SESSIONS$session
"
    HOOK_SHELLS=$((HOOK_SHELLS + 1))
  done

  if [ "$HOOK_SHELLS" -gt 0 ]; then
    local distinct
    distinct="$(printf '%s' "$HOOK_SESSIONS" | sort -u | grep -c . || true)"
    assert_eq "$HOOK_SHELLS" "$distinct" \
      "each of the $HOOK_SHELLS shells got a kubeconfig copy of its own"
  fi
}

check_guard() {
  section "Guards, on every route to a cluster"

  capture kctx guard list
  assert_status 0 "guard list exits 0"
  assert_contains "danger" "... showing the built-in rules"
  assert_contains "no rules are configured yet" "... and saying they are defaults"

  capture kctx guard add "$PROD" --confirm
  assert_status 0 "guard add records an exact-name rule"

  capture kctx guard list -o json
  assert_eq "true" "$(printf '%s' "$E2E_OUTPUT" | jq -r '.[0].confirm')" \
    "the new rule is prepended, so it wins over the defaults"

  capture kctx ctx "$PROD" <"$WORK/no-answer"
  assert_status 130 "declining the guard exits 130"
  assert_contains "Aborted." "... and says so"
  assert_eq "$LIVE" "$(current_context)" "... without switching"

  # The v0.2.0 fix: covering only "ctx" left these two walking past the guard.
  capture kctx exec "$PROD" -- true <"$WORK/no-answer"
  assert_status 130 "the guard also gates exec"

  capture kctx shell "$PROD" <"$WORK/no-answer"
  assert_status 130 "the guard also gates shell"

  capture kctx exec -y "$PROD" -- true
  assert_status 0 "-y skips the guard, for scripts"

  printf '%s\n' "$PROD" >"$WORK/answer"
  capture kctx ctx "$PROD" <"$WORK/answer"
  assert_status 0 "retyping the context name passes the guard"
  assert_eq "$PROD" "$(current_context)" "... and the switch lands"

  capture kctx guard remove 1
  assert_status 0 "guard remove drops the rule"

  capture kctx ctx "$LIVE"
  assert_status 0 "with the rule gone the switch is not gated"
  assert_eq "$LIVE" "$(current_context)" "... and lands without a prompt"
}

check_alias() {
  section "Aliases, resolved everywhere a context name is taken"

  capture kctx alias p "$STAGING"
  assert_status 0 "alias records a short name"

  capture kctx alias -o json
  assert_eq "$STAGING" "$(printf '%s' "$E2E_OUTPUT" | jq -r '.[0].target')" "alias -o json reports the target"

  capture kctx ctx @p
  assert_status 0 "an alias is accepted where a context name is"
  assert_eq "$STAGING" "$(current_context)" "... and resolves to the right context"

  capture kctx doctor @p --timeout 5s
  assert_status 0 "doctor resolves the alias too"

  capture kctx alias -d p
  assert_status 0 "alias -d removes it"

  capture kctx ctx "$LIVE"
  assert_status 0 "back to the live context"
}

check_edits() {
  section "Durable edits, with a backup taken first"

  local before after
  before="$(backup_count)"

  capture kctx rename "$STAGING" stg-renamed
  assert_status 0 "rename rewrites the kubeconfig"
  case " $(context_names) " in
  *" stg-renamed "*) pass "kubectl reads back the new name" ;;
  *) fail "kubectl reads back the new name" "context list: $(context_names)" ;;
  esac

  after="$(backup_count)"
  if [ "$after" -gt "$before" ]; then
    pass "rename took a backup first ($before to $after)"
  else
    fail "rename took a backup first" "backup count stayed at $before"
  fi

  capture kctx delete stg-renamed --yes
  assert_status 0 "delete removes the context"
  case " $(context_names) " in
  *" stg-renamed "*) fail "the context is gone from the kubeconfig" "still present: $(context_names)" ;;
  *) pass "the context is gone from the kubeconfig" ;;
  esac

  capture kctx doctor "$LIVE" --timeout 5s
  assert_status 0 "the kubeconfig is still usable after the edits"
}

# --- main --------------------------------------------------------------------

main() {
  printf '%b\n' "${BOLD}kctx end-to-end suite${RESET}"
  setup

  check_basics
  check_switching
  check_doctor
  check_exec
  check_shell
  check_hook
  check_guard
  check_alias
  check_edits

  printf "\n${BOLD}%d passed, %d failed, %d skipped${RESET}\n" "$PASSED" "$FAILED" "$SKIPPED"
  if [ "$FAILED" -gt 0 ]; then
    printf '%b\n' "${RED}e2e failed${RESET}"
    return 1
  fi
  printf '%b\n' "${GREEN}e2e passed${RESET}"
}

main "$@"
