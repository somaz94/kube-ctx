package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name   string
		envVar string
		envVal string
		fn     func() (string, error)
		want   string
	}{
		{"config from XDG", "XDG_CONFIG_HOME", "/xdg/config", ConfigDir, "/xdg/config/kube-ctx"},
		{"config fallback", "XDG_CONFIG_HOME", "", ConfigDir, filepath.Join(home, ".config", "kube-ctx")},
		{"cache from XDG", "XDG_CACHE_HOME", "/xdg/cache", CacheDir, "/xdg/cache/kube-ctx"},
		{"cache fallback", "XDG_CACHE_HOME", "", CacheDir, filepath.Join(home, ".cache", "kube-ctx")},
		{"state from XDG", "XDG_STATE_HOME", "/xdg/state", StateDir, "/xdg/state/kube-ctx"},
		{"state fallback", "XDG_STATE_HOME", "", StateDir, filepath.Join(home, ".local", "state", "kube-ctx")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envVar, tt.envVal)
			got, err := tt.fn()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected a directory")
	}
	if perm := info.Mode().Perm(); perm != dirPerm {
		t.Errorf("perm = %o, want %o", perm, dirPerm)
	}

	// Creating an existing directory must stay a no-op.
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir (second call): %v", err)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"dev", "dev"},
		{"arn:aws:eks:ap-northeast-2:1:cluster/prod", "arn_aws_eks_ap-northeast-2_1_cluster_prod"},
		{`win\path`, "win_path"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := SanitizeName(tt.in); got != tt.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
