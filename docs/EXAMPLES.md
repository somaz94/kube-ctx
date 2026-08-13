# Examples

<br/>

## Everyday switching

```bash
$ kctx
❯ prod
▸ prod-eks-apne2      DANGER  monitoring
  prod-gke-asia1      DANGER  default
  staging-eks-apne2   WARN    default
3/12  ↑↓ move  ⏎ select  esc cancel
```

Type to filter, arrows to move, Enter to pick. No `fzf` required.

```bash
$ kctx ctx staging-eks-apne2
Switched to context staging-eks-apne2 (namespace default).  WARN

$ kctx ns monitoring
Namespace set to monitoring in context staging-eks-apne2.

$ kctx ctx -
Switched to context prod-eks-apne2 (namespace monitoring).  DANGER
```

<br/>

## Two terminals, two contexts

With the hook installed (`eval "$(kctx init zsh)"`):

```bash
# terminal A                          # terminal B
$ kctx ctx prod-eks                   $ kctx ctx dev-kind
$ kubectl config current-context      $ kubectl config current-context
prod-eks                              dev-kind
```

Neither terminal disturbs the other, and `~/.kube/config` still says whatever it said this morning.

<br/>

## Look at production without leaving what you are doing

```bash
$ kubectl config current-context
dev-kind

$ kctx exec prod-eks -- kubectl get pods -n monitoring
NAME                          READY   STATUS    RESTARTS   AGE
prometheus-server-0           2/2     Running   0          9d

$ kubectl config current-context
dev-kind                                  # unchanged
```

<br/>

## A shell for one task

```bash
$ kctx shell prod-eks -n monitoring
Entering a shell pinned to prod-eks (namespace monitoring). Type exit to leave.  DANGER

[prod-eks] $ kubectl get pods
[prod-eks] $ helm list
[prod-eks] $ exit

$                                          # back to where you were
```

(The `[prod-eks]` prompt comes from the `$KUBE_CTX_ACTIVE` snippet in [Configuration](CONFIGURATION.md#prompt).)

<br/>

## Gate a switch to production

`~/.config/kube-ctx/config.yaml`:

```yaml
guards:
  - match: '(^|[-_.])(prod|production)([-_.]|$)'
    level: danger
    confirm: true
```

```bash
$ kctx ctx prod-eks-apne2
! prod-eks-apne2 is classified danger by the guard rule (^|[-_.])(prod|production)([-_.]|$).
Type "prod-eks-apne2" to continue: prod
Aborted.
```

Scripts opt out with `-y`.

<br/>

## Find the dead contexts

```bash
$ kctx doctor --unhealthy
STATUS       CONTEXT       VERSION  LATENCY  AUTH                    NOTES
unreachable  old-staging   -        3.001s   exec:aws                Get "https://…": i/o timeout
broken       lab-cluster   -        -        client-cert (expired)   credential expired 2026-03-02T…
broken       leftover      -        -        none                    cluster "leftover" is not defined

$ kctx delete old-staging lab-cluster leftover --prune
Delete context old-staging, lab-cluster, leftover? [y/N]: y
Deleted 3 context(s).
```

The kubeconfig is backed up before the delete; `$XDG_STATE_HOME/kube-ctx/backups/` keeps ten generations.

<br/>

## Short names

```bash
$ kctx alias p prod-eks-apne2
Alias p now points at prod-eks-apne2.

$ kctx ctx p
$ kctx exec p -- kubectl get nodes
$ kctx shell p
```

<br/>

## Scripting

```bash
# fail a pipeline when a cluster is unreachable
kctx doctor prod-eks --timeout 5s || exit 1

# JSON everywhere
kctx list -o json | jq -r '.[] | select(.Current) | .Name'
kctx doctor -o json | jq -r '.[] | select(.reachable | not) | .context'

# exit status passes through
kctx exec prod-eks -- kubectl get pod missing-pod
echo $?          # 1, from kubectl
```

<br/>

## Piping

```bash
$ kctx list | grep prod          # no ANSI escapes, no picker
$ kctx ctx | head -3             # plain list when stdout is not a terminal
```

The picker opens `/dev/tty` directly, so it still works when stdout is redirected — but it falls back to listing when there is no terminal at all, such as in CI.
