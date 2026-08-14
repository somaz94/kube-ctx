# Deployment

<br/>

## Install

### Homebrew

```bash
brew install somaz94/tap/kube-ctx
brew upgrade somaz94/tap/kube-ctx
```

The formula lives in [somaz94/homebrew-tap](https://github.com/somaz94/homebrew-tap) and is published by GoReleaser on every tag.

<br/>

### Scoop (Windows)

```powershell
scoop bucket add somaz94 https://github.com/somaz94/scoop-bucket
scoop install kube-ctx
```

The picker needs a terminal that understands ANSI escapes — Windows Terminal does, `cmd.exe` in its default configuration does not. Everything else works either way.

<br/>

### krew (kubectl plugin)

```bash
kubectl krew index add somaz94 https://github.com/somaz94/krew-index
kubectl krew install somaz94/ctx2
```

The plugin is called `ctx2` because `ctx` in the central index belongs to
kubectx. It is published to [somaz94/krew-index](https://github.com/somaz94/krew-index) by GoReleaser on every tag, next to `diff2` and `events2`.

**Switching through `kubectl` is always global.** This is the one channel where
kube-ctx cannot deliver its main feature, so it is worth being precise about:

```bash
kubectl ctx2 list          # fine
kubectl ctx2 doctor        # fine
kubectl ctx2 current       # fine

kubectl ctx2 ctx prod      # switches the GLOBAL kubeconfig — every terminal follows
```

`kubectl` runs a plugin as a subprocess, and a subprocess cannot change the
environment of the shell that called it. The shell hook is a function, and
`kubectl` never consults it, so the switch falls back to the global write the
hook exists to avoid. kube-ctx cannot warn you about it either: `kubectl`
leaves no marker in the environment or in `argv`, and it `exec`s rather than
forks, so even the parent process looks identical.

For per-terminal isolation, install the hook and call the binary directly —
krew puts it on your `$PATH` as `kubectl-ctx2`:

```bash
eval "$(kubectl-ctx2 init zsh)"    # bash: kubectl-ctx2 init bash
                                   # fish: kubectl-ctx2 init fish | source
kubectl-ctx2 ctx prod              # shell-local, as intended
```

If that name is a mouthful, `brew install somaz94/tap/kube-ctx` gives you the
same binary as plain `kctx`. The two can be installed side by side.

<br/>

### Install script

```bash
curl -sSL https://raw.githubusercontent.com/somaz94/kube-ctx/main/scripts/install.sh | bash
```

Detects the platform, downloads the matching release archive, and installs `kctx` into `/usr/local/bin` (with `sudo` only when that directory is not writable).

<br/>

### Binary

Release archives are named after the project, and the binary inside is `kctx`:

```bash
# latest
curl -sL https://github.com/somaz94/kube-ctx/releases/latest/download/kube-ctx_linux_amd64.tar.gz | tar xz
sudo mv kctx /usr/local/bin/

# a specific version
curl -sL https://github.com/somaz94/kube-ctx/releases/download/v0.1.0/kube-ctx_0.1.0_darwin_arm64.tar.gz | tar xz
sudo mv kctx /usr/local/bin/
```

Builds are published for linux, darwin and windows on amd64 and arm64, with `checksums.txt` alongside them.

<br/>

### From source

```bash
git clone https://github.com/somaz94/kube-ctx.git
cd kube-ctx
make build            # → ./bin/kctx
make install          # → /usr/local/bin/kctx
```

Requires Go 1.26 or newer. The binary is static (`CGO_ENABLED=0`) and has no runtime dependencies — no `fzf`, no `kubectl` (though you will want the latter).

<br/>

### With go install

```bash
go install github.com/somaz94/kube-ctx/cmd@latest
```

The binary lands in `$(go env GOPATH)/bin` under the name `cmd`; rename it to `kctx`.

<br/>

## Shell integration

Installing the binary is enough to use kube-ctx. The per-terminal isolation needs one more line:

```bash
# ~/.zshrc
eval "$(kctx init zsh)"

# ~/.bashrc
eval "$(kctx init bash)"

# ~/.config/fish/config.fish
kctx init fish | source
```

That also installs completions. Use `--no-completion` to skip them.

Verify:

```bash
$ type kctx
kctx is a shell function          # the hook is active

$ kctx ctx some-context
$ echo $KUBE_CTX_ACTIVE
some-context
```

<br/>

## Coexisting with kubectx

The binary is called `kctx`, so both can be installed at once. `kctx` never changes `~/.kube/kubectx` (kubectx's own previous-context marker) and keeps its state elsewhere, so the two do not interfere — they simply do not share history.

To replace kubectx entirely:

```bash
brew uninstall kubectx            # removes kubectx and kubens
alias kubectx=kctx                # optional muscle-memory shims
alias kubens='kctx ns'
```

<br/>

## Uninstall

```bash
rm /usr/local/bin/kctx
rm -rf ~/.config/kube-ctx ~/.cache/kube-ctx ~/.local/state/kube-ctx
```

Then remove the `eval "$(kctx init …)"` line from your rc file. Your kubeconfig is untouched by any of this.

<br/>

## Verifying a release build

```bash
make build
./bin/kctx version
# kctx v0.1.0 (commit: abc1234, built: 2026-01-01T00:00:00Z, go1.26.1 darwin/arm64)
```

Version, commit and build date are injected with `-ldflags` at build time; a `go build` without them reports `dev`.

<br/>

## Cutting a release

Pushing a `vX.Y.Z` tag is the whole procedure:

```bash
git tag -a v0.1.0 -m "v0.1.0"
git push origin v0.1.0
```

`.github/workflows/release.yml` then runs GoReleaser, which builds every
platform, creates the GitHub release, and updates the Homebrew tap and the
Scoop bucket. `.github/workflows/changelog-generator.yml` regenerates
`CHANGELOG.md` afterwards.

Repository prerequisites:

| Secret | Used by | Why |
|---|---|---|
| `PAT_TOKEN` | `release.yml` | GoReleaser pushes to `somaz94/homebrew-tap` and `somaz94/scoop-bucket`, which the built-in `GITHUB_TOKEN` cannot reach |
| `GITLAB_TOKEN` | `gitlab-mirror.yml` | Optional; only needed if the GitLab mirror is wanted |

`GITHUB_TOKEN` is provided automatically and needs no setup.
