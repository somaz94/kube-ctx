# CLAUDE.md — kube-ctx

`kctx`: a kubectx/kubens replacement with per-terminal context isolation,
production guards, a built-in fuzzy picker, and a cluster health check.

<br/>

## Build & Test

```bash
make build           # Build binary → ./bin/kctx
make test            # go test ./... -v -race -cover
make cover           # Coverage report
make cover-html      # Coverage in the browser
make fmt             # go fmt
make vet             # go vet
make install         # Install to /usr/local/bin

make e2e-cluster     # kind create cluster --name kctx-e2e
make e2e             # ./scripts/e2e.sh, against a real API server
make e2e-cluster-clean
```

<br/>

## Project Structure

```
cmd/main.go              Entry point; maps errors onto exit codes
cmd/cli/                 Cobra commands: root, ctx, ns, list, rename, delete,
                         import, export, alias, guard, current, doctor, shell,
                         exec, init, version
                         (+ util, pick, session helpers)
pkg/kubeconfig/          clientcmd-backed load / save / backup / encode
pkg/transfer/            Merge and extract contexts between kubeconfigs
pkg/contexts/            Context CRUD, switching, history stack
pkg/namespaces/          Namespace listing (live API + cache)
pkg/config/              kube-ctx's own config.yaml: aliases, guard rules
pkg/guard/               Regex classification of contexts (safe/warn/danger)
pkg/probe/               Parallel cluster health checks
pkg/picker/              The interactive fuzzy selector
pkg/shellenv/            Per-shell kubeconfig sessions and hook scripts
pkg/paths/               XDG config / cache / state directory resolution
pkg/render/              Color palette and ANSI-aware table alignment
internal/testutil/       Kubeconfig fixtures for tests
.goreleaser.yml          Multi-platform build + Homebrew tap + Scoop bucket
```

<br/>

## Release

Push a `vX.Y.Z` tag; `release.yml` runs GoReleaser and the tap and bucket update
themselves. Needs the `PAT_TOKEN` secret — the built-in `GITHUB_TOKEN` cannot
push to `somaz94/homebrew-tap`.

The archive is named after the **project** (`kube-ctx_0.1.0_darwin_arm64.tar.gz`)
and the binary inside after the **command** (`kctx`). Anything that constructs a
download URL has to keep the two apart; `scripts/install.sh` has a separate
`PROJECT` and `BINARY` for exactly this reason.

Do not edit `.goreleaser.yml`, `release.yml` or `changelog-generator.yml`
without asking — they are the release pipeline.

<br/>

## Key Concepts

- **Loader** (`pkg/kubeconfig`) — every read and write goes through
  `clientcmd`. On write that matters: with a multi-file `$KUBECONFIG`, changes
  land back in the file each stanza came from. Never re-emit the YAML by hand.
- **Backups** — `Save(cfg, WithBackup())` snapshots every kubeconfig file first.
  Used by destructive edits (rename, delete) only; a plain switch skips it.
- **Transfer** (`pkg/transfer`) — `Merge` for `import`, `Extract` for `export`,
  both in memory. Three things there are non-obvious. A colliding cluster or
  user stanza is never replaced when its contents differ — that is how
  `kubectl config view --flatten` silently repoints existing contexts at another
  API server — but one that *is* identical is reused, or importing five contexts
  that share a cluster leaves five copies. Comparison zeroes
  `LocationOfOrigin` first: clientcmd stamps every stanza with its file, so a
  raw `DeepEqual` never matches and every re-import would look like a conflict.
  And an imported stanza has that field *cleared*, because `ModifyConfig` routes
  a write by it — left set, the import is written back into the file it came
  from. `Extract` keeps it, since it is what resolves a relative certificate
  path for `--flatten`. Merge works on a `DeepCopy` and commits at the end, so a
  refused collision leaves the config untouched rather than half-imported.
  `import --overwrite` can orphan the stanzas the replaced context named, and the
  two halves of that are deliberately asymmetric: the *note* subtracts
  `Orphans.Without` so it names only what this import orphaned, while `--prune`
  removes every orphan the way `delete --prune` does. Scoping the removal too
  would make the note's own advice fail — by the time you re-run with `--prune`,
  the stanzas it named are no longer new.
- **Prompts vs payload** (`promptingOnStderr`, `cmd/cli/util.go`) — `export`
  writes a kubeconfig to stdout, so its guard prompt has to go to stderr;
  otherwise `kctx export prod > prod.yaml` puts the question in the file and
  leaves the user at a silent terminal. Every other command's prompt stays on
  stdout, where its tests and docs expect it.
- **History** (`pkg/contexts`) — a stack of contexts switched away from, backing
  `ctx -` and `ctx -N`. Scoped per shell when `KUBE_CTX_SHELL_ID` is set, and
  namespace history is additionally scoped per context.
- **Namespace cache** (`pkg/namespaces`) — a stale cache beats an empty list when
  the API server is unreachable; `Result.Source` says which was used.
