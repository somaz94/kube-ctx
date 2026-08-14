# Usage

Every command, every flag.

<br/>

## Global flags

| Flag | Default | Description |
|---|---|---|
| `--kubeconfig <path>` | — | Use this file instead of `$KUBECONFIG` / `~/.kube/config` |
| `-o, --output <format>` | `color` | `color`, `plain`, or `json` |
| `--no-color` | `false` | Never emit ANSI escapes. `NO_COLOR` and `TERM=dumb` do the same |
| `-y, --yes` | `false` | Answer every confirmation prompt with yes |

Color turns itself off when stdout is not a terminal, so `kctx list | grep prod` gets clean text without any flag.

<br/>

## kctx ctx

```bash
kctx ctx                  # picker (list, when there is no terminal)
kctx                      # the same thing — bare kctx is a shortcut for ctx
kctx ctx prod-eks         # switch
kctx ctx p                # switch via an alias
kctx ctx -                # previous context
kctx ctx -3               # three contexts back
kctx ctx --back 3         # identical, for scripts
```

`-N` is rewritten to `--back=N` before the flags are parsed — a bare `-3` would otherwise look like an unknown shorthand flag.

History is a stack of the contexts you switched *away from*, capped at 20. Re-selecting the context you are already on does not push an entry, so `-` always means "the last context I was actually on".

<br/>

## kctx ns

```bash
kctx ns                   # picker over the namespaces of the current context
kctx ns kube-system       # switch
kctx ns -                 # previous namespace *of this context*
kctx ns --refresh         # bypass the cache
kctx ns --timeout 2s      # how long to wait for the API server
```

The namespace list comes from the API server and is cached for 10 minutes per context. If the cluster is unreachable, the cached list is shown with a warning on stderr rather than an empty list.

Namespace history is scoped per context: `kctx ns -` in the dev cluster never offers a namespace that only exists in prod.

<br/>

## kctx list

```bash
kctx list                 # marker, name, namespace, guard badge
kctx list --wide          # + cluster, user, server
kctx list -o json         # machine-readable
```

The current context is bold and marked with `*`.

<br/>

## kctx rename

```bash
kctx rename dev development
kctx rename . prod-eks-apne2      # "." is the current context
```

`current-context` follows the rename, so the kubeconfig never ends up pointing at a name that no longer exists. The kubeconfig is backed up first.

<br/>

## kctx delete

```bash
kctx delete old-cluster
kctx delete a b c --yes
kctx delete old-cluster --prune   # also drop the now-unreferenced cluster/user
kctx delete . --yes               # the current context
```

Without `--prune`, the cluster and user entries a deleted context referenced are left alone — they are frequently shared, and removing credentials as a side effect of dropping one context is rarely what anyone wants. When something *is* left unreferenced, the command says so.

Deleting the current context clears `current-context` rather than silently promoting another one. The kubeconfig is backed up first.

<br/>

## kctx alias

```bash
kctx alias                     # list
kctx alias p prod-eks-apne2    # set
kctx alias --delete p          # remove
```

An alias works anywhere a context name does — `kctx ctx p`, `kctx exec p -- ...`, `kctx shell p`. Prefix it with `@` to force the alias reading when a context of the same name also exists.

<br/>

## kctx current

```bash
kctx current            # the context name, nothing else
kctx current -n         # its namespace instead
kctx current -o json    # {"context": "...", "namespace": "..."}
```

Prints where you are and exits. Nothing is changed, no picker opens, and the output carries no badge or color — it is meant to be substituted straight into a shell prompt:

```bash
# bash / zsh
PS1='[$(kctx current)] '"$PS1"
```

Inside a `kctx shell` or a hook-managed terminal this reports that terminal's context, which is the thing `kubectl config current-context` gets wrong when `$KUBECONFIG` is not honored. Exits non-zero when nothing is set, printing nothing, so a prompt degrades quietly.

<br/>

## kctx guard

