package testutil

import (
	"path/filepath"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	cfg := Config(Spec{
		Current: "dev",
		Contexts: []Ctx{
			{Name: "dev"},
			{Name: "prod", Cluster: "shared", User: "admin", Namespace: "kube-system", Server: "https://prod:6443"},
		},
	})

	if cfg.CurrentContext != "dev" {
		t.Errorf("CurrentContext = %q, want dev", cfg.CurrentContext)
	}
	if got := cfg.Contexts["dev"].Cluster; got != "dev-cluster" {
		t.Errorf("derived cluster = %q, want dev-cluster", got)
	}
	if got := cfg.Contexts["dev"].AuthInfo; got != "dev-user" {
		t.Errorf("derived user = %q, want dev-user", got)
	}
	if got := cfg.Clusters["dev-cluster"].Server; got != "https://dev.example.com:6443" {
		t.Errorf("derived server = %q", got)
	}
	if got := cfg.Contexts["prod"].Namespace; got != "kube-system" {
		t.Errorf("namespace = %q, want kube-system", got)
	}
	if got := cfg.Clusters["shared"].Server; got != "https://prod:6443" {
		t.Errorf("explicit server = %q", got)
	}
}

func TestWriteAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	Write(t, path, Config(Spec{Current: "dev", Contexts: []Ctx{{Name: "dev"}}}))

	got := Read(t, path)
	if got.CurrentContext != "dev" {
		t.Errorf("round trip lost current context: %q", got.CurrentContext)
	}
	if _, ok := got.Contexts["dev"]; !ok {
		t.Error("round trip lost the dev context")
	}
}
