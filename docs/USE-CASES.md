# Use Cases

Situations kube-ctx was built for, and what it does about them.

<br/>

## The cross-terminal switch

**The problem.** You are debugging in tab A against `dev`. In tab B you switch to `prod` to check something. You come back to tab A, run `kubectl delete pod …`, and delete it in production — because there is only one `current-context` and tab B moved it.

**What kube-ctx does.** With the shell hook installed, each terminal gets its own copy of the kubeconfig and its own `$KUBECONFIG`. Tab B's switch is invisible to tab A. For a one-off there is `kctx exec`, which never writes anything at all.

<br/>

## The unguarded production switch

**The problem.** `kubectx prod-eks` and `kubectx dev-eks` are the same keystrokes and the same silence. Nothing tells you which one you just did.

**What kube-ctx does.** Contexts are classified by name. Production is badged in red in the list, in the picker, and on the switch confirmation. Set `confirm: true` and switching demands you retype the full context name — the same shape of speed bump as `terraform destroy`.

<br/>

## The kubeconfig that grew for a year

**The problem.** Forty contexts. Some clusters were torn down months ago, several client certificates expired, two contexts reference a `cluster:` block someone deleted. You find out which when a command hangs.

**What kube-ctx does.** `kctx doctor` contacts every cluster in parallel and reports reachability, server version and latency, plus everything findable without a network call: dangling cluster and user references, expired client certificates, expired tokens. Then `kctx delete … --prune` cleans up, with a backup taken first.

<br/>

## The kubeconfig in your Downloads folder

**The problem.** A colleague sends you the kubeconfig for a cluster you need today. Both it and yours were produced by `kubeadm`, so both call their cluster `kubernetes` and their user `kubernetes-admin`. `KUBECONFIG=mine:theirs kubectl config view --flatten` resolves that by last-writer-wins: the merge succeeds, and the contexts you already had now point at their API server. Nothing tells you.

**What kube-ctx does.** `kctx import` treats a colliding stanza as a collision. One whose contents differ is stored under a name of its own and only the imported context is pointed at it; one that is identical is reused rather than copied. Colliding context names stop the import until you choose `--prefix`, `--as` or `--overwrite`, `--dry-run` shows the plan first, and the kubeconfig is backed up before the write. `kctx export prod --flatten` is the same job in reverse — one context, certificates inlined, ready to hand over.

<br/>

## The machine without fzf

**The problem.** `kubectx` degrades to a plain list without `fzf`, and `fzf` is not installed on the jump host, the fresh laptop, or the container you are debugging from.

**What kube-ctx does.** The picker is part of the binary — fuzzy matching, highlighting, scrolling, no external dependency. One static binary, and it falls back to listing when there is no terminal.

<br/>

## The VPN that just dropped

**The problem.** `kubens` asks the API server for the namespace list. Off VPN, you get an error and no way to switch namespaces even though the target is one you use every day.

**What kube-ctx does.** The namespace list is cached per context for 10 minutes. When the live call fails, the cached list is shown with a warning on stderr saying it is stale. `--refresh` forces a live call when you want one.

<br/>

## The multi-file $KUBECONFIG

**The problem.** You keep `~/.kube/config` plus a per-project file, merged through `$KUBECONFIG`. A tool that rewrites the YAML collapses them into one file, loses your comments, and reorders every key.

**What kube-ctx does.** All reads and writes go through client-go's `clientcmd` — the same code path `kubectl config` uses. Each change is written back to the file its stanza came from.

<br/>

## Checking a cluster from CI

**The problem.** A pipeline step should fail early when the cluster it is about to deploy to is unreachable or the credential has expired.

**What kube-ctx does.** `kctx doctor <context>` exits non-zero when anything is unhealthy, and `-o json` gives the detail. `kctx exec <context> -- <cmd>` runs a command against a named context without mutating any shared state, which matters on a shared runner.

<br/>

## Not a use case

- **Editing clusters, users or credentials.** kube-ctx switches between what is already in your kubeconfig; use `kubectl config set-cluster` and friends to add entries.
- **Merging kubeconfig files.** Set `$KUBECONFIG` to a list; that is a kubectl feature and kube-ctx respects it.
- **Multi-cluster fan-out.** Running one command against ten clusters is a different tool's job.
