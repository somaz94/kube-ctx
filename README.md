# kube-ctx

[![CI](https://github.com/somaz94/kube-ctx/actions/workflows/ci.yml/badge.svg)](https://github.com/somaz94/kube-ctx/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Latest Tag](https://img.shields.io/github/v/tag/somaz94/kube-ctx)](https://github.com/somaz94/kube-ctx/tags)
[![Top Language](https://img.shields.io/github/languages/top/somaz94/kube-ctx)](https://github.com/somaz94/kube-ctx)

`kctx` — a single-binary replacement for `kubectx` and `kubens` that fixes the three things which make them risky once you have more than a couple of clusters: a current-context every terminal shares, no guard rail in front of production, and an external `fzf` dependency for the picker.

> For detailed documentation, see the [docs/](docs/) folder:
>
> [Usage](docs/USAGE.md) |
> [Configuration](docs/CONFIGURATION.md) |
> [Examples](docs/EXAMPLES.md) |
> [Use Cases](docs/USE-CASES.md) |
> [Deployment](docs/DEPLOYMENT.md) |
> [Development](docs/DEVELOPMENT.md)

<br/>

## Why kube-ctx?

| | `kubectx` + `kubens` | `kctx` |
|---|---|---|
| **Scope of a switch** | Global — every terminal follows | Per-terminal via the shell hook or `kctx shell`, global otherwise |
| **Production guard** | None | Regex-classified contexts, colored, optionally confirm-to-switch |
| **Interactive picker** | Needs `fzf` on `$PATH` | Built in — no external dependency |
| **Run against another cluster** | Switch, run, switch back | `kctx exec prod -- kubectl get pods` |
| **Namespace list offline** | Fails when the API server is unreachable | Falls back to a cache and says so |
| **Cluster health** | — | `kctx doctor`: reachability, version, expired certs and tokens |
| **kubeconfig writes** | Re-emits the YAML | `clientcmd`, the same path `kubectl` uses — multi-file `$KUBECONFIG` safe |
| **Backups** | — | Automatic before every destructive edit |
| **Binaries** | Two | One |

<br/>

## The problem it solves

A kubeconfig has exactly one `current-context`. Switch it in one tab and every other tab follows — including the one where you are halfway through a production incident. `kctx` gives each terminal its own copy of the kubeconfig and points `$KUBECONFIG` at it:

```bash
# terminal A                     # terminal B
$ kctx ctx prod-eks              $ kubectl config current-context
Switched to context prod-eks.    dev-cluster        # unchanged
```

That behavior needs a shell hook, because a child process cannot change its parent's environment. One line in your rc file installs it:

```bash
eval "$(kctx init zsh)"     # or bash; fish: kctx init fish | source
```

Without the hook, `kctx` edits the global kubeconfig exactly the way `kubectx` does — nothing breaks, you just do not get the isolation.

<br/>

## Quick Start

### Install

```bash
# From source
git clone https://github.com/somaz94/kube-ctx.git
cd kube-ctx
make build && make install       # → /usr/local/bin/kctx
```

<br/>

### Use

```bash
kctx                             # (via ctx) interactive picker
kctx ctx prod-eks                # switch
kctx ctx -                       # back to the previous context
kctx ns kube-system              # namespace of the current context
kctx list --wide                 # everything at a glance
kctx doctor                      # what still works?
kctx exec prod-eks -- kubectl get nodes    # one command, no switch
kctx shell prod-eks              # a subshell pinned to prod
```

<br/>

## Commands

| Command | What it does |
|---|---|
| `kctx ctx [name\|-\|-N]` | Switch context. No argument opens the picker; `-` goes back |
| `kctx ns [name\|-\|-N]` | Switch the namespace of the current context |
| `kctx list [--wide]` | Table of every context, with guard badges |
| `kctx rename <old> <new>` | Rename a context (`.` = current) |
| `kctx delete <name>... [--prune]` | Delete contexts, optionally their orphaned cluster/user entries |
| `kctx alias <name> <context>` | Short names usable anywhere a context is |
| `kctx doctor [context...]` | Parallel health check; non-zero exit if anything is broken |
| `kctx shell [context]` | Subshell pinned to a context |
| `kctx exec <context> -- <cmd>` | Run one command against a context |
| `kctx init bash\|zsh\|fish` | Shell hook + completions |
| `kctx version` | Build information |

Global flags: `--kubeconfig`, `-o color\|plain\|json`, `--no-color`, `-y/--yes`.

<br/>

## Production guards

Contexts are classified by regular expression. Out of the box `prod`, `prd` and `production` are marked **DANGER**, `staging`/`uat` **WARN** — labels only, nothing blocks. Turn a rule into a real gate in `~/.config/kube-ctx/config.yaml`:

```yaml
guards:
  - match: '(^|[-_.])(prod|production)([-_.]|$)'
    level: danger
    confirm: true          # retype the context name to switch to it

aliases:
  p: prod-eks-apne2
```

```
$ kctx ctx prod-eks-apne2
! prod-eks-apne2 is classified danger by the guard rule (^|[-_.])(prod|production)([-_.]|$).
Type "prod-eks-apne2" to continue:
```

See [Configuration](docs/CONFIGURATION.md) for every field.

<br/>

## Where things are stored

| Path | Contents |
|---|---|
| `$XDG_CONFIG_HOME/kube-ctx/config.yaml` | aliases, guard rules |
| `$XDG_STATE_HOME/kube-ctx/history*` | context and namespace history |
| `$XDG_STATE_HOME/kube-ctx/backups/` | kubeconfig snapshots, 10 generations |
| `$XDG_STATE_HOME/kube-ctx/shells/` | per-terminal kubeconfig copies |
| `$XDG_CACHE_HOME/kube-ctx/namespaces/` | namespace list cache |

Everything is created `0600`/`0700` — the copies hold credentials. Nothing is written to `~/.kube/` except the kubeconfig edit you asked for, and destructive edits are backed up first.

<br/>

## License

Apache 2.0 — see [LICENSE](LICENSE).
