// Package shellenv gives a single terminal its own view of the current
// context.
//
// A kubeconfig has exactly one current-context, so switching it changes every
// terminal at once — the failure mode where a switch in one tab silently
// re-points the kubectl you are about to run in another. The fix is to give the
// shell its own copy of the kubeconfig and point $KUBECONFIG at it. This
// package owns those copies: creating them, cleaning them up, and generating
// the shell code that makes the export stick in an interactive session.
package shellenv

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/paths"
)

const (
	// sessionSubdir is the state subdirectory holding per-shell kubeconfigs.
	sessionSubdir = "shells"
	// DefaultMaxAge is how long an orphaned session file is kept. A shell that
	// exits without cleaning up (a closed terminal window, a killed SSH
	// session) leaves its copy behind, so the next session sweeps old ones.
	DefaultMaxAge = 7 * 24 * time.Hour
	// filePerm keeps session kubeconfigs owner-only; they carry credentials.
	filePerm = 0o600
)

// Environment variables a kube-ctx-managed shell carries.
const (
	// EnvKubeconfig is the standard kubeconfig override.
	EnvKubeconfig = "KUBECONFIG"
	// EnvShellID marks a shell as kube-ctx-managed and names its session.
	EnvShellID = "KUBE_CTX_SHELL_ID"
	// EnvActive names the context the session started on, for prompts.
	EnvActive = "KUBE_CTX_ACTIVE"
	// EnvDepth counts nested managed shells.
	EnvDepth = "KUBE_CTX_DEPTH"
	// EnvFile is the path the shell hook asks kube-ctx to write exports to.
	EnvFile = "KUBE_CTX_ENV_FILE"
	// EnvShell names the shell the hook was generated for. $SHELL is the login
	// shell, not the one running the hook, so it cannot decide the syntax of
	// the file the hook is about to source.
	EnvShell = "KUBE_CTX_SHELL"
	// EnvBound records the directory binding this shell has already acted on,
	// so entering a bound tree switches once rather than on every cd inside it
	// — and so a deliberate switch made in there is not undone by the next cd.
	EnvBound = "KUBE_CTX_BOUND"
	// EnvPinned marks a shell opened by "kctx shell", which promised to stay on
	// one context. Directory bindings do not apply inside one.
	EnvPinned = "KUBE_CTX_PINNED"
)

// ExportLine renders one environment assignment in sh's syntax, for callers
// that add a variable of their own to the file the hook sources.
func ExportLine(sh Shell, key, value string) string { return exportLine(sh, key, value) }

// Session is one shell's private kubeconfig copy.
type Session struct {
	// ID identifies the session and scopes its history.
	ID string
	// Path is the private kubeconfig this shell uses.
	Path string
	// Context is the context the session was opened on.
	Context string
}

// New writes cfg to a fresh private kubeconfig and returns the session.
//
// The config passed in is the fully merged view, so the copy is self-contained:
// the shell keeps working even if the original files move.
func New(cfg *clientcmdapi.Config, ctxName string) (*Session, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	if err := paths.EnsureDir(dir); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	id, err := newID()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, id+".yaml")

	// Create the file with owner-only permissions before clientcmd writes to
	// it, so the credentials are never briefly world-readable.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return nil, fmt.Errorf("create session kubeconfig: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("create session kubeconfig: %w", err)
	}
	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("write session kubeconfig: %w", err)
	}

	return &Session{ID: id, Path: path, Context: ctxName}, nil
}

// Remove deletes the session kubeconfig and its history file.
func (s *Session) Remove() error {
	if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove session kubeconfig: %w", err)
	}

	state, err := paths.StateDir()
	if err != nil {
		return err
	}
	// History files are scoped by session ID; a session that ends takes its
	// context and namespace history with it.
	matches, err := filepath.Glob(filepath.Join(state, "history-"+s.ID+"*"))
	if err != nil {
		return err
	}
	for _, path := range matches {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove session history: %w", err)
		}
	}
	return nil
}

// Env returns the environment entries a managed shell needs, in KEY=value form.
func (s *Session) Env(depth int) []string {
	return []string{
		EnvKubeconfig + "=" + s.Path,
		EnvShellID + "=" + s.ID,
		EnvActive + "=" + s.Context,
		fmt.Sprintf("%s=%d", EnvDepth, depth),
	}
}

// Exports renders the session environment as shell code for sh.
func (s *Session) Exports(sh Shell, depth int) string {
	var b strings.Builder
	for _, entry := range s.Env(depth) {
		key, value, _ := strings.Cut(entry, "=")
		b.WriteString(exportLine(sh, key, value))
		b.WriteByte('\n')
	}
	return b.String()
}

// Info describes one session copy found on disk.
type Info struct {
	// ID identifies the session.
	ID string `json:"id"`
	// Path is the private kubeconfig.
	Path string `json:"path"`
	// Context is the context that copy is currently on, read back from it.
	Context string `json:"context,omitempty"`
	// LastUsed is when kube-ctx last ran in the session.
	LastUsed time.Time `json:"lastUsed"`
	// Current marks the session of the shell asking.
	Current bool `json:"current"`
}

// List returns every session copy on disk, newest first.
func List() ([]Info, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session dir: %w", err)
	}

	current := os.Getenv(EnvShellID)
	out := make([]Info, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}
		stat, err := entry.Info()
		if err != nil {
			continue // it went away while we were looking; that is fine
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		path := filepath.Join(dir, entry.Name())

		info := Info{ID: id, Path: path, LastUsed: stat.ModTime(), Current: id == current}
		// Read back rather than remembered: the shell owning this copy switches
		// context in it, and this file is the only record of where it ended up.
		if cfg, err := clientcmd.LoadFromFile(path); err == nil {
			info.Context = cfg.CurrentContext
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastUsed.After(out[j].LastUsed) })
	return out, nil
}

// Touch records that the current session is still in use.
//
// It is what makes an age-based sweep safe. Nothing rewrites a session copy
// except a context switch, so a terminal left open for longer than the sweep
// window without switching would have its kubeconfig deleted out from under it
// — every later kubectl in that shell failing on a file that is no longer
// there. Running kube-ctx at all is proof the shell is alive.
func Touch() error {
	if !Active() {
		return nil
	}
	dir, err := sessionsDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, os.Getenv(EnvShellID)+".yaml")
	now := time.Now()
	return os.Chtimes(path, now, now)
}

// GC removes session kubeconfigs older than maxAge, along with their history.
//
// It is best-effort: a file that cannot be removed is skipped rather than
// failing the command the user actually asked for.
func GC(maxAge time.Duration) error {
	dir, err := sessionsDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read session dir: %w", err)
	}

	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		_ = (&Session{ID: id, Path: filepath.Join(dir, entry.Name())}).Remove()
	}
	return nil
}

// Active reports whether the current process is running inside a managed shell.
func Active() bool { return os.Getenv(EnvShellID) != "" }

// Depth returns how many managed shells deep the process is.
func Depth() int {
	var depth int
	_, _ = fmt.Sscanf(os.Getenv(EnvDepth), "%d", &depth)
	return depth
}

// sessionsDir returns the directory holding per-shell kubeconfigs.
func sessionsDir() (string, error) {
	state, err := paths.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, sessionSubdir), nil
}

// newID returns a random session identifier.
func newID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
