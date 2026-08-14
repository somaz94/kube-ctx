# Development

<br/>

## Prerequisites

- Go 1.26+
- Make

<br/>

## Build and test

```bash
make build           # → ./bin/kctx
make test            # go test ./... -v -race -cover
make cover           # coverage report
make cover-html      # coverage in the browser
make fmt             # go fmt
make vet             # go vet
make install         # → /usr/local/bin
make clean
```

The suite is expected to stay at or above 90% overall, excluding `cmd/main.go`.

<br/>

## End-to-end tests

```bash
make e2e-cluster        # kind create cluster --name kctx-e2e
make e2e                # ./scripts/e2e.sh
make e2e-cluster-clean  # kind delete cluster --name kctx-e2e
```

Needs `kubectl`, `jq` and a cluster that answers; `zsh` and `fish` are used when
installed and skipped when not. CI runs the same three commands against kind in
`.github/workflows/test-e2e.yml`.

The suite exists for the things the unit tests deliberately cannot reach: a
kubeconfig on disk that `kubectl` reads back, an API server that answers
`doctor` and the namespace list, and real bash / zsh / fish processes sourcing
the shell hook. That last one is the reason it is worth the kind cluster —
exports written in the wrong shell's syntax fail to source, leaving the switch
silently undone while the command still reports success, and only a real shell
shows it.

It never writes to your kubeconfig. The one context it is pointed at
(`$E2E_CONTEXT`, or the current one) is copied into a throwaway workspace, and
`$KUBECONFIG` plus the three XDG variables are redirected there for the whole
run. The copy still points at the real cluster, though, and the suite calls it —
so a context the default guard rules would badge DANGER is refused outright:

```bash
E2E_CONTEXT=kind-kctx-e2e make e2e
E2E_ALLOW_DANGER=1 make e2e  # override the production refusal
KCTX_E2E_KEEP=1 make e2e     # keep the workspace to inspect after a failure
```

The no-argument `ctx` and `ns` checks are skipped when a terminal is present:
those two open the picker on `/dev/tty`, which would block waiting for a
keystroke. They run in CI, which has no terminal.

<br/>

## Layout

```
cmd/main.go              Entry point; maps errors onto exit codes
cmd/cli/                 Cobra commands: root, ctx, ns, list, rename, delete,
                         alias, doctor, shell, exec, init, version
pkg/kubeconfig/          clientcmd-backed load / save / backup
pkg/contexts/            Context CRUD, switching, history stack
pkg/namespaces/          Namespace listing (live API + cache)
pkg/config/              kube-ctx's own config.yaml: aliases, guard rules
pkg/guard/               Regex classification of contexts
pkg/probe/               Parallel health checks
pkg/picker/              The interactive fuzzy selector
pkg/shellenv/            Per-shell kubeconfig sessions and hook scripts
pkg/paths/               XDG directory resolution
pkg/render/              Color palette and ANSI-aware table alignment
internal/testutil/       Kubeconfig fixtures for tests
```

Dependencies are deliberately limited to `cobra`, `client-go`, `golang.org/x/term` and `yaml.v3`.

<br/>

## Conventions

- Comments and documentation in English.
- New behavior lands with tests in the same change.
- Commands are built by `newXxxCmd(a *app)` constructors, never package-level
  vars, so each test builds an isolated tree with its own streams.
- Tests must never touch the developer's real `~/.kube/config`. Use
  `internal/testutil` plus `t.Setenv` for `KUBECONFIG` and the XDG variables —
  `newHarness` in `cmd/cli/cli_test.go` does all of it.

<br/>

## Testing notes

**Never let a test reach the terminal.** A test binary launched from a shell has
a usable `/dev/tty`, so a picker that opened it would block forever waiting for
a keystroke. `newHarness` stubs `newPicker` to report "no terminal"; tests that
want the picker call `scriptPicker(t, "keys\r")`.

**Never let a test spawn a process.** `shell` and `exec` go through the
`runCommand` variable; tests replace it and inspect the `*exec.Cmd` that would
have run. Note that `cmd.Env` appends the session entries to the inherited
environment, so a duplicated variable must be read *last-wins*.

**The picker is tested without a TTY.** `Score`, the key decoder and `Model` are
pure; `Picker` itself takes plain `io.Reader`/`io.Writer` plus an optional
raw-mode hook, so a whole session runs against a `strings.Reader`.

**Clusters are faked with `httptest`.** `pkg/probe` and `pkg/namespaces` point a
`rest.Config` at a test server rather than requiring a cluster.

<br/>

## Adding a command

1. `cmd/cli/<name>.go` with `func newNameCmd(a *app) *cobra.Command`.
2. Register it in `NewRootCmd`.
3. Business logic goes in a `pkg/` package, not in the command — the command
   should parse flags, call, and render.
4. Tests in `cmd/cli/<name>_test.go` using `newHarness`.
5. Document it in `README.md` and `docs/USAGE.md`.

<br/>

## Release

Tags follow semver (`vX.Y.Z`). Build metadata is injected with `-ldflags` in the
Makefile.
