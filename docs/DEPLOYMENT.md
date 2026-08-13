# Deployment

<br/>

## Install

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
