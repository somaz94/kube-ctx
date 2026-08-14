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

## kctx import

```bash
kctx import ~/Downloads/kubeconfig.yaml       # every context in the file
kctx import a.yaml b.yaml                     # several files at once
kctx import k.yaml --context prod --as acme   # one context, under a new name
kctx import k.yaml --prefix acme-             # namespace the whole file
kctx import k.yaml --overwrite                # replace what is already there
kctx import k.yaml --overwrite --prune        # and clean up what that orphaned
kctx import k.yaml --dry-run                  # report, change nothing
```

The named files are read on their own — `$KUBECONFIG` is not consulted for them — and every selected context is copied over with the cluster and user stanzas it references. Nothing is activated: `current-context` is left where it was, and `kctx ctx <name>` switches when you are ready.

A context whose name is already taken is **refused**, not replaced. Import it under another name with `--prefix` or `--as`, or pass `--overwrite`. Re-importing a file whose contexts are already present is a no-op reported as `unchanged`, so the command is safe to repeat:

```
$ kctx import ~/Downloads/kubeconfig.yaml
CONTEXT                      ACTION  CLUSTER       USER                SOURCE
kubernetes-admin@kubernetes  added   kubernetes-2  kubernetes-admin-2
Imported 1 context(s). Switch to one with kctx ctx <name>.
```

That `kubernetes-2` is the interesting part. Cluster and user names collide far more often than context names do — every kubeadm cluster calls its cluster `kubernetes` and its user `kubernetes-admin` — and `kubectl config view --flatten` resolves that by last-writer-wins, which silently repoints the contexts you already had at a different API server. `kctx import` never replaces a stanza whose contents differ: the incoming one is stored under a suffixed name and only the imported context is pointed at it. A stanza that is byte-for-byte the one already there is reused rather than copied, so importing five contexts that share a cluster does not leave five copies of it behind.

`--overwrite` repoints a context, which can leave the cluster and user it used to name unreferenced. The note that follows names those, and only those — not whatever your kubeconfig was already carrying, because a hint that lists a year of accumulated cruft is one you learn to skip:

```
$ kctx import ~/Downloads/acme.yaml --overwrite
CONTEXT  ACTION       CLUSTER  USER       SOURCE
prod     overwritten  acme     acme-user
Imported 1 context(s). Switch to one with kctx ctx <name>.
note: cluster(s) prod-cluster and user(s) prod-user are now unreferenced; re-run with --prune to remove them
```

`--prune` does the removal, and like `kctx delete --prune` it takes *every* unreferenced entry rather than only this import's leftovers — otherwise re-running with `--prune`, which is exactly what the note tells you to do, would skip the stanzas it just named.

The kubeconfig is backed up first, and the write lands in your own kubeconfig — never back in the file you imported from.

<br/>

## kctx export

```bash
kctx export                          # the current context, to stdout
kctx export prod                     # one named context
kctx export dev prod                 # several
kctx export --all -f backup.yaml     # everything, to a file
kctx export prod --flatten -f p.yaml # portable: certificates inlined
kctx export prod -o json             # the same document as JSON
```

Writes a standalone kubeconfig holding only the named contexts and the cluster and user stanzas they actually reference — the smallest file that still works. `current-context` is set to the exported context, or kept as it was when it survived the extraction, because a kubeconfig without one is a file `kubectl` refuses to use.

`--flatten` inlines the certificates and keys the contexts point at. Without it the export refers to paths that exist only on this machine, which is fine for a backup and useless for handing to someone else.

The output carries credentials, and the command treats it that way. A file is written `0600`, an existing file is never replaced without `--force`, and a context guarded with `confirm: true` asks before it is exported — handing over a kubeconfig is handing over a route to the cluster. The question goes to stderr, so `kctx export prod > prod.yaml` never ends up with a prompt at the top of the file.

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

Inside the shell, `$KUBE_CTX_ACTIVE` names the context and `$KUBE_CTX_DEPTH` counts how many managed shells deep you are.

**Your prompt will look exactly the same in there.** That surprises people, and it is not a bug: the session copy names the same context, so anything reading `current-context` — including `kube-ps1` — reports what it did before. kube-ctx exports the two variables a prompt needs but does not install them, because reaching into `$PS1` would fight whatever theme you already run. Entering the first managed shell prints the snippet for your shell; wiring it up once is enough:

```bash
# bash / zsh
PS1='${KUBE_CTX_DEPTH:+[kctx:$KUBE_CTX_DEPTH] }'"$PS1"

# already using kube-ps1? add the marker inside your wrapper — it reads an
# exported variable, so nothing prints when kube-ctx is not involved
[ -n "$KUBE_CTX_DEPTH" ] && printf '[kctx:%s]' "$KUBE_CTX_DEPTH"
```

```fish
# fish
test -n "$KUBE_CTX_DEPTH"; and echo -n "[kctx:$KUBE_CTX_DEPTH] "
```

<br/>

## kctx exec

```bash
kctx exec prod-eks -- kubectl get pods
kctx exec prod-eks -n monitoring -- helm list
kctx exec p -- kubectl top nodes           # aliases work here too
```

Runs one command with its kubeconfig pinned to a context, then throws the copy away. The command's own exit status is passed through unchanged, so `kctx exec ... -- kubectl get pod x || echo missing` behaves the way you would expect.

`--all` and `-c` run against several contexts at once:

```bash
kctx exec --all -- kubectl get nodes
kctx exec -c dev,staging -- kubectl get deploy -n api
kctx exec --all -p 2 -- kubectl version      # at most two clusters at a time
kctx exec --all -o json -- kubectl get ns    # one object per context
```

```
$ kctx exec -c dev,prod -- kubectl get nodes -o name
== dev
node/dev-control-plane
== prod  DANGER  exit 1
1 of 2 context(s) failed: prod
```

Which flag you use decides *how* the command runs, not just how many contexts it lands on:

| | `kctx exec <ctx>` | `--all` / `-c` |
|---|---|---|
| Output | streamed straight through | captured, then printed per context |
| stdin | the terminal's | none |
| Concurrency | — | `-p`, 8 by default |
| Exit status | the command's own | the first non-zero, in the order the contexts were named |

The split is not arbitrary. Streaming is what makes `kctx exec prod -- kubectl logs -f` work, and it is exactly what cannot work for several children at once: four clusters writing to one terminal interleave into nonsense, and a command that waits on stdin would hang the sweep with nothing on screen to say why. Capturing also makes `-o json` meaningful — `[{"context": …, "exitCode": …, "stdout": …, "stderr": …}]` — which a streamed command could not produce.

Every guard is answered before any command runs. Declining any one of them aborts the whole thing and exits `130`, so a fan-out never reaches half its clusters and then stops to ask.

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
