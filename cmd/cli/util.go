package cli

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/somaz94/kube-ctx/pkg/render"
)

// renderTable prints a table to stdout.
func renderTable(a *app, headers []string, rows [][]string) error {
	return render.Table(a.out, headers, rows)
}

// contextWithTimeout returns a context bounded by d, or an unbounded one when d
// is not positive.
func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), d)
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
	line, err := bufio.NewReader(a.in).ReadString('\n')
	if err != nil && line == "" {
		// EOF on a closed or empty stdin means "no answer", which the callers
		// treat as a decline rather than a failure.
		return "", nil
	}
	return line, nil
}
