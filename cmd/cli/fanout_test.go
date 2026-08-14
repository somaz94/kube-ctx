package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/somaz94/kube-ctx/internal/testutil"
)

// fanoutRun stubs the spawner with one that is safe to call from several
// goroutines at once and reports which context each child was pinned to.
//
// The context is read back out of the child's own $KUBECONFIG rather than
// assumed, because "did each child get a session of its own" is the whole
// question a fan-out has to answer.
func fanoutRun(t *testing.T, behave func(ctxName string, stdout, stderr io.Writer) error) *[]string {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []string
	)

	original := runCommand
	runCommand = func(cmd *exec.Cmd) error {
		path := envValue(cmd.Env, "KUBECONFIG")
		cfg, err := clientcmd.LoadFromFile(path)
		if err != nil {
			return fmt.Errorf("child kubeconfig %s: %w", path, err)
		}
		name := cfg.CurrentContext

		mu.Lock()
		calls = append(calls, name)
		mu.Unlock()

		if behave == nil {
			return nil
		}
		return behave(name, cmd.Stdout, cmd.Stderr)
	}
	t.Cleanup(func() { runCommand = original })
	return &calls
}

// sessionFileCount counts the per-shell kubeconfig copies left on disk.
func sessionFileCount(t *testing.T, h *harness) int {
	t.Helper()
	dir := filepath.Join(filepath.Dir(h.kubeconfig), "state", "kube-ctx", "shells")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("read session dir: %v", err)
	}
	return len(entries)
}

func TestExecFanoutRunsAgainstEveryContext(t *testing.T) {
	h := newHarness(t, defaultSpec())
	calls := fanoutRun(t, func(name string, stdout, _ io.Writer) error {
		fmt.Fprintf(stdout, "hello from %s\n", name)
		return nil
	})

	if err := h.run("exec", "--all", "--", "kubectl", "version"); err != nil {
		t.Fatalf("exec --all: %v", err)
	}

	got := append([]string(nil), *calls...)
	sort.Strings(got)
	if want := "dev prod staging"; strings.Join(got, " ") != want {
		t.Errorf("children ran against %v, want %s", got, want)
	}

	// Each context's output is grouped under its own header rather than
	// interleaved with the others.
	out := h.stdout()
	for _, want := range []string{"== dev", "hello from dev", "== prod", "hello from prod", "== staging"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout = %q, want it to contain %q", out, want)
		}
	}
}

func TestExecFanoutSelectsAndDedupes(t *testing.T) {
	h := newHarness(t, defaultSpec())
	calls := fanoutRun(t, nil)

	if err := h.run("exec", "-c", "prod,dev,prod", "--", "true"); err != nil {
		t.Fatalf("exec -c: %v", err)
	}
	if len(*calls) != 2 {
		t.Errorf("children = %v, want prod and dev once each", *calls)
	}

	// The report keeps the order the contexts were named, not the order they
	// happened to finish in.
	out := h.stdout()
	if i, j := strings.Index(out, "== prod"), strings.Index(out, "== dev"); i < 0 || j < 0 || i > j {
		t.Errorf("stdout = %q, want prod reported before dev", out)
	}
}

func TestExecFanoutResolvesAliases(t *testing.T) {
	h := newHarness(t, defaultSpec())
	if err := h.run("alias", "p", "prod"); err != nil {
		t.Fatalf("alias: %v", err)
	}
	calls := fanoutRun(t, nil)

	if err := h.run("exec", "-c", "p", "--", "true"); err != nil {
		t.Fatalf("exec -c p: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != "prod" {
		t.Errorf("children = %v, want the alias resolved to prod", *calls)
	}
}

// There is no single child status to pass through, so the first failure in the
// order the contexts were named wins — and something non-zero is essential, or
// "kctx exec --all -- kubectl apply && ./ship" ships on a rejected apply.
func TestExecFanoutExitStatus(t *testing.T) {
	h := newHarness(t, defaultSpec())
	fanoutRun(t, func(name string, _, stderr io.Writer) error {
		if name == "dev" {
			fmt.Fprintln(stderr, "it broke")
			// A real non-zero exit, so the *exec.ExitError is the genuine article.
			return exec.Command("sh", "-c", "exit 7").Run()
		}
		return nil
	})

	err := h.run("exec", "-c", "staging,dev", "--", "kubectl", "get", "pods")
	if code := ExitCode(err); code != 7 {
		t.Errorf("ExitCode = %d, want the child's own 7", code)
	}
	if !strings.Contains(h.stdout(), "exit 7") {
		t.Errorf("stdout = %q, want the failing context marked", h.stdout())
	}
	for _, want := range []string{"it broke", "1 of 2 context(s) failed: dev"} {
		if !strings.Contains(h.stderr(), want) {
			t.Errorf("stderr = %q, want it to contain %q", h.stderr(), want)
		}
	}
}

func TestExecFanoutSucceedsQuietly(t *testing.T) {
	h := newHarness(t, defaultSpec())
	fanoutRun(t, nil)

	if err := h.run("exec", "--all", "--", "true"); err != nil {
		t.Fatalf("exec --all: %v", err)
	}
	if strings.Contains(h.stderr(), "failed") {
		t.Errorf("stderr = %q, want no failure summary", h.stderr())
	}
}

func TestExecFanoutJSON(t *testing.T) {
	h := newHarness(t, defaultSpec())
	fanoutRun(t, func(name string, stdout, stderr io.Writer) error {
		fmt.Fprintf(stdout, "out-%s", name)
		fmt.Fprintf(stderr, "err-%s", name)
		return nil
	})

	if err := h.run("exec", "-c", "dev,prod", "-o", "json", "--", "true"); err != nil {
		t.Fatalf("exec -o json: %v", err)
	}

	var results []fanoutResult
	if err := json.Unmarshal([]byte(h.stdout()), &results); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, h.stdout())
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v, want 2", results)
	}
	if results[0].Context != "dev" || results[0].Stdout != "out-dev" || results[0].Stderr != "err-dev" {
		t.Errorf("results[0] = %+v", results[0])
	}
	// The captured output belongs in the payload, not printed alongside it.
	if strings.Contains(h.stdout(), "== dev") {
		t.Errorf("stdout = %q, want no table alongside the JSON", h.stdout())
	}
}

