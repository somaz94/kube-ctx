// Package paths resolves the on-disk locations kube-ctx uses for its own
// configuration, cached data, and mutable state.
//
// The XDG base directory variables win when set; otherwise the conventional
// ~/.config, ~/.cache and ~/.local/state fallbacks are used. Keeping this in
// one place means tests can redirect every kube-ctx write with two t.Setenv
// calls instead of stubbing each caller.
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// appName is the per-user subdirectory every kube-ctx path lives under.
const appName = "kube-ctx"

// dirPerm is used for every directory kube-ctx creates. Kubeconfig copies and
// backups hold credentials, so nothing here is group- or world-readable.
const dirPerm = 0o700

// ConfigDir returns the directory holding the user-editable config.yaml.
func ConfigDir() (string, error) {
	return resolve("XDG_CONFIG_HOME", ".config")
}

// CacheDir returns the directory holding regenerable data such as the
// namespace list cache.
func CacheDir() (string, error) {
	return resolve("XDG_CACHE_HOME", ".cache")
}

// StateDir returns the directory holding mutable state that must survive a
// reboot but is not user-editable: context history, kubeconfig backups, and
// the per-shell kubeconfig copies.
func StateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if base := os.Getenv("XDG_STATE_HOME"); base != "" {
		return filepath.Join(base, appName), nil
	}
	return filepath.Join(home, ".local", "state", appName), nil
}

// resolve returns $envVar/kube-ctx when envVar is set, else ~/fallback/kube-ctx.
func resolve(envVar, fallback string) (string, error) {
	if base := os.Getenv(envVar); base != "" {
		return filepath.Join(base, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback, appName), nil
}

// EnsureDir creates dir and any missing parents with owner-only permissions.
func EnsureDir(dir string) error {
	return os.MkdirAll(dir, dirPerm)
}

// SanitizeName makes a Kubernetes context name safe to embed in a filename.
// EKS names contexts after an ARN, which is full of colons and slashes, so an
// unescaped name would silently write outside the intended directory.
func SanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', ':', '\\':
			return '_'
		}
		return r
	}, name)
}
