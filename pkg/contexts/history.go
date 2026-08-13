package contexts

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/somaz94/kube-ctx/pkg/paths"
)

const (
	// historyLimit is how many previous contexts are remembered.
	historyLimit = 20
	// historyPerm matches the rest of kube-ctx's owner-only file mode.
	historyPerm = 0o600
)

// History is the stack of contexts that were switched away from, most recent
// first. It backs "kctx ctx -" and "kctx ctx -2".
//
// A scope suffix keeps one shell's history separate from another's: in shell
// hook mode each shell has its own current context, so a single global stack
// would make "-" jump somewhere the user never was in this terminal.
type History struct {
	path  string
	limit int
}

// NewHistory returns the history stack for a scope. An empty scope is the
// global stack used when no shell-local isolation is active.
func NewHistory(scope string) (*History, error) {
	dir, err := paths.StateDir()
	if err != nil {
		return nil, err
	}
	name := "history"
	if scope != "" {
		name += "-" + paths.SanitizeName(scope)
	}
	return &History{path: filepath.Join(dir, name), limit: historyLimit}, nil
}

// Path returns the backing file path.
func (h *History) Path() string { return h.path }

// Push records name as the most recent previous context. An empty name, or a
// repeat of the newest entry, is ignored so that switching to the context you
// are already on does not bury the real previous one.
func (h *History) Push(name string) error {
	if name == "" {
		return nil
	}
	entries, err := h.List()
	if err != nil {
		return err
	}
	if len(entries) > 0 && entries[0] == name {
		return nil
	}

	entries = append([]string{name}, entries...)
	if len(entries) > h.limit {
		entries = entries[:h.limit]
	}
	if err := paths.EnsureDir(filepath.Dir(h.path)); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data := strings.Join(entries, "\n") + "\n"
	if err := os.WriteFile(h.path, []byte(data), historyPerm); err != nil {
		return fmt.Errorf("write history: %w", err)
	}
	return nil
}

// List returns the remembered contexts, most recent first. A missing history
// file is an empty stack, not an error.
func (h *History) List() ([]string, error) {
	data, err := os.ReadFile(h.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}

	var entries []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			entries = append(entries, line)
		}
	}
	return entries, nil
}

// Lookup resolves the nth previous context, where n starts at 1.
func (h *History) Lookup(n int) (string, error) {
	if n < 1 {
		return "", fmt.Errorf("history position must be 1 or greater, got %d", n)
	}
	entries, err := h.List()
	if err != nil {
		return "", err
	}
	if len(entries) < n {
		return "", fmt.Errorf("no context %d step(s) back in history", n)
	}
	return entries[n-1], nil
}

// ParseRef interprets the "-" and "-N" arguments as a history position. It
// returns 0 when arg is an ordinary context name.
func ParseRef(arg string) int {
	if arg == "-" {
		return 1
	}
	if !strings.HasPrefix(arg, "-") {
		return 0
	}
	n, err := strconv.Atoi(arg[1:])
	if err != nil || n < 1 {
		return 0
	}
	return n
}