// A guard that asked once the command had already reached half the clusters
// would not be a guard, and the question must not land in the payload.
func TestExecFanoutGuardAsksBeforeAnythingRuns(t *testing.T) {
	h := newHarness(t, defaultSpec())
	writeUserConfig(t, guardConfirmConfig)
	captureRunNoop(t)
	h.stdin("no\n")

	err := h.run("exec", "--all", "--", "kubectl", "delete", "ns", "api")
	if code := ExitCode(err); code != ExitAborted {
		t.Errorf("ExitCode = %d, want %d", code, ExitAborted)
	}
	if h.stdout() != "" {
		t.Errorf("stdout = %q; a declined fan-out writes nothing at all", h.stdout())
	}
	if !strings.Contains(h.stderr(), "Aborted") {
		t.Errorf("stderr = %q, want the prompt and the verdict", h.stderr())
	}
}

func TestExecFanoutRejectsBadInput(t *testing.T) {
	h := newHarness(t, defaultSpec())
	captureRunNoop(t)

	// Choosing contexts two ways at once: picking one silently would run the
	// command against a set the user did not ask for.
	err := h.run("exec", "--all", "prod", "--", "true")
	if err == nil || !strings.Contains(err.Error(), "prod") {
		t.Errorf("err = %v, want the stray positional named", err)
	}
	if err := h.run("exec", "--all"); err == nil || !strings.Contains(err.Error(), "no command") {
		t.Errorf("err = %v, want a complaint about the missing command", err)
	}
	if err := h.run("exec"); err == nil || !strings.Contains(err.Error(), "context and a command") {
		t.Errorf("err = %v, want a complaint naming both missing pieces", err)
	}
	if err := h.run("exec", "-c", "nope", "--", "true"); err == nil {
		t.Error("an unknown context must fail before anything runs")
	}
	if err := h.run("exec", "--all", "-c", "dev", "--", "true"); err == nil {
		t.Error("--all and --context are alternatives")
	}
}

func TestExecFanoutWithoutContexts(t *testing.T) {
	h := newHarness(t, testutil.Spec{})
	captureRunNoop(t)

	if err := h.run("exec", "--all", "--", "true"); err == nil {
		t.Error("a kubeconfig with no contexts has nothing to fan out to")
	}
}

// Every child gets a copy of the merged kubeconfig — every cluster, token and
// certificate in it — so leaving one behind per context is a real leak.
func TestExecFanoutRemovesItsSessions(t *testing.T) {
	h := newHarness(t, defaultSpec())
	fanoutRun(t, nil)

	if err := h.run("exec", "--all", "-p", "1", "--", "true"); err != nil {
		t.Fatalf("exec --all: %v", err)
	}
	if got := sessionFileCount(t, h); got != 0 {
		t.Errorf("%d session file(s) left behind", got)
	}
}

func TestDedupeKeepsTheFirstOccurrence(t *testing.T) {
	got := dedupe([]string{"b", "a", "b", "c", "a"})
	if want := "b a c"; strings.Join(got, " ") != want {
		t.Errorf("dedupe = %v, want %s", got, want)
	}
	if got := dedupe(nil); len(got) != 0 {
		t.Errorf("dedupe(nil) = %v, want empty", got)
	}
}

// A command that cannot be started at all is kube-ctx failing, not the command
// returning non-zero, and the two read differently in the report.
func TestExecFanoutReportsASpawnFailure(t *testing.T) {
	h := newHarness(t, defaultSpec())
	fanoutRun(t, func(string, io.Writer, io.Writer) error {
		return errors.New("no such file or directory")
	})

	err := h.run("exec", "-c", "dev", "--", "definitely-not-a-command")
	if code := ExitCode(err); code != ExitFailure {
		t.Errorf("ExitCode = %d, want %d", code, ExitFailure)
	}
	if !strings.Contains(h.stderr(), "no such file or directory") {
		t.Errorf("stderr = %q, want the reason", h.stderr())
	}
}
