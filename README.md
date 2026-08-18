# kube-ctx

[![CI](https://github.com/somaz94/kube-ctx/actions/workflows/ci.yml/badge.svg)](https://github.com/somaz94/kube-ctx/actions/workflows/ci.yml)
[![E2E](https://github.com/somaz94/kube-ctx/actions/workflows/test-e2e.yml/badge.svg)](https://github.com/somaz94/kube-ctx/actions/workflows/test-e2e.yml)
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
| **Production guard** | None | Contexts classified by name, exact list, prefix or suffix; colored everywhere, and `confirm` gates `ctx`, `shell` and `exec` alike |
| **Context per project** | — | `kctx bind`: `cd` into a repo, this terminal follows |
| **Interactive picker** | Needs `fzf` on `$PATH` | Built in — no external dependency |
| **Run against another cluster** | Switch, run, switch back | `kctx exec prod -- kubectl get pods` |
| **The same question, every cluster** | A `for` loop that switches and hopes | `kctx exec --all -- kubectl get nodes`, in parallel |
| **Namespace list offline** | Fails when the API server is unreachable | Falls back to a cache and says so |
| **Cluster health** | — | `kctx doctor`: reachability, version, expired certs and tokens |
| **Adding someone's kubeconfig** | Hand-merge, or `--flatten` and hope the names do not clash | `kctx import`: colliding stanzas are disambiguated, never overwritten |
| **Handing one context over** | Edit a copy by hand | `kctx export prod --flatten -f prod.yaml` |
| **kubeconfig writes** | Re-emits the YAML | `clientcmd`, the same path `kubectl` uses — multi-file `$KUBECONFIG` safe |
| **Backups** | — | Automatic before every destructive kubeconfig edit |
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

<br/>

### Install

```bash
# Homebrew (macOS / Linux)
brew install somaz94/tap/kube-ctx

# Scoop (Windows)
scoop bucket add somaz94 https://github.com/somaz94/scoop-bucket
scoop install kube-ctx

# krew (kubectl plugin)
kubectl krew index add somaz94 https://github.com/somaz94/krew-index
kubectl krew install somaz94/ctx2

# Install script
curl -sSL https://raw.githubusercontent.com/somaz94/kube-ctx/main/scripts/install.sh | bash

# Binary
curl -sL https://github.com/somaz94/kube-ctx/releases/latest/download/kube-ctx_linux_amd64.tar.gz | tar xz
sudo mv kctx /usr/local/bin/

# From source
git clone https://github.com/somaz94/kube-ctx.git
cd kube-ctx && make build && make install       # → /usr/local/bin/kctx
```

Then, for per-terminal isolation, one line in your rc file:

```bash
eval "$(kctx init zsh)"     # bash: kctx init bash;  fish: kctx init fish | source
```

> **On the krew install**, the binary is named `kubectl-ctx2`, and switching
> through `kubectl ctx2 ctx ...` is always global — `kubectl` runs a plugin as
> a subprocess, which cannot change the shell that called it. Install the hook
> against `kubectl-ctx2` and call it directly for shell-local switching. See
> [Deployment](docs/DEPLOYMENT.md#krew-kubectl-plugin).

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
kctx exec --all -- kubectl get nodes       # every cluster, in parallel
kctx bind dev                    # cd here later, and this terminal follows
kctx shell prod-eks              # a subshell pinned to prod
```

<br/>

## Commands

| Command | What it does |
|---|---|
| `kctx ctx [name\|-\|-N]` | Switch context. No argument opens the picker; `-` goes back |
| `kctx ns [name\|-\|-N]` | Switch the namespace of the current context |
| `kctx current [-n]` | Print the current context (or namespace) and exit — for prompts |
| `kctx list [--wide]` | Table of every context, with guard badges |
| `kctx rename <old> <new>` | Rename a context (`.` = current) |
| `kctx delete\|rm\|del <name>... [--prune]` | Delete contexts, optionally their orphaned cluster/user entries |
| `kctx import <file>... [--prune]` | Merge contexts from another kubeconfig, without colliding |
| `kctx export [name]... [-f file]` | Write contexts out as a standalone kubeconfig |
| `kctx alias <name> <context>` | Short names usable anywhere a context is |
| `kctx bind [context]` | Bind a directory to a context; `cd` there switches |
| `kctx guard add\|list\|remove` | Classify a context — or a namespace inside it — as production, without writing a regex |
| `kctx doctor [context...]` | Parallel health check; non-zero exit if anything is broken |
| `kctx shell [context]` | Subshell pinned to a context |
| `kctx sessions [--clean]` | List the per-terminal kubeconfig copies, and tidy them |
| `kctx exec <context> -- <cmd>` | Run one command against a context |
| `kctx exec --all\|-c a,b -- <cmd>` | Run it against many contexts at once |
| `kctx init bash\|zsh\|fish` | Shell hook + completions |
| `kctx version` | Build information |

Global flags: `--kubeconfig`, `-o color\|plain\|json`, `--no-color`, `-y/--yes`. An unknown `-o` value is an error rather than a silent fallback, so a script asking for `-o jsno` never gets a human table to parse.

Exit status is scriptable: `1` is kube-ctx failing, `2` is `doctor` finding a sick cluster, `130` is you declining a prompt — so `kctx ctx prod && ./deploy.sh` does not deploy when you back out. See [Usage](docs/USAGE.md#exit-status).

<br/>

## Production guards

Contexts are classified by name. Out of the box `prod`, `prd` and `production` are marked **DANGER**, `staging`/`uat` **WARN** — labels only, nothing blocks. Turn a rule into a real gate in `~/.config/kube-ctx/config.yaml`:

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

The gate covers `kctx shell` and `kctx exec` too, not just switching — otherwise `kctx exec prod -- kubectl delete deploy/api` would walk straight past it. Declining exits `130`, so `kctx ctx prod && ./deploy.sh` does not deploy.

Names lie, though — the cluster that would hurt most to break is often the one called `cluster-7`. Name it directly, no regex and no editing:

```bash
kctx guard add cluster-7 --confirm --label PROD
kctx guard add --suffix -live --level danger
kctx guard list
```

A rule can also list `namespaces:`, which moves it onto the other axis: it then guards those namespaces *inside* the contexts it matches, rather than the contexts themselves. That is the guard for someone who lives in a production cluster all day, where the risk is not arriving but what gets deleted in `kube-system` — and it covers `kctx ctx`, `kctx ns`, `kctx exec -n` and `kctx shell -n` alike:

```bash
kctx guard add --prefix prod- -n kube-system --confirm
```

See [Configuration](docs/CONFIGURATION.md) for every field.

<br/>

## Taking on another kubeconfig

Someone sends you a kubeconfig. The usual answer is `KUBECONFIG=a:b kubectl config view --flatten > merged`, and it has a trap: every kubeadm cluster calls its cluster `kubernetes` and its user `kubernetes-admin`, so the merge resolves the collision by last-writer-wins and quietly repoints the contexts you already had at a different API server. You find out later, from the wrong cluster.

```bash
$ kctx import ~/Downloads/kubeconfig.yaml
CONTEXT                      ACTION  CLUSTER       USER                SOURCE
kubernetes-admin@kubernetes  added   kubernetes-2  kubernetes-admin-2
Imported 1 context(s). Switch to one with kctx ctx <name>.
```

A stanza whose contents differ is never replaced — the incoming one lands under a name of its own and only the imported context points at it. A stanza identical to one already there is reused rather than duplicated, so re-running an import is a no-op instead of a pile of copies. Colliding *context* names are refused outright until you say what to do about them (`--prefix`, `--as`, `--overwrite`), and `--dry-run` shows the whole plan first.

The other direction hands one context to someone else, or takes a backup:

```bash
kctx export prod --flatten -f prod.yaml   # certificates inlined, so it works elsewhere
kctx export --all -f backup.yaml          # everything
```

Exports are written `0600`, never overwrite a file without `--force`, and a `confirm` guard applies — handing over a kubeconfig is handing over a route to the cluster. See [Usage](docs/USAGE.md#kctx-import).

<br/>

## Where things are stored

| Path | Contents |
|---|---|
| `$XDG_CONFIG_HOME/kube-ctx/config.yaml` | aliases, guard rules |
| `$XDG_STATE_HOME/kube-ctx/history*` | context and namespace history |
| `$XDG_STATE_HOME/kube-ctx/backups/` | kubeconfig snapshots, 10 generations |
| `$XDG_STATE_HOME/kube-ctx/shells/` | per-terminal kubeconfig copies (`kctx sessions`) |
| `$XDG_CACHE_HOME/kube-ctx/namespaces/` | namespace list cache |

Everything is created `0600`/`0700` — the copies hold credentials. Nothing is written to `~/.kube/` except the kubeconfig edit you asked for, and destructive edits are backed up first.

<br/>

## License

Apache 2.0 — see [LICENSE](LICENSE).
