# Runs the real kctx inside the vhs container against a synthetic kubeconfig.
# To regenerate, from the repo root:
#   GOOS=linux go build -o bin/linux/kctx ./cmd
#   docker run --rm -v "$PWD:/vhs" ghcr.io/charmbracelet/vhs examples/demo/demo.tape
Output examples/demo/demo.gif

Set Shell bash
Set FontSize 16
Set Width 1280
Set Height 400
Set Padding 16
Set TypingSpeed 35ms
Set Theme "Catppuccin Mocha"

Hide
Type "export PATH=/vhs/bin/linux:$PATH KUBECONFIG=/tmp/kctx/kubeconfig XDG_CONFIG_HOME=/tmp/kctx/config XDG_STATE_HOME=/tmp/kctx/state XDG_CACHE_HOME=/tmp/kctx/cache"
Enter
Type "mkdir -p $XDG_CONFIG_HOME/kube-ctx && cp examples/demo/kubeconfig.yaml $KUBECONFIG && cp examples/demo/config.yaml $XDG_CONFIG_HOME/kube-ctx/"
Enter
Type "PS1='$ ' && clear"
Enter
Sleep 500ms
Show

Type "kctx ctx"
Enter
Sleep 2s
Type "prod"
Sleep 1.5s
Down
Sleep 1s
Up
Sleep 1s
Enter
Sleep 2s
Type "prod-eks"
Sleep 500ms
Enter
Sleep 4s
