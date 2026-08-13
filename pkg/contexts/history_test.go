package contexts

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// newTestHistory returns a History rooted in a temp state dir.
func newTestHistory(t *testing.T, scope string) *History {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	h, err := NewHistory(scope)
	if err != nil {
		t.Fatalf("NewHistory: %v", err)
	}
	return h
}

func TestHistoryPushAndList(t *testing.T) {
	h := newTestHistory(t, "")

	for _, name := range []string{"dev", "prod", "staging"} {
		if err := h.Push(name); err != nil {
			t.Fatalf("Push(%s): %v", name, err)
		}
	}

	got, err := h.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := []string{"staging", "prod", "dev"}; !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v (most recent first)", got, want)
	}
}

func TestHistoryIgnoresEmptyAndRepeats(t *testing.T) {
	h := newTestHistory(t, "")

	if err := h.Push(""); err != nil {
		t.Fatalf("Push(empty): %v", err)
	}
	if entries, _ := h.List(); len(entries) != 0 {
		t.Errorf("empty push recorded: %v", entries)
	}

	for i := 0; i < 3; i++ {
		if err := h.Push("dev"); err != nil {
			t.Fatalf("Push: %v", err)
		}
	}
	if entries, _ := h.List(); !reflect.DeepEqual(entries, []string{"dev"}) {
		t.Errorf("consecutive duplicates recorded: %v", entries)
	}
}

func TestHistoryTrimsToLimit(t *testing.T) {
	h := newTestHistory(t, "")
	h.limit = 3

	for _, name := range []string{"a", "b", "c", "d", "e"} {
		if err := h.Push(name); err != nil {
			t.Fatalf("Push(%s): %v", name, err)
		}
	}

	got, _ := h.List()
	if want := []string{"e", "d", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("List = %v, want %v", got, want)
	}
}

func TestHistoryLookup(t *testing.T) {
	h := newTestHistory(t, "")
	for _, name := range []string{"dev", "prod"} {
		if err := h.Push(name); err != nil {
			t.Fatalf("Push: %v", err)
		}
	}

	if got, err := h.Lookup(1); err != nil || got != "prod" {
		t.Errorf("Lookup(1) = %q, %v; want prod", got, err)
	}
	if got, err := h.Lookup(2); err != nil || got != "dev" {
		t.Errorf("Lookup(2) = %q, %v; want dev", got, err)
	}
	if _, err := h.Lookup(3); err == nil {
		t.Error("expected an error past the end of history")
	}
	if _, err := h.Lookup(0); err == nil {
		t.Error("expected an error for position 0")
	}
}

func TestHistoryMissingFileIsEmpty(t *testing.T) {
	h := newTestHistory(t, "")

	got, err := h.List()
	if err != nil {
		t.Fatalf("List on missing file: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List = %v, want empty", got)
	}
}

func TestHistoryUnreadableFile(t *testing.T) {
	h := newTestHistory(t, "")
	if err := os.MkdirAll(h.Path(), 0o700); err != nil {
		t.Fatalf("mkdir over history path: %v", err)
	}

	if _, err := h.List(); err == nil {
		t.Error("expected an error reading a directory as the history file")
	}
	if err := h.Push("dev"); err == nil {
		t.Error("expected an error pushing onto an unreadable history")
	}
}

func TestHistoryScopeIsolatesShells(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	global, err := NewHistory("")
	if err != nil {
		t.Fatalf("NewHistory: %v", err)
	}
	scoped, err := NewHistory("shell42")
	if err != nil {
		t.Fatalf("NewHistory(scoped): %v", err)
	}

	if global.Path() == scoped.Path() {
		t.Fatal("scoped history shares the global file")
	}
	if want := filepath.Join(dir, "kube-ctx", "history-shell42"); scoped.Path() != want {
		t.Errorf("scoped path = %q, want %q", scoped.Path(), want)
	}

	// An ARN-shaped context name must not escape the state directory.
	arn, err := NewHistory("ns-arn:aws:eks:ap-northeast-2:123:cluster/prod")
	if err != nil {
		t.Fatalf("NewHistory(arn): %v", err)
	}
	if got := filepath.Dir(arn.Path()); got != filepath.Join(dir, "kube-ctx") {
		t.Errorf("scoped history escaped its directory: %q", arn.Path())
	}
	if err := arn.Push("default"); err != nil {
		t.Errorf("Push into an ARN-scoped history: %v", err)
	}

	if err := global.Push("dev"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if entries, _ := scoped.List(); len(entries) != 0 {
		t.Errorf("scoped history saw the global push: %v", entries)
	}
}

func TestParseRef(t *testing.T) {
	tests := []struct {
		arg  string
		want int
	}{
		{"-", 1},
		{"-1", 1},
		{"-3", 3},
		{"prod", 0},
		{"-0", 0},
		{"-x", 0},
		{"", 0},
		{"-prod", 0},
	}
	for _, tt := range tests {
		if got := ParseRef(tt.arg); got != tt.want {
			t.Errorf("ParseRef(%q) = %d, want %d", tt.arg, got, tt.want)
		}
	}
}
