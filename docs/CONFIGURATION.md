# Configuration

kube-ctx works with no configuration at all. Everything below is opt-in.

<br/>

## The config file

`$XDG_CONFIG_HOME/kube-ctx/config.yaml`, or `~/.config/kube-ctx/config.yaml` when `XDG_CONFIG_HOME` is unset.

```yaml
aliases:
  p: prod-eks-apne2
  d: dev-kind

guards:
  - match: '(^|[-_.])(prod|prd|production)([-_.]|$)'
    level: danger
    confirm: true
    label: PROD
  - match: '(^|[-_.])(stg|stage|staging|uat)([-_.]|$)'
    level: warn
```

The file is created and rewritten by `kctx alias` and `kctx guard`; you can also edit it by hand. It is written `0600`, with a header comment explaining the fields — comments you add yourself are not preserved across a rewrite.

<br/>

### aliases

A map of short name to context name. An alias is accepted anywhere a context name is. Prefix a name with `@` to force the alias reading when a context of the same name also exists.

<br/>

### guards

A list of rules, tested against the context name in order — the first match wins. Rules that list `namespaces:` are tested against namespaces instead, on a list of their own.

| Field | Type | Description |
|---|---|---|
| `match` | regexp | Go regular expression tested against the context name |
| `contexts` | list | Exact context names this rule applies to |
| `prefix` | string | Matches context names starting with it |
| `suffix` | string | Matches context names ending with it |
| `namespaces` | list | Exact namespace names; moves the whole rule onto the namespace axis |
| `level` | `safe` \| `warn` \| `danger` | How dangerous the context is |
| `confirm` | bool | Require retyping the guarded name — the context, or the namespace on a namespace rule |
| `label` | string | Badge text; defaults to `DANGER` / `WARN` |

