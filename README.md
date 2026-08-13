# kube-ctx

[![CI](https://github.com/somaz94/kube-ctx/actions/workflows/ci.yml/badge.svg)](https://github.com/somaz94/kube-ctx/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Latest Tag](https://img.shields.io/github/v/tag/somaz94/kube-ctx)](https://github.com/somaz94/kube-ctx/tags)
[![Top Language](https://img.shields.io/github/languages/top/somaz94/kube-ctx)](https://github.com/somaz94/kube-ctx)

`kctx` — a `kubectx` / `kubens` replacement that switches Kubernetes contexts and namespaces without the three things that make the originals risky at scale: a global current-context every terminal shares, no guard rail in front of production, and an external `fzf` dependency for the picker.

<br/>

## Why kube-ctx?

| | `kubectx` + `kubens` | `kctx` |
|---|---|---|
| **Scope of a switch** | Global — every terminal follows | Global, or per-terminal via `kctx shell` / shell hook |
| **Production guard** | None | Regex-classified contexts, colored, optionally confirm-to-switch |
| **Interactive picker** | Needs `fzf` on `$PATH` | Built in, no external dependency |
| **Namespace list offline** | Fails when the API server is unreachable | Falls back to a cache and says so |
| **Cluster health** | — | `kctx doctor`: reachability, server version, expired certs and tokens |
| **kubeconfig writes** | Re-emits the YAML | `clientcmd`, the same path `kubectl` uses — multi-file `$KUBECONFIG` safe |
| **Backups** | — | Automatic before every destructive edit |
| **Binaries** | Two | One |

<br/>

## Quick Start

```bash
git clone https://github.com/somaz94/kube-ctx.git
cd kube-ctx
make build          # → ./bin/kctx
make install        # → /usr/local/bin/kctx
```

<br/>

## Usage

```bash
kctx list [--wide]              # every context, current one marked
kctx ctx                        # list (interactive picker once built)
kctx ctx prod-eks               # switch
kctx ctx -                      # back to the previous context
kctx ctx -2                     # two contexts back
kctx ns kube-system             # set the namespace of the current context
kctx ns -                       # previous namespace *of this context*
kctx rename . prod-eks-apne2    # "." means the current context
kctx delete old-cluster --prune # delete, and drop the entries left unreferenced
kctx alias p prod-eks           # then: kctx ctx p
kctx version
```

Global flags: `--kubeconfig`, `-o color|plain|json`, `--no-color`, `-y/--yes`.

<br/>

## Where things are stored

| Path | Contents |
|---|---|
| `$XDG_CONFIG_HOME/kube-ctx/config.yaml` | aliases, production guard rules |
| `$XDG_STATE_HOME/kube-ctx/history*` | context and namespace history |
| `$XDG_STATE_HOME/kube-ctx/backups/` | kubeconfig snapshots, 10 generations |
| `$XDG_CACHE_HOME/kube-ctx/namespaces/` | namespace list cache |

Nothing is written to `~/.kube/` except the kubeconfig edit you asked for.

<br/>

## Status

Under active development. Implemented: context and namespace switching with
history, listing, rename, delete, aliases. Next: the built-in picker,
production guards, `kctx doctor`, and per-terminal isolation
(`kctx shell` / `kctx exec` / `kctx init`).

<br/>

## License

Apache 2.0 — see [LICENSE](LICENSE).
