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

// renderOutput prints payload as JSON under -o json and the table otherwise,
// so a table routed through here cannot accept -o json and ignore it.
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
// exists: "." is the current context, a context's own name beats an alias of
// the same name, and "@name" forces the alias.
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
	target := name
	if strings.HasPrefix(name, "@") || !contexts.Exists(cfg, name) {
		target = userCfg.ResolveAlias(name)
	}
	// A context may itself be named "@team", and completion offers it as typed.
	// With no alias to force, that name can only mean the context.
	if !contexts.Exists(cfg, target) && contexts.Exists(cfg, name) {
		target = name
	}
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
func historyRef(args []string, back int) int {
	if back == 0 && len(args) == 1 {
		if n := contexts.ParseRef(args[0]); n > 0 {
			return n
		}
	}
	return back
}

// stepsBack renders a history distance as "1 step" or "N steps".
func stepsBack(n int) string {
	if n == 1 {
		return "1 step"
	}
	return fmt.Sprintf("%d steps", n)
}

// contextWithTimeout returns a context bounded by d, or an unbounded one when d
// is not positive.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), d)
}

// dedupe removes repeats, keeping the first occurrence and the original order:
// naming a context twice is a typo, not a request to act on it twice.
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
	redirected.prompts = a.stdin() // one reader across views, or the next answer is swallowed
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

// trimError shortens the errors client-go produces into something that fits a
// table cell.
//
// The newline is cut first, and that is the load-bearing half: render measures
// a cell's visible width over the whole string, so one embedded newline sets
// its column to the width of the entire message and destroys the alignment of
// every other row — the one property pkg/render exists to preserve. Trimming
// runs rather than bytes keeps a multi-byte character from being cut in half.
func trimError(msg string, max int) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	runes := []rune(msg)
	if len(runes) <= max || max < 1 {
		return msg
	}
	return string(runes[:max-1]) + "…"
}

// boldIfCurrent bolds name when it is the current context or namespace.
func boldIfCurrent(pal render.Palette, name, current string) string {
	if name == current {
		return pal.Bold(name)
	}
	return name
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