```bash
kctx guard list                                    # rules in effect, in order
kctx guard add cluster-7 --confirm --label PROD    # this exact context
kctx guard add --suffix -live --level danger       # a local naming convention
kctx guard add --prefix acme- --level warn
kctx guard add --match '^eks-.*-main$' --confirm   # regex, for anything else
kctx guard remove 1                                # by its number in the list
```

The built-in rules classify by name — `prod`, `prd`, `production` as **danger**, `stg`, `staging`, `uat` as **warn** — and only badge; nothing is blocked until a rule sets `confirm`. `--confirm` makes switching to a matching context demand that you retype its full name, the same shape of speed bump as `terraform destroy`. `-y` skips it, for scripts.

The reason `add` takes a plain context name is that the clusters most worth guarding are the ones the built-in patterns miss. A rule may carry only one matcher — a context list, a prefix, a suffix, or a regex — and new rules are prepended, so they win over the defaults.

<br/>

## kctx doctor

```bash
kctx doctor                        # every context
kctx doctor prod-eks staging       # only these
kctx doctor --timeout 1s           # per-cluster deadline (default 3s)
kctx doctor --concurrency 16       # parallel probes (default 8)
kctx doctor --unhealthy            # only what is broken
kctx doctor -o json
```

```
STATUS  CONTEXT     VERSION              LATENCY  AUTH                     NOTES
ok      prod-eks    v1.31.4-eks-bca9cf6  213ms    exec:aws
ok      onprem-dev  v1.34.3              9ms      client-cert (237d left)
broken  old-lab     -                    -        none                     cluster "old-lab" is not defined
```

Checks that need no network are reported even when the cluster cannot be reached: dangling cluster or user references, an expired client certificate, an expired token. Token expiry comes from decoding the JWT `exp` claim — it is decoded, not verified.

Exits non-zero when any probed context is unhealthy, so it can gate a script.

<br/>

## kctx shell

```bash
kctx shell prod-eks
kctx shell prod-eks -n monitoring
kctx shell                        # the current context
```

Opens a subshell (`$SHELL`) whose `$KUBECONFIG` points at a private copy pinned to that context. The global kubeconfig is never written, so other terminals keep the context they were on. The copy is deleted when the shell exits.

Inside the shell, `$KUBE_CTX_ACTIVE` names the context and `$KUBE_CTX_DEPTH` counts how many managed shells deep you are — both useful in a prompt.

<br/>

## kctx exec

```bash
kctx exec prod-eks -- kubectl get pods
kctx exec prod-eks -n monitoring -- helm list
kctx exec p -- kubectl top nodes           # aliases work here too
```

Runs one command with its kubeconfig pinned to a context, then throws the copy away. The command's own exit status is passed through unchanged, so `kctx exec ... -- kubectl get pod x || echo missing` behaves the way you would expect.

<br/>

## kctx init

```bash
eval "$(kctx init zsh)"        # ~/.zshrc
eval "$(kctx init bash)"       # ~/.bashrc
kctx init fish | source        # ~/.config/fish/config.fish

kctx init zsh --no-completion  # hook only
kctx init                      # infer the shell from $SHELL
```

Prints a wrapper function plus completions. With the hook installed, a switch applies to the current terminal only. See [Configuration](CONFIGURATION.md#shell-integration) for how it works.

<br/>

## Exit status

| Code | Meaning |
|---|---|
| `0` | Success |
| `1` | kube-ctx itself failed: unreadable kubeconfig, unknown context, bad guard rule |
| `2` | `doctor` reached the clusters and some are unhealthy |
| `130` | You declined a confirmation, or closed the picker |
| _n_ | `exec` passes the wrapped command's status through |
| `128+`_sig_ | `exec`'s command was killed by a signal, the way a shell reports it |

`1` and `2` are separated on purpose: `kctx doctor prod || page-oncall` should fire when a cluster is sick, not when `--kubeconfig` was misspelled.

So is `130`: `kctx ctx prod && ./deploy.sh` must not deploy when you declined the guard. Declining is not success, and it is not an error either — the shell's convention for "the user stopped this" is what it gets.