- **Guards** (`pkg/config`) — rules classify a context as safe / warn / danger.
  Defaults classify and colorize but never block; the user opts into
  `confirm: true`. A rule carries exactly one matcher — `match` (regex),
  `contexts` (exact names), `prefix` or `suffix`; zero or two is an error,
  because a rule that matches everything or silently half-applies is worse than
  none. `kctx guard` writes them, prepending so a new rule beats the defaults.
  `confirm` gates every route to a cluster — `ctx`, `shell` and `exec` all call
  `requireGuardConfirmation`; covering only `ctx` left `kctx exec prod -- ...`
  walking straight past the guard.
- **Exit codes** (`cmd/cli/root.go`) — `1` kube-ctx failed, `2` doctor found a
  sick cluster, `130` the user declined. Distinct because the uses are shell
  one-liners: `&&` must not proceed past a declined guard, and `||` must not
  page on a typo'd `--kubeconfig`.
- **One resolver** (`cmd/cli/util.go`) — every command taking a context name
  goes through `resolveContext`: `.` expansion, alias, existence. Completion
  offers aliases everywhere, so a command that skipped it would suggest inputs
  it then rejects.
- **Output seam** (`cmd/cli/util.go`) — `renderOutput` picks table or JSON so a
  new command cannot accept `-o json` and ignore it. `-o plain` means
  `--no-color`; an unknown `-o` is an error, never a fallback. JSON keys are
  lowerCamel everywhere.
- **Table alignment** (`pkg/render`) — column widths are measured with ANSI
  escapes stripped, so colorized cells still line up.
- **Picker** (`pkg/picker`) — scoring, key decoding and the model are pure; the
  runner takes plain `io.Reader`/`io.Writer` plus an optional raw-mode hook, so
  a session is testable without a terminal. Weights are fzf-shaped, and
  `gapStart` deliberately exceeds `bonusBoundary` or "p-r-o" would outrank
  "prod" for the query "pro".
- **Sessions** (`pkg/shellenv`) — a per-terminal kubeconfig copy plus the shell
  function that exports `$KUBECONFIG` at it. The hook passes an env file rather
  than parsing stdout, because the picker draws on the same terminal. bash and
  zsh call the binary through the `command` builtin with an assignment prefix;
  fish uses `env`, since `env VAR=x command kctx` cannot work — `command` is a
  builtin and env can only exec a real binary. The hook also names its own
  shell in `$KUBE_CTX_SHELL`: `$SHELL` is the login shell, so deriving the
  syntax from it writes `set -gx` for a bash caller (or the reverse), which
  sources with an error and silently loses the switch.
- **A managed shell is invisible by design** — the session copy names the same
  context, so the prompt renders identically and `kube-ps1` and friends report
  no change. kube-ctx exports `$KUBE_CTX_ACTIVE` / `$KUBE_CTX_DEPTH` and prints
  the snippet on the way into the first one (`hintPrompt`), but never installs
  it: rewriting `$PS1` would fight the user's own theme.
- **The krew build cannot switch shell-locally** — `kubectl` runs a plugin as a
  subprocess, so `kubectl ctx2 ctx prod` writes the global kubeconfig no matter
  what: the hook is a shell function `kubectl` never consults. It cannot be
  detected either — `kubectl` sets no environment marker, `argv` is identical,
  and it `exec`s rather than forks, so even the parent process is the same. The
  krew `caveats` and `docs/DEPLOYMENT.md` say so; `kubectl-ctx2` called
  directly, with the hook installed, behaves normally.
- **The hook is named after `argv[0]`, not `os.Executable()`** (`invokedName`,
  `cmd/cli/init.go`) — the latter reads `/proc/self/exe` on Linux, which
  resolves symlinks. krew puts only a `kubectl-<plugin>` symlink on `$PATH`, so
  a hook built from the resolved path would define a function nothing can call.
  macOS keeps the symlink and looks fine, which is how this would have shipped
  broken on the platform most kubectl users are on.
- **Durable edits are refused in a session** — inside a managed shell
  `$KUBECONFIG` is a copy that dies with the shell, so `rename` and `delete`
  stop at `guardSessionScoped` (`cmd/cli/session.go`) rather than reporting a
  success that disappears on exit. Switching is meant to be shell-local; an
  edit is not.

<br/>

## Conventions

- Comments and documentation in English.
- New behavior lands with tests in the same change; the suite must stay at or
  above 90% coverage, excluding `cmd/main.go`.
- Commands are built by `newXxxCmd(a *app)` constructors, never package-level
  vars, so tests build an isolated tree with their own streams.
- Tests must never touch the developer's real `~/.kube/config`: use
  `internal/testutil` plus `t.Setenv` for `KUBECONFIG` and the XDG variables.
  `newHarness` (`cmd/cli/cli_test.go`) does all of it, and also stubs the picker
  and the process spawner — a test that reached `/dev/tty` would block forever.
- The e2e suite (`scripts/e2e.sh`) covers what those stubs rule out: a
  kubeconfig `kubectl` reads back, an API server that answers `doctor`, and real
  bash / zsh / fish processes sourcing the hook — where a switch written in the
  wrong shell's syntax silently fails to apply while still reporting success. It
  copies one context into a throwaway workspace and redirects `$KUBECONFIG` and
  the XDG variables there, so it never writes to the developer's kubeconfig
  either. Checks that would open the picker are skipped when a terminal exists.
