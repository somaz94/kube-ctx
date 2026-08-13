package shellenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/somaz94/kube-ctx/internal/testutil"
)

// isolate points the state directory at a temp dir.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return filepath.Join(dir, "kube-ctx", sessionSubdir)
}

func TestNewWritesPrivateCopy(t *testing.T) {
	sessions := isolate(t)
	cfg := testutil.Config(testutil.Spec{
		Current:  "prod",
		Contexts: []testutil.Ctx{{Name: "dev"}, {Name: "prod"}},
	})

	s, err := New(cfg, "prod")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if filepath.Dir(s.Path) != sessions {
		t.Errorf("session written to %q, want a file under %q", s.Path, sessions)
	}
	if s.ID == "" || !strings.HasPrefix(filepath.Base(s.Path), s.ID) {
		t.Errorf("path %q does not carry the session id %q", s.Path, s.ID)
	}

	info, err := os.Stat(s.Path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != filePerm {
		t.Errorf("session perm = %o, want %o — it holds credentials", perm, filePerm)
	}

	// The copy is self-contained and carries the requested context.
	got := testutil.Read(t, s.Path)
	if got.CurrentContext != "prod" {
		t.Errorf("copy current-context = %q, want prod", got.CurrentContext)
	}
	if len(got.Contexts) != 2 {
		t.Errorf("copy has %d contexts, want the full merged set", len(got.Contexts))
	}
}

func TestSessionsAreDistinct(t *testing.T) {
	isolate(t)
	cfg := testutil.Config(testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})

	a, err := New(cfg, "dev")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	b, err := New(cfg, "dev")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if a.ID == b.ID || a.Path == b.Path {
		t.Errorf("two sessions collided: %+v vs %+v", a, b)
	}
}

func TestRemoveDeletesConfigAndHistory(t *testing.T) {
	isolate(t)
	cfg := testutil.Config(testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})

	s, err := New(cfg, "dev")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	state := filepath.Join(os.Getenv("XDG_STATE_HOME"), "kube-ctx")
	history := filepath.Join(state, "history-"+s.ID)
	nsHistory := filepath.Join(state, "history-"+s.ID+"-ns-dev")
	other := filepath.Join(state, "history")
	for _, p := range []string{history, nsHistory, other} {
		if err := os.WriteFile(p, []byte("dev\n"), filePerm); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	if err := s.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for _, p := range []string{s.Path, history, nsHistory} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s survived Remove", filepath.Base(p))
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("Remove deleted the global history")
	}

	// Removing twice is not an error: a shell may exit after a GC sweep.
	if err := s.Remove(); err != nil {
		t.Errorf("second Remove: %v", err)
	}
}

func TestEnvAndExports(t *testing.T) {
	isolate(t)
	s := &Session{ID: "abc123", Path: "/tmp/kube-ctx/abc123.yaml", Context: "prod"}

	env := s.Env(1)
	want := map[string]string{
		EnvKubeconfig: s.Path,
		EnvShellID:    "abc123",
		EnvActive:     "prod",
		EnvDepth:      "1",
	}
	if len(env) != len(want) {
		t.Fatalf("Env = %v, want %d entries", env, len(want))
	}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if want[key] != value {
			t.Errorf("%s = %q, want %q", key, value, want[key])
		}
	}

	posix := s.Exports(Zsh, 1)
	if !strings.Contains(posix, "export KUBECONFIG='/tmp/kube-ctx/abc123.yaml'") {
		t.Errorf("zsh exports:\n%s", posix)
	}
	fish := s.Exports(Fish, 1)
	if !strings.Contains(fish, "set -gx KUBECONFIG '/tmp/kube-ctx/abc123.yaml'") {
		t.Errorf("fish exports:\n%s", fish)
	}
	if strings.Count(posix, "\n") != 4 {
		t.Errorf("expected one line per variable:\n%s", posix)
	}
}

func TestExportsQuoteHostileValues(t *testing.T) {
	s := &Session{ID: "x", Path: "/tmp/it's here/config.yaml", Context: "prod"}

	got := s.Exports(Bash, 0)
	if !strings.Contains(got, `'/tmp/it'\''s here/config.yaml'`) {
		t.Errorf("single quote not escaped:\n%s", got)
	}
	if strings.Contains(got, "$(") {
		t.Errorf("value must not be left expandable:\n%s", got)
	}
}

func TestGCRemovesOnlyStaleSessions(t *testing.T) {
	sessions := isolate(t)
	cfg := testutil.Config(testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})

	fresh, err := New(cfg, "dev")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stale, err := New(cfg, "dev")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(stale.Path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := GC(DefaultMaxAge); err != nil {
		t.Fatalf("GC: %v", err)
	}
	if _, err := os.Stat(stale.Path); !os.IsNotExist(err) {
		t.Error("stale session survived GC")
	}
	if _, err := os.Stat(fresh.Path); err != nil {
		t.Errorf("fresh session was swept: %v", err)
	}

	entries, err := os.ReadDir(sessions)
	if err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d sessions after GC, want 1", len(entries))
	}
}

func TestGCWithoutSessionDir(t *testing.T) {
	isolate(t)
	if err := GC(time.Hour); err != nil {
		t.Errorf("GC on a missing directory should be a no-op, got %v", err)
	}
}

func TestActiveAndDepth(t *testing.T) {
	t.Setenv(EnvShellID, "")
	t.Setenv(EnvDepth, "")
	if Active() {
		t.Error("Active() = true outside a managed shell")
	}
	if Depth() != 0 {
		t.Errorf("Depth() = %d, want 0", Depth())
	}

	t.Setenv(EnvShellID, "abc")
	t.Setenv(EnvDepth, "2")
	if !Active() {
		t.Error("Active() = false inside a managed shell")
	}
	if Depth() != 2 {
		t.Errorf("Depth() = %d, want 2", Depth())
	}

	t.Setenv(EnvDepth, "not a number")
	if Depth() != 0 {
		t.Errorf("Depth() = %d for a malformed value, want 0", Depth())
	}
}

func TestNewFailsWhenStateDirIsAFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "kube-ctx")
	if err := os.WriteFile(blocker, []byte("x"), filePerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("XDG_STATE_HOME", dir)

	cfg := testutil.Config(testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})
	if _, err := New(cfg, "dev"); err == nil {
		t.Error("expected an error when the state directory cannot be created")
	}
}

func TestSessionPathsWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	cfg := testutil.Config(testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})
	if _, err := New(cfg, "dev"); err == nil {
		t.Error("New: expected an error with no resolvable state directory")
	}
	if err := GC(time.Hour); err == nil {
		t.Error("GC: expected an error with no resolvable state directory")
	}
	if err := (&Session{ID: "x", Path: filepath.Join(t.TempDir(), "absent")}).Remove(); err == nil {
		t.Error("Remove: expected an error with no resolvable state directory")
	}
}
