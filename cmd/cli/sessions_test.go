package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/somaz94/kube-ctx/pkg/shellenv"
)

// sessionFiles returns the session copies on disk.
func sessionFiles(t *testing.T, h *harness) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(
		filepath.Dir(h.kubeconfig), "state", "kube-ctx", "shells", "*.yaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return matches
}

// startSession makes the harness look like a shell with the hook installed and
// switches once, which is what creates a session copy.
func startSession(t *testing.T, h *harness, context string) string {
	t.Helper()
	t.Setenv(shellenv.EnvFile, filepath.Join(t.TempDir(), "env"))
	t.Setenv("SHELL", "/bin/bash")

	if err := h.run("ctx", context); err != nil {
		t.Fatalf("ctx %s: %v", context, err)
	}
	files := sessionFiles(t, h)
	if len(files) == 0 {
		t.Fatal("no session copy was created")
	}
	return files[len(files)-1]
}

// ageSession backdates a session copy so the sweep considers it abandoned.
func ageSession(t *testing.T, path string, age time.Duration) {
	t.Helper()
	when := time.Now().Add(-age)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func TestSessionsList(t *testing.T) {
	h := newHarness(t, defaultSpec())

	if err := h.run("sessions"); err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if !strings.Contains(h.stderr(), "No shell sessions") {
		t.Errorf("stderr = %q", h.stderr())
	}

	startSession(t, h, "prod")
	if err := h.run("sessions"); err != nil {
		t.Fatalf("sessions: %v", err)
	}
	if !strings.Contains(h.stdout(), "prod") {
		t.Errorf("stdout = %q, want the session's context", h.stdout())
	}

	if err := h.run("sessions", "-o", "json"); err != nil {
		t.Fatalf("sessions -o json: %v", err)
	}
	var list []shellenv.Info
	if err := json.Unmarshal([]byte(h.stdout()), &list); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, h.stdout())
	}
	if len(list) != 1 || list[0].Context != "prod" {
		t.Errorf("sessions = %+v", list)
	}
}

func TestSessionsCleanRespectsAge(t *testing.T) {
	h := newHarness(t, defaultSpec())
	path := startSession(t, h, "prod")

	// Recently used: left alone, even though the shell that owned it is gone.
	if err := h.run("sessions", "--clean"); err != nil {
		t.Fatalf("sessions --clean: %v", err)
	}
	if !strings.Contains(h.stdout(), "Removed 0") {
		t.Errorf("stdout = %q", h.stdout())
	}
	if len(sessionFiles(t, h)) != 1 {
		t.Error("a session in use was removed")
	}

	ageSession(t, path, 30*24*time.Hour)
	if err := h.run("sessions", "--clean"); err != nil {
		t.Fatalf("sessions --clean: %v", err)
	}
	if !strings.Contains(h.stdout(), "Removed 1") {
		t.Errorf("stdout = %q", h.stdout())
	}
	if len(sessionFiles(t, h)) != 0 {
		t.Error("the abandoned session survived")
	}
}

// $KUBECONFIG points at this shell's own copy, so removing it would break every
// kubectl in the terminal that asked for the cleanup.
func TestSessionsCleanAllKeepsTheCurrentOne(t *testing.T) {
	h := newHarness(t, defaultSpec())
	mine := startSession(t, h, "prod")
	t.Setenv(EnvShellID, strings.TrimSuffix(filepath.Base(mine), ".yaml"))

	// A second, abandoned copy belonging to no live shell.
	other := filepath.Join(filepath.Dir(mine), "abandoned.yaml")
	if err := os.WriteFile(other, []byte("apiVersion: v1\nkind: Config\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := h.run("sessions", "--clean", "--all"); err != nil {
		t.Fatalf("sessions --clean --all: %v", err)
	}
	if !strings.Contains(h.stdout(), "Removed 1") {
		t.Errorf("stdout = %q, want only the abandoned one removed", h.stdout())
	}
	if _, err := os.Stat(mine); err != nil {
		t.Errorf("this shell's own session was removed: %v", err)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Error("the abandoned session survived --all")
	}
}

// Age is time since last use. Running any kube-ctx command in a session is
// proof the shell is alive, and without that a terminal open longer than the
// sweep window loses its kubeconfig mid-use.
func TestRunningInASessionKeepsItAlive(t *testing.T) {
	h := newHarness(t, defaultSpec())
	path := startSession(t, h, "prod")
	ageSession(t, path, 30*24*time.Hour)

	t.Setenv(EnvShellID, strings.TrimSuffix(filepath.Base(path), ".yaml"))
	if err := h.run("current"); err != nil {
		t.Fatalf("current: %v", err)
	}

	// Reported from another shell, so "current" does not shield it.
	t.Setenv(EnvShellID, "")
	if err := h.run("sessions", "--clean"); err != nil {
		t.Fatalf("sessions --clean: %v", err)
	}
	if !strings.Contains(h.stdout(), "Removed 0") {
		t.Errorf("stdout = %q; the session was in use", h.stdout())
	}
}

func TestDeleteAcceptsDelAndRm(t *testing.T) {
	for _, name := range []string{"del", "rm"} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, defaultSpec())
			if err := h.run(name, "staging", "--yes"); err != nil {
				t.Fatalf("%s staging: %v", name, err)
			}
			if _, ok := h.config().Contexts["staging"]; ok {
				t.Errorf("%s did not delete the context", name)
			}
		})
	}
}

func TestHumanAge(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{90 * time.Minute, "1h ago"},
		{5 * time.Minute, "5m ago"},
		{50 * time.Hour, "2d ago"},
	}
	for _, tt := range tests {
		if got := humanAge(tt.d); got != tt.want {
			t.Errorf("humanAge(%s) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
