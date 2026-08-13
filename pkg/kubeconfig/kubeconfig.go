// Package kubeconfig loads and persists kubeconfig files.
//
// Every read and write goes through client-go's clientcmd, which is what
// kubectl itself uses. That matters most on write: when $KUBECONFIG lists
// several files, clientcmd knows which of them a given stanza came from and
// writes the change back to that file. Tools that parse and re-emit the YAML
// themselves collapse the list into one file and drop comments and key order.
package kubeconfig

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/somaz94/kube-ctx/pkg/paths"
)

const (
	// backupDirName is the StateDir subdirectory holding timestamped backups.
	backupDirName = "backups"
	// defaultBackupKeep is how many backup generations are retained.
	defaultBackupKeep = 10
	// filePerm is used for every file kube-ctx writes; kubeconfigs carry
	// credentials, so nothing is group- or world-readable.
	filePerm = 0o600
)

// Loader reads and writes the kubeconfig set selected by --kubeconfig, by
// $KUBECONFIG, or by the default ~/.kube/config.
type Loader struct {
	pathOptions *clientcmd.PathOptions
	backupKeep  int
}

// New returns a Loader. An empty explicitPath means "use $KUBECONFIG, else the
// default file", matching kubectl's own resolution order.
func New(explicitPath string) *Loader {
	po := clientcmd.NewDefaultPathOptions()
	if explicitPath != "" {
		po.LoadingRules.ExplicitPath = explicitPath
	}
	return &Loader{pathOptions: po, backupKeep: defaultBackupKeep}
}

// PathOptions exposes the underlying clientcmd path options so callers that
// need to build their own clients reuse the exact same resolution.
func (l *Loader) PathOptions() *clientcmd.PathOptions {
	return l.pathOptions
}

// Precedence lists the kubeconfig files in load order, highest priority first.
func (l *Loader) Precedence() []string {
	return l.pathOptions.GetLoadingPrecedence()
}

// Load returns the merged view of every kubeconfig file in the precedence list.
func (l *Loader) Load() (*clientcmdapi.Config, error) {
	cfg, err := l.pathOptions.GetStartingConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	return cfg, nil
}

// SaveOption customizes Save.
type SaveOption func(*saveOptions)

type saveOptions struct {
	backup bool
}

// WithBackup snapshots every kubeconfig file before the write. Reserved for
// destructive edits (rename, delete) — a plain context or namespace switch is
// frequent and trivially reversible, so it does not pay the copy.
func WithBackup() SaveOption {
	return func(o *saveOptions) { o.backup = true }
}

// Save writes cfg back through clientcmd, distributing each change to the file
// it originated from.
func (l *Loader) Save(cfg *clientcmdapi.Config, opts ...SaveOption) error {
	var o saveOptions
	for _, fn := range opts {
		fn(&o)
	}
	if o.backup {
		if err := l.Backup(); err != nil {
			return err
		}
	}
	// relativizePaths=false: leave certificate and exec-plugin paths exactly as
	// the user wrote them. Rewriting them relative to the config file would put
	// unrelated churn in the diff of a file we were only asked to switch.
	if err := clientcmd.ModifyConfig(l.pathOptions, *cfg, false); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}
	return nil
}

// Backup copies every existing kubeconfig file into a timestamped directory
// under the state dir, then prunes the oldest generations.
func (l *Loader) Backup() error {
	state, err := paths.StateDir()
	if err != nil {
		return err
	}
	root := filepath.Join(state, backupDirName)
	if err := paths.EnsureDir(root); err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}
	// A fixed-width timestamp prefix keeps lexical order chronological; the
	// random suffix keeps two backups in the same millisecond from colliding.
	dir, err := os.MkdirTemp(root, time.Now().UTC().Format("20060102-150405.000")+"-")
	if err != nil {
		return fmt.Errorf("create backup dir: %w", err)
	}

	copied := 0
	for i, src := range l.Precedence() {
		if _, err := os.Stat(src); err != nil {
			continue // a listed-but-absent file is normal, not an error
		}
		// The index prefix keeps precedence order recoverable even when two
		// files in the list share a basename.
		dst := filepath.Join(dir, fmt.Sprintf("%02d-%s", i, filepath.Base(src)))
		if err := copyFile(src, dst); err != nil {
			return err
		}
		copied++
	}
	if copied == 0 {
		return os.Remove(dir)
	}
	return rotateBackups(root, l.backupKeep)
}

// RestConfig builds a client connection config for one context. An empty
// ctxName means the current context.
func (l *Loader) RestConfig(ctxName string) (*rest.Config, error) {
	overrides := &clientcmd.ConfigOverrides{}
	if ctxName != "" {
		overrides.CurrentContext = ctxName
	}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(l.pathOptions.LoadingRules, overrides)
	rc, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build client config for context %q: %w", ctxName, err)
	}
	return rc, nil
}

// copyFile copies src to dst with owner-only permissions.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dst, err)
	}
	return nil
}

// rotateBackups deletes the oldest generations beyond keep. Directory names are
// UTC timestamps, so lexical order is chronological order.
func rotateBackups(root string, keep int) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("read backup dir: %w", err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) <= keep {
		return nil
	}
	sort.Strings(dirs)
	for _, name := range dirs[:len(dirs)-keep] {
		if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
			return fmt.Errorf("prune backup %s: %w", name, err)
		}
	}
	return nil
}
