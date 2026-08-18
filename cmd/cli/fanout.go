package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/contexts"
	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// defaultFanoutParallel bounds how many contexts run at once, for the same
// reason pkg/probe bounds its sweep: answering a question about a 40-context
// kubeconfig should not open 40 connections to do it.
const defaultFanoutParallel = 8

// fanoutResult is one context's outcome.
//
// Output is captured rather than streamed, so it is a string here: with several
// commands running at once there is no way to pass their writes straight
// through and still have the result be readable.
type fanoutResult struct {
	Context  string `json:"context"`
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	// Err is kube-ctx failing to run the command at all, as opposed to the
	// command running and returning non-zero.
	Err string `json:"error,omitempty"`
}

// runFanout runs argv against every target at once and reports what each said.
func runFanout(a *app, base *clientcmdapi.Config, targets []string, argv []string, opts execOptions) error {
	// Every guard is answered before anything runs. Asking once the command has
	// already reached half the clusters is not a guard, and the prompt goes to
	// stderr because the collected output is what stdout carries.
	prompt := promptingOnStderr(a)
	// Kept per target rather than recomputed for the report: without -n each
	// context brings its own, and the header has to badge the namespace the
	// command actually ran in.
	namespaces := make([]string, len(targets))
	for i, target := range targets {
		if err := requireGuardConfirmation(prompt, target); err != nil {
			return err
		}
		namespaces[i] = opts.namespace
		if namespaces[i] == "" {
			namespaces[i] = namespaceOf(base, target)
		}
		if err := requireNamespaceGuardConfirmation(prompt, target, namespaces[i]); err != nil {
			return err
		}
	}

	parallel := opts.parallel
	if parallel < 1 {
		parallel = defaultFanoutParallel
	}

	// Ctrl-C reaches the whole foreground process group. Dying alongside the
	// children would skip every deferred Remove and strand a session kubeconfig
	// per context — each one a copy of every cluster, token and cert.
	stop := ignoreInterrupts()
	defer stop()

	results := make([]fanoutResult, len(targets))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup

	for i, target := range targets {
		wg.Add(1)
		go func(i int, target string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = execOne(base, target, argv, opts.namespace)
		}(i, target)
	}
	wg.Wait()

	return reportFanout(a, results, namespaces)
}

// execOne runs argv against one context in a throwaway session.
func execOne(base *clientcmdapi.Config, target string, argv []string, namespace string) fanoutResult {
	result := fanoutResult{Context: target}
	fail := func(err error) fanoutResult {
		result.Err = err.Error()
		result.ExitCode = ExitFailure
		return result
	}

	// Its own copy of the config: each child needs a different current-context,
	// and sharing one would have the goroutines writing over each other.
	cfg := base.DeepCopy()
	cfg.CurrentContext = target
	if namespace != "" {
		if err := contexts.SetNamespace(cfg, target, namespace); err != nil {
			return fail(err)
		}
	}

	session, err := shellenv.New(cfg, target)
	if err != nil {
		return fail(err)
	}
	defer func() { _ = session.Remove() }()

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), session.Env(shellenv.Depth())...)
	// No stdin, and the output is captured rather than passed through: several
	// children cannot share one terminal. Lines from four clusters interleaved
	// are unreadable, and a command that waits on stdin would hang the sweep
	// with nothing on screen to explain why.
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err = runCommand(cmd)
	result.Stdout, result.Stderr = stdout.String(), stderr.String()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = waitStatusCode(exitErr)
			return result
		}
		return fail(fmt.Errorf("run %s: %w", argv[0], err))
	}
	return result
}

// reportFanout prints every context's output and returns the process status.
func reportFanout(a *app, results []fanoutResult, namespaces []string) error {
	if a.jsonOutput() {
		if err := writeJSON(a, results); err != nil {
			return err
		}
		return fanoutExit(results)
	}

	pal := a.palette()
	for i, r := range results {
		header := "== " + pal.Bold(r.Context) + guardSuffix(a, r.Context)
		// The fan-out has the widest blast radius of the three, so a guarded
		// namespace must not be the one thing it runs against in silence.
		if badge := namespaceGuardSuffix(a, r.Context, namespaces[i]); badge != "" {
			header += ", namespace " + pal.Bold(namespaces[i]) + badge
		}
		if r.ExitCode != 0 {
			header += "  " + pal.Red(fmt.Sprintf("exit %d", r.ExitCode))
		}
		if _, err := fmt.Fprintln(a.out, header); err != nil {
			return err
		}
		if err := writeBlock(a.out, r.Stdout); err != nil {
			return err
		}
		// The child's streams stay separate, so a caller can still redirect one
		// without the other.
		if err := writeBlock(a.errOut, r.Stderr); err != nil {
			return err
		}
		if r.Err != "" {
			if _, err := fmt.Fprintln(a.errOut, r.Err); err != nil {
				return err
			}
		}
	}

	if failed := failedContexts(results); len(failed) > 0 {
		fmt.Fprintf(a.errOut, "%d of %d context(s) failed: %s\n",
			len(failed), len(results), joinNames(failed))
	}
	return fanoutExit(results)
}

// writeBlock writes captured output, making sure it ends on a line boundary so
// the next context's header starts in column one.
func writeBlock(w interface{ Write([]byte) (int, error) }, text string) error {
	if text == "" {
		return nil
	}
	if _, err := fmt.Fprint(w, text); err != nil {
		return err
	}
	if text[len(text)-1] != '\n' {
		_, err := fmt.Fprintln(w)
		return err
	}
	return nil
}

// failedContexts names the contexts whose command did not exit zero.
func failedContexts(results []fanoutResult) []string {
	var failed []string
	for _, r := range results {
		if r.ExitCode != 0 {
			failed = append(failed, r.Context)
		}
	}
	return failed
}

// fanoutExit maps the per-context statuses onto one process status.
//
// There is no single child to pass through the way "kctx exec <ctx>" does, so
// the first failure in the order the contexts were named wins. What matters is
// that it is non-zero at all: "kctx exec --all -- kubectl apply -f x && ./ship"
// must not ship when one cluster rejected the apply.
func fanoutExit(results []fanoutResult) error {
	for _, r := range results {
		if r.ExitCode != 0 {
			return &exitError{code: r.ExitCode}
		}
	}
	return nil
}
