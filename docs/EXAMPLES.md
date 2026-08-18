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

## Gate the namespace, not the cluster

When production is where you work all day, a guard on the context fires once and then gets typed past. Put it on the namespace instead:

```yaml
guards:
  - prefix: prod-
    namespaces: [kube-system, istio-system]
    level: danger
    confirm: true
```

```bash
$ kctx ctx prod-eks
Switched to context prod-eks (namespace default).

$ kctx ns kube-system
! kube-system in prod-eks is classified danger by the guard rule prod-* / kube-system, istio-system.
Type "kube-system" to continue: kube-system
Namespace set to kube-system  DANGER in context prod-eks.

$ kctx exec prod-eks -n kube-system -- kubectl delete deploy/coredns
! kube-system in prod-eks is classified danger by the guard rule prod-* / kube-system, istio-system.
Type "kube-system" to continue: kube-sys
Aborted.
```

Arriving in the cluster is unremarkable, so nothing is said about it; the namespace inside it is the gate, on every route to it — including `kctx ctx`, when the context you switch to already sits in a guarded namespace. Badge production as well and that is a second rule — one rule carries one `level`.

<br/>

## Ask every cluster the same question

```bash
$ kctx exec --all -- kubectl get nodes -o name
== dev
node/dev-control-plane
== prod-eks-apne2  DANGER
node/ip-10-0-1-14.ap-northeast-2.compute.internal
node/ip-10-0-2-31.ap-northeast-2.compute.internal
== staging  WARN  exit 1
1 of 3 context(s) failed: staging
```

The contexts run in parallel, each in its own throwaway kubeconfig, and the output is grouped rather than interleaved. Nothing switches — the terminal is on whatever context it was on before.

The failing context sets the exit status, so this is safe in a pipeline:

```bash
kctx exec -c dev,staging -- kubectl apply -f manifests/ && ./promote.sh
```

For anything that is going to be parsed, ask for the structure instead of grepping the table:

```bash
$ kctx exec --all -o json -- kubectl get ns -o name | jq -r '.[] | select(.exitCode != 0) | .context'
staging
```

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

## Take on a kubeconfig someone sent you

```bash
$ kctx import ~/Downloads/acme.yaml --dry-run
CONTEXT                      ACTION  CLUSTER       USER                SOURCE
kubernetes-admin@kubernetes  added   kubernetes-2  kubernetes-admin-2
Would import 1 context(s). Switch to one with kctx ctx <name>.

$ kctx import ~/Downloads/acme.yaml --context kubernetes-admin@kubernetes --as acme
CONTEXT  ACTION  CLUSTER       USER                SOURCE
acme     added   kubernetes-2  kubernetes-admin-2  kubernetes-admin@kubernetes
Imported 1 context(s). Switch to one with kctx ctx <name>.

$ kctx ctx acme
Switched to context acme (namespace default).
```

The suffix on `kubernetes-2` is the collision being handled rather than ignored: the cluster named `kubernetes` you already had still points where it always did. Run the same import again and every row reads `unchanged`.

<br/>

## Hand one context to someone else

```bash
$ kctx export prod --flatten -f prod.yaml
Wrote 1 context(s) to prod.yaml (0600). It carries credentials.

$ KUBECONFIG=prod.yaml kubectl get nodes     # works on any machine

$ kctx export --all -f backup.yaml           # or take the lot
```

Without `--flatten` the export still names certificate paths from this machine — fine for a backup, useless for someone else. An existing file is never replaced without `--force`.

<br/>

## A context per repository

```bash
$ cd ~/work/payments && kctx bind staging-eks
Bound /home/u/work/payments to staging-eks.  WARN

$ cd ~/work/api && kctx bind dev
Bound /home/u/work/api to dev.

$ cd ~/work/payments
Switched to context staging-eks (namespace default).  WARN

$ cd cmd/server          # deeper in the same repo: nothing happens
$ kctx ctx dev           # and a choice made by hand sticks
$ cd ..                  # still dev

$ cd ~/work/api
Switched to context dev (namespace default).

$ kctx bind
DIRECTORY               CONTEXT
/home/u/work/api        dev
/home/u/work/payments   staging-eks
```

Only the terminal that moved switches. Binding a context guarded with `confirm` is allowed, but entering the directory will not switch to it — kube-ctx names it and leaves you where you were.

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
