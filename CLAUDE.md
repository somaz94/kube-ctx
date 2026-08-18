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
                         import, export, alias, bind, guard, current, doctor,
                         shell, sessions, exec, expiry, init, version
                         (+ util, pick, session helpers)
pkg/kubeconfig/          clientcmd-backed load / save / backup / encode
pkg/transfer/            Merge and extract contexts between kubeconfigs
pkg/contexts/            Context CRUD, switching, history stack
pkg/namespaces/          Namespace listing (live API + cache)
pkg/config/              kube-ctx's own config.yaml: aliases, guards, bindings
pkg/guard/               Regex classification of contexts (safe/warn/danger)
pkg/probe/               Parallel cluster health checks
pkg/expiry/              Parallel sweep for certificates about to run out
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
- **Session lifetime** (`pkg/shellenv`) — session copies are swept by age, and
  the age is *time since last use*: `Touch` runs from the root command's
  `PersistentPreRunE`, so any kube-ctx command in a session refreshes it.
  Without that the sweep is a live bug rather than housekeeping — nothing else
  rewrites a session copy except a context switch, so a terminal open past
  `DefaultMaxAge` without switching would have its kubeconfig deleted while
  `$KUBECONFIG` still pointed at it. `kctx sessions` surfaces the same list, and
  never removes the caller's own copy.
- **Directory bindings** (`cmd/cli/bind.go`) — `kctx bind` maps a directory to
  a context, and the shell hook runs `bind --apply` on every directory change.
  Three rules make it livable rather than bossy: it applies once per tree
  (`$KUBE_CTX_BOUND` records what this shell already acted on, so `cd` deeper
  does not undo a context picked by hand), it never switches back on the way
  out, and it refuses to auto-enter a `confirm`-guarded context — a prompt on
  every `cd` is unusable, and walking into a directory is not consent to be in
  production. `kctx shell` exports `$KUBE_CTX_PINNED` to opt out entirely.
  Binding paths are `EvalSymlinks`-resolved on both store and lookup; without
  that, macOS's `/tmp` and `/var` alone would make bindings never match.
- **Fan-out** (`cmd/cli/fanout.go`) — `exec --all` / `-c` runs one command
  against many contexts at once. The flag, not the number of contexts, decides
  the execution model: a positional context streams (stdin and both output
  streams are the terminal's, which is what makes `kubectl logs -f` work), while
  `--all` / `-c` capture output and pass no stdin, because several children
  cannot share one terminal. That is also what makes `-o json` possible here and
  impossible for the streamed form. Every guard is answered *before* the first
  child starts — a guard that fires once the command has reached half the
  clusters is not one — and the exit status is the first non-zero child in the
  order the contexts were named, since there is no single status to pass
  through and `&& ./ship` must not ship on a partial failure.
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
- **The second guard axis** (`Guard.Namespaces`) — a rule listing namespaces
  classifies those *inside* the contexts it matches and stops classifying the
  context, because one rule has one `level` and the two verdicts differ: a
  context worth badging is rarely worth prompting for, and `kube-system` is the
  reverse. The classifier keeps the axes as separate lists so "first match
  wins" stays answerable; interleaved, a namespace rule above a context rule
  would look like it shadowed it. Such a rule may omit the context matcher —
  the one exception to the no-matcher error, and only because the reasoning
  behind that error does not reach it: a namespace list is already a matcher,
  so the omitted half over-classifies nothing. `requireNamespaceGuardConfirmation`
  sits beside the context check on `ctx`, `ns`, `shell` and `exec`, and guards
  the *effective* namespace rather than the flag — a context whose own default
  is `kube-system` would otherwise walk in unguarded, and `ctx` is on the list
  because it runs nothing but decides where everything typed after it runs.
  `bind --apply` refuses a namespace-guarded context for the same reason it
  refuses a guarded one, which is also what keeps `ctx` being gated from
  turning every `cd` into a prompt. Namespace names are trimmed on both the
  read and the write path: pflag splits `-n "a, b"` without trimming, and a
  rule keyed on `" b"` looks accepted and never fires — a guard failing open.
- **One buffered stdin per command** (`app.prompts`, `cmd/cli/root.go`) — a
  fresh `bufio.Reader` per prompt reads ahead and discards everything past the
  first newline, so a second question always saw EOF and read a decline. A
  terminal hides it by delivering one line per `Read`; piped answers do not,
  and two questions in one command became ordinary once a guarded context and
  a guarded namespace started being asked separately. It is a pointer so
  `promptingOnStderr`'s copy shares the position.
- **Expiry is not doctor** (`pkg/expiry`) — `doctor` asks whether a cluster
  works now and calls a sick one a failure; `expiry` asks what breaks in N
  days, where nothing is wrong yet. Folding them together would make a
  certificate with three weeks left report a sick cluster, and would turn
  doctor — one `GET /version` per context — into a namespace-wide list that
  fails for any credential allowed to reach the API but not read secrets. The
  unit of truth is the certificate, not the resource managing it: every
  `kubernetes.io/tls` secret carries the PEM, so `notAfter` is readable with no
  CRD installed, and cert-manager `Certificate`s are folded on top keyed by the
  secret each writes — that is what says who renews it, and why a managed row
  is renamed to the Certificate rather than the secret. The first *CERTIFICATE*
  block is the leaf: a `tls.crt` carries intermediates that outlive it, so
  reading the last would call a dead certificate healthy for years, and it can
  lead with a preamble, so taking the first block of any type drops a readable
  secret in silence. Two things gate the exit status, not one: something due,
  *or* a context that could not be read — a sweep that reached nothing has not
  established that nothing is wrong there, and `kctx expiry || notify` going
  quiet when every cluster is unreachable is the failure this command exists to
  prevent. The two ways of reading nothing are not equal and the status knows
  it: the secret list is issued once, cluster-wide, with no namespaced
  fallback, so a refused one read *nothing* and counts as unknown, while a
  cert-manager failure costs only the "who renews this" column and leaves the
  status alone — every `notAfter` was already in hand. Which of the two a skip
  is lives in `Skip.Blind`, a field, and never in its text: spelled as a name
  the difference survives exactly until someone appends the reason for a better
  warning, a change that reads like tidying and passes every test. The branch
  that sets it is split out as `classifySecrets` for the same reason — `Live`
  cannot be driven without an API server, so leaving the decision inside it put
  the one thing the exit status depends on in the one function no test reaches. For the same reason
  `--all` widens only what is shown; letting it widen the threshold would exit
  2 on any cluster holding one TLS secret.
- **Exit codes** (`cmd/cli/root.go`) — `1` kube-ctx failed, `2` doctor found a
  sick cluster or expiry found something due, `130` the user declined.
  Distinct because the uses are shell one-liners: `&&` must not proceed past a declined guard, and `||` must not
  page on a typo'd `--kubeconfig`.
- **A bare name switches** (`NewRootCmd`, `cmd/cli/root.go`) — `kctx prod` is
  `kctx ctx prod`, because that is what fingers trained on kubectx type first
  and `Args: cobra.NoArgs` answered it with "unknown command" for a context
  that plainly exists. The root carries `--back` for the same reason:
  `normalizeArgs` already rewrote `-2` into `--back=2` before cobra saw it, so
  without the flag the root failed with "unknown flag" rather than walking
  history. A name colliding with a subcommand loses to it — `kctx list` has to
  keep listing — and `kctx ctx list` is the escape hatch.
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