A rule carries **exactly one** matcher. Two is an error rather than a precedence question: it is a typo, and silently honouring one of them is how a context nobody guarded ends up looking guarded. A rule with none is an error too — treating it as match-everything would classify a whole kubeconfig as production. The single exception is a rule that sets `namespaces`, which already carries a matcher of its own; see [Namespace rules](#namespace-rules).

An unrecognized `level` is treated as `safe`. A typo downgrades a rule rather than silently promoting a context to dangerous.

`confirm` applies to **every** route to the cluster — `kctx ctx`, `kctx shell` and `kctx exec` — not just switching. A guard that only covered `ctx` would be walked straight past by `kctx exec prod -- kubectl delete deploy/api`, which is the more dangerous of the two. `kctx exec` also announces the guarded context it is about to run against. `-y` skips the prompt everywhere, for scripts, and declining exits `130` so `kctx ctx prod && ./deploy.sh` does not deploy.

The name is the only thing every cluster has in common — an EKS ARN, a kind cluster and a kubeadm context share no label or field that says "production" — which is why the rules match on it.

But names lie. The cluster that would hurt most to break is often the one called `cluster-7`, and no pattern over `prod` will ever find it. That is what `contexts`, `prefix` and `suffix` are for: naming it takes no regex.

```yaml
guards:
  # The real production cluster, whose name says nothing.
  - contexts: [cluster-7, arn-eks-apne2-main]
    level: danger
    label: PROD
    confirm: true
  # A local convention the built-in patterns know nothing about.
  - suffix: '-live'
    level: danger
    confirm: true
```

<br/>

### Namespace rules

A rule may also list `namespaces:`, and that moves the whole rule onto the other axis: it then classifies those namespaces *inside* the contexts it matches, and stops classifying the context itself.

```yaml
guards:
  # kube-system in any production cluster requires retyping the namespace.
  - prefix: prod-
    namespaces: [kube-system, istio-system]
    level: danger
    confirm: true
  # Switching to production itself is only badged, as before.
  - prefix: prod-
    level: danger
```

Those are two rules on purpose. **One rule cannot guard both a cluster and a namespace inside it**: `level`, `confirm` and `label` would have to mean two things at once, and the two verdicts genuinely differ — a context worth badging is rarely one worth prompting for, and `kube-system` is the reverse. Guard both by writing two rules.

`namespaces` is **exact names, not a pattern**, which is the one place these rules do not offer a regex. It is the same reasoning as everywhere else, run backwards: context names lie, so the cluster that would hurt most is the one called `cluster-7` that no pattern over `prod` will ever find — while the namespaces worth guarding are the standard ones every cluster spells identically. `kube-system` is `kube-system` on every cluster you will ever touch.

The context matcher may be **left out**, meaning every context, and this is the one exception to "a rule with no matcher is an error". That rule exists because a match-everything *context* rule would classify a whole kubeconfig as production; a namespace list is already a matcher, so an omitted context half over-classifies nothing — it guards `kube-system` and nothing else, everywhere, which is the most obvious rule anyone writes here. Demanding `match: '.*'` to say it would put a barrier in front of the safety feature:

```yaml
guards:
  - namespaces: [kube-system]
    level: danger
    confirm: true
```

The two axes are **separate lists**, each keeping its own first-match-wins order, so a namespace rule sitting above a context rule does not shadow it — they are never competing for the same verdict. When a namespace rule does carry a context matcher, **both halves have to match**: `kube-system` in a kind cluster is not the `kube-system` a rule about production means.

`confirm` gates **every route to the namespace** — `kctx ctx`, `kctx ns`, `kctx exec -n` and `kctx shell -n` — for exactly the reason the context guard covers every route to a cluster. One that only covered `kctx ns` would be walked straight past by `kctx exec prod -n kube-system -- kubectl delete deploy/api`. `kctx ctx` is on the list because it runs nothing but decides where everything typed after it runs: switch into a context whose default namespace is `kube-system` and the next bare `kubectl` is already there. `-y` skips it, and declining exits `130`.

When the context is guarded too, the two are asked in turn and each wants its own name back — the context first, then the namespace. Neither answers for the other, because they are not the same decision.

What gets guarded is the **effective** namespace, not just the one `-n` named. If the context's own default is already `kube-system`, then `kctx exec prod -- ...` prompts too — guarding only the flag would leave that route open, and it is the route someone who works in that namespace all day actually uses.

The prompt asks you to retype the **namespace** rather than the context, since the namespace is what the rule is about, and it names both halves so it is clear which rule fired and how far it reaches. Both transcripts below run under the two-rule config at the top of this section, which is why the context carries a badge of its own:

```console
$ kctx exec prod-eks -n kube-system -- kubectl delete deploy/coredns
! kube-system in prod-eks is classified danger by the guard rule prod-* / kube-system, istio-system.
Type "kube-system" to continue: kube-system
Running against prod-eks  DANGER, namespace kube-system  DANGER
```

Elsewhere the badge sits next to the namespace rather than the context, since that is what the rule classified:

```console
$ kctx ns kube-system
! kube-system in prod-eks is classified danger by the guard rule prod-* / kube-system, istio-system.
Type "kube-system" to continue: kube-system
Namespace set to kube-system  DANGER in context prod-eks.  DANGER
```

`kctx exec` names the namespace on that announcement line **only when it is the guarded half** — an unremarkable namespace adds no noise. The namespace picker carries the badge too, with `current` moved into the detail column to make room, matching what the context picker already did.

<br/>

### Managing rules from the command line

`kctx guard` writes these rules for you, so the config file never has to be opened:

```console
$ kctx guard add cluster-7 --confirm --label PROD
Guard added: cluster-7 → danger (confirm)

$ kctx guard add --suffix -live --level danger
Guard added: *-live → danger

$ kctx guard add -n kube-system --confirm
Guard added: any context / kube-system → danger (confirm)

$ kctx guard add --prefix prod- -n kube-system -n istio-system --confirm
Guard added: prod-* / kube-system, istio-system → danger (confirm)

$ kctx guard list
#  MATCH                                      LEVEL   CONFIRM
1  prod-* / kube-system, istio-system         danger  yes
2  any context / kube-system                  danger  yes
3  *-live                                     danger  no
4  cluster-7                                  danger  yes
5  (^|[-_.])(prod|prd|production)([-_.]|$)    danger  no
6  (^|[-_.])(stg|stage|staging|uat)([-_.]|$)  warn    no

$ kctx guard remove 3
Removed guard *-live.
```

A new rule is **prepended**, so it wins over the built-in patterns. The first `add` also materializes the defaults into the file, which is where the `confirm: false` lines you can flip come from. An exact context name that matches nothing in the kubeconfig is rejected — a guard rule that silently covers nothing is worse than no rule at all. `--namespace`/`-n` is repeatable and also takes a comma-separated list; the `MATCH` column shows a namespace rule as both halves, because told only `kube-system` you cannot see whether the rule reaches the cluster in front of you.

<br/>

### Defaults

With no `guards:` block, these are used:

```yaml
guards:
  - match: '(^|[-_.])(prod|prd|production)([-_.]|$)'
    level: danger
  - match: '(^|[-_.])(stg|stage|staging|uat)([-_.]|$)'
    level: warn
```

Note the absence of `confirm`. The built-in rules label and colorize but never block — a tool that prompts on a fresh install before the user asked for it gets uninstalled. Add `confirm: true` when you want the gate.

The word boundaries matter: `reproducible-lab` contains `prod` but is not production.

<br/>

## Shell integration

A child process cannot change its parent's environment, so making a switch affect only the current terminal takes a shell function:

```bash
eval "$(kctx init zsh)"        # ~/.zshrc
eval "$(kctx init bash)"       # ~/.bashrc
kctx init fish | source        # ~/.config/fish/config.fish
```

What the wrapper does:

1. creates a temp file and passes its path in `$KUBE_CTX_ENV_FILE`;
2. names its own shell in `$KUBE_CTX_SHELL`;
3. runs the real binary;
4. sources the file if the binary wrote to it;
5. deletes it and returns the binary's exit status.

And what the binary does, on the first switch in a shell that has the hook: copy the merged kubeconfig into `$XDG_STATE_HOME/kube-ctx/shells/<id>.yaml`, point `current-context` at the target, and write the assignments into the env file. The global kubeconfig is left untouched. Later switches in that shell land in the copy directly, because `$KUBECONFIG` already points there.

The exports go through a file rather than stdout because the picker draws on the same terminal — mixing UI with code to `eval` is how that kind of integration breaks.

The shell name travels in `$KUBE_CTX_SHELL` rather than being read from `$SHELL`, because `$SHELL` is the *login* shell. A fish user who never ran `chsh`, or a fish user running bash for one command, would otherwise be handed the other shell's syntax — and a `set -gx` sourced by bash reports success while changing nothing.

Without the hook, kube-ctx edits the global kubeconfig, exactly the way kubectx does.

<br/>

### Destructive edits inside a managed shell

`rename` and `delete` refuse to run inside a hook-managed shell or a `kctx shell` subshell:

```console
$ kctx delete staging
Error: delete would edit this shell's private kubeconfig copy (session 83cc09ccfef7),
which is discarded when the shell exits; leave the kube-ctx shell first
```

There, `$KUBECONFIG` is the private copy, so the edit would land in a file that is deleted when the shell exits — reporting success and then vanishing. Switching contexts is shell-local on purpose; an edit meant to outlive the shell is not. Run these from a terminal without a kube-ctx session.

<br/>

### Prompt

Managed shells export `$KUBE_CTX_ACTIVE`. `kctx init` prints a matching snippet:

```bash
# bash / zsh
PS1='${KUBE_CTX_ACTIVE:+[$KUBE_CTX_ACTIVE] }'"$PS1"

# fish, inside fish_prompt
test -n "$KUBE_CTX_ACTIVE"; and echo -n "[$KUBE_CTX_ACTIVE] "
```

<br/>

## Environment variables

| Variable | Read | Written | Meaning |
|---|---|---|---|
| `KUBECONFIG` | ✅ | in managed shells | Standard kubeconfig override |
| `KUBE_CTX_ENV_FILE` | ✅ | by the hook | Where to write exports for the calling shell |
| `KUBE_CTX_SHELL` | ✅ | by the hook | Which shell's syntax to write that file in |
| `KUBE_CTX_SHELL_ID` | ✅ | ✅ | Marks a managed shell; scopes its history |
| `KUBE_CTX_ACTIVE` | — | ✅ | The context a managed shell is on |
| `KUBE_CTX_DEPTH` | ✅ | ✅ | How many managed shells deep |
| `SHELL` | ✅ | — | Which shell to spawn, the default for `kctx init`, and the fallback when `KUBE_CTX_SHELL` is unset |
| `NO_COLOR`, `TERM` | ✅ | — | Color opt-out |
| `XDG_CONFIG_HOME`, `XDG_CACHE_HOME`, `XDG_STATE_HOME` | ✅ | — | Where kube-ctx keeps its files |

<br/>

## Files

| Path | Mode | Contents |
|---|---|---|
| `$XDG_CONFIG_HOME/kube-ctx/config.yaml` | `0600` | Aliases and guard rules |
| `$XDG_STATE_HOME/kube-ctx/history` | `0600` | Global context history |
| `$XDG_STATE_HOME/kube-ctx/history-<id>*` | `0600` | Per-shell and per-context history |
| `$XDG_STATE_HOME/kube-ctx/backups/<ts>/` | `0600` | Kubeconfig snapshots, 10 generations |
| `$XDG_STATE_HOME/kube-ctx/shells/<id>.yaml` | `0600` | Per-terminal kubeconfig copies |
| `$XDG_CACHE_HOME/kube-ctx/namespaces/*.json` | `0600` | Namespace list cache |

Session copies hold credentials, which is why nothing here is group- or world-readable. Copies left behind by a shell that was killed rather than exited are swept after 7 days, on the next session.

Backups are taken before destructive edits only — `rename` and `delete`. A plain context or namespace switch is frequent and trivially reversible, so it does not pay the copy.

<br/>

## Multiple kubeconfig files

`$KUBECONFIG` may list several files. kube-ctx reads the merged view and, on write, sends each change back to the file its stanza came from — the same routing `kubectl config` performs, because it is the same code (`clientcmd`). Tools that parse and re-emit the YAML themselves collapse the list into one file and lose comments and key order.
