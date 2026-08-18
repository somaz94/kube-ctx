package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/render"
)

// renderTable prints a table to stdout.
func renderTable(a *app, headers []string, rows [][]string) error {
	return render.Table(a.out, headers, rows)
}

// renderOutput prints payload as JSON when -o json was asked for, and the
// table otherwise.
//
// Commands go through here rather than testing jsonOutput() themselves so that
// adding a command cannot quietly leave -o json unimplemented — which is how
// alias and guard came to accept the flag and ignore it.
func renderOutput(a *app, headers []string, rows [][]string, payload any) error {
	if a.jsonOutput() {
		return writeJSON(a, payload)
	}
	return renderTable(a, headers, rows)
}

// writeJSON encodes payload to stdout.
//
// A nil slice is emitted as [] rather than null: a consumer piping into jq
// should get an empty list when there is nothing, not a value it has to
// special-case.
func writeJSON(a *app, payload any) error {
	if v := reflect.ValueOf(payload); v.Kind() == reflect.Slice && v.IsNil() {
		payload = reflect.MakeSlice(v.Type(), 0, 0).Interface()
	}
	enc := json.NewEncoder(a.out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// resolveContext turns a name typed on the command line into a context that
// exists: "." is the current context, an alias is expanded, and whatever is
// left is checked against the kubeconfig.
//
// Every command taking a context name goes through here. Completion offers
// aliases for all of them, so a command that skipped the alias step would be
// suggesting inputs it then rejects — which is exactly what rename, delete,
// doctor and guard did before this existed.
func resolveContext(a *app, cfg *clientcmdapi.Config, name string) (string, error) {
	if name == "." {
		if cfg.CurrentContext == "" {
			return "", fmt.Errorf("no current context is set")
		}
		return cfg.CurrentContext, nil
	}

	userCfg, err := a.userConfig()
	if err != nil {
		return "", err
	}
	target := userCfg.ResolveAlias(name)
	if !contexts.Exists(cfg, target) {
		// Report what the user typed, not what the alias expanded to: being
		// told that "prod-eks-apne2" does not exist when you typed "p" is a
		// worse error than the one it replaced.
		return "", fmt.Errorf("no context named %q", name)
	}
	return target, nil
}

// resolveContexts resolves a list of names, preserving order.
func resolveContexts(a *app, cfg *clientcmdapi.Config, names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, name := range names {
		target, err := resolveContext(a, cfg, name)
		if err != nil {
			return nil, err
		}
		out = append(out, target)
	}
	return out, nil
}

// historyRef reads the "-" / "-N" / --back forms shared by ctx and ns.
//
// Both commands accept the same shorthand and parsed it separately; keeping one
// copy means "back two" cannot come to mean different things in each.
func historyRef(args []string, back int) int {
	if back == 0 && len(args) == 1 {
		if n := contexts.ParseRef(args[0]); n > 0 {
			return n
		}
	}
	return back
}

// contextWithTimeout returns a context bounded by d, or an unbounded one when d
// is not positive.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), d)
}

// dedupe removes repeats, keeping the first occurrence and the original order.
//
// Both commands that take a list of contexts want this: naming one twice is a
// typo, not a request to export it twice or run the command against it twice.
func dedupe(names []string) []string {
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// joinNames renders a list of names for a message.
func joinNames(names []string) string { return strings.Join(names, ", ") }

// promptingOnStderr returns a view of the app whose questions and notices go to
// stderr instead of stdout.
//
// Only the commands whose payload *is* stdout need it, and they need it badly:
// "kctx export prod > prod.yaml" with a guard prompt on stdout writes the
// question into prod.yaml and leaves the user staring at a silent terminal.
func promptingOnStderr(a *app) *app {
	redirected := *a
	redirected.out = a.errOut
	return &redirected
}

// confirm asks a yes/no question, defaulting to no. It returns true
// immediately when --yes was given.
func confirm(a *app, question string) (bool, error) {
	if a.opts.assumeYes {
		return true, nil
	}
	if _, err := fmt.Fprintf(a.out, "%s [y/N]: ", question); err != nil {
		return false, err
	}

	answer, err := readLine(a)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// confirmPhrase asks the user to retype an exact phrase. This is the guard for
// operations too destructive for a one-keystroke yes.
func confirmPhrase(a *app, prompt, phrase string) (bool, error) {
	if a.opts.assumeYes {
		return true, nil
	}
	if _, err := fmt.Fprintf(a.out, "%s\nType %q to continue: ", prompt, phrase); err != nil {
		return false, err
	}

	answer, err := readLine(a)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(answer) == phrase, nil
}

// readLine reads one line from the app's input stream.
func readLine(a *app) (string, error) {
	line, err := a.stdin().ReadString('\n')
	if err != nil && line == "" {
		// EOF on a closed or empty stdin means "no answer", which the callers
		// treat as a decline rather than a failure.
		return "", nil
	}
	return line, nil
}
