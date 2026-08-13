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
```

<br/>

## Project Structure

```
cmd/main.go              Entry point
cmd/cli/                 Cobra commands: root, ctx, ns, list, rename, delete,
                         alias, version (+ util, session helpers)
pkg/kubeconfig/          clientcmd-backed load / save / backup
pkg/contexts/            Context CRUD, switching, history stack
pkg/namespaces/          Namespace listing (live API + cache)
pkg/config/              kube-ctx's own config.yaml: aliases, guard rules
pkg/paths/               XDG config / cache / state directory resolution
pkg/render/              Color palette and ANSI-aware table alignment
internal/testutil/       Kubeconfig fixtures for tests
```

<br/>

## Key Concepts

- **Loader** (`pkg/kubeconfig`) — every read and write goes through
  `clientcmd`. On write that matters: with a multi-file `$KUBECONFIG`, changes
  land back in the file each stanza came from. Never re-emit the YAML by hand.
- **Backups** — `Save(cfg, WithBackup())` snapshots every kubeconfig file first.
  Used by destructive edits (rename, delete) only; a plain switch skips it.
- **History** (`pkg/contexts`) — a stack of contexts switched away from, backing
  `ctx -` and `ctx -N`. Scoped per shell when `KUBE_CTX_SHELL_ID` is set, and
  namespace history is additionally scoped per context.
- **Namespace cache** (`pkg/namespaces`) — a stale cache beats an empty list when
  the API server is unreachable; `Result.Source` says which was used.
- **Guards** (`pkg/config`) — regex rules classify a context as
  safe / warn / danger. Defaults classify and colorize but never block; the
  user opts into `confirm: true`.
- **Table alignment** (`pkg/render`) — column widths are measured with ANSI
  escapes stripped, so colorized cells still line up.

<br/>

## Conventions

- Comments and documentation in English.
- New behavior lands with tests in the same change; the suite must stay at or
  above 90% coverage, excluding `cmd/main.go`.
- Commands are built by `newXxxCmd(a *app)` constructors, never package-level
  vars, so tests build an isolated tree with their own streams.
- Tests must never touch the developer's real `~/.kube/config`: use
  `internal/testutil` plus `t.Setenv` for `KUBECONFIG` and the XDG variables.
