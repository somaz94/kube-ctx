package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadFromMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if len(cfg.Guards) != len(DefaultGuards()) {
		t.Errorf("got %d guards, want the defaults", len(cfg.Guards))
	}
	if len(cfg.Aliases) != 0 {
		t.Errorf("got aliases %v, want none", cfg.Aliases)
	}
	if cfg.Path() != path {
		t.Errorf("Path = %q, want %q", cfg.Path(), path)
	}
	// The defaults must not require confirmation on a fresh install.
	for _, g := range cfg.Guards {
		if g.Confirm {
			t.Errorf("default guard %q requires confirmation", g.Match)
		}
	}
}

func TestLoadFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := `
aliases:
  p: prod-eks
guards:
  - match: '^prod'
    level: danger
    confirm: true
`
	if err := os.WriteFile(path, []byte(body), filePerm); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if got := cfg.Aliases["p"]; got != "prod-eks" {
		t.Errorf("alias p = %q, want prod-eks", got)
	}
	if len(cfg.Guards) != 1 || cfg.Guards[0].Level != LevelDanger || !cfg.Guards[0].Confirm {
		t.Errorf("guards = %+v", cfg.Guards)
	}
}

func TestLoadFromEmptyFileFallsBackToDefaultGuards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("aliases:\n  p: prod\n"), filePerm); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if len(cfg.Guards) == 0 {
		t.Error("guards should fall back to the defaults when the file defines none")
	}
}

func TestLoadFromInvalidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("aliases: [unclosed"), filePerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadFrom(path); err == nil {
		t.Error("expected a parse error")
	}
}

func TestLoadFromUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadFrom(dir); err == nil {
		t.Error("expected an error reading a directory as a config file")
	}
}

func TestLoadUsesXDGPath(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := filepath.Join(base, "kube-ctx", FileName); cfg.Path() != want {
		t.Errorf("Path = %q, want %q", cfg.Path(), want)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.SetAlias("p", "prod-eks"); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.Aliases["p"]; got != "prod-eks" {
		t.Errorf("alias survived as %q, want prod-eks", got)
	}

	info, err := os.Stat(cfg.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != filePerm {
		t.Errorf("config perm = %o, want %o", perm, filePerm)
	}
}

func TestSaveResolvesPathWhenUnset(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	cfg := &Config{}
	if err := cfg.SetAlias("d", "dev"); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "kube-ctx", FileName)); err != nil {
		t.Errorf("config not written to the default path: %v", err)
	}
}

func TestSaveIntoUnwritableDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), filePerm); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := &Config{path: filepath.Join(file, "config.yaml")}
	if err := cfg.Save(); err == nil {
		t.Error("expected an error saving under a regular file")
	}
}

func TestResolveAlias(t *testing.T) {
	cfg := &Config{Aliases: map[string]string{"p": "prod-eks"}}

	tests := []struct{ in, want string }{
		{"p", "prod-eks"},
		{"@p", "prod-eks"},
		{"dev", "dev"},
		{"@dev", "dev"},
	}
	for _, tt := range tests {
		if got := cfg.ResolveAlias(tt.in); got != tt.want {
			t.Errorf("ResolveAlias(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSetAndDeleteAlias(t *testing.T) {
	cfg := &Config{}

	if err := cfg.SetAlias("@p", "prod"); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}
	if got := cfg.Aliases["p"]; got != "prod" {
		t.Errorf("alias stored as %q, want the @ stripped", got)
	}
	if err := cfg.SetAlias("", "prod"); err == nil {
		t.Error("expected an error for an empty alias")
	}
	if err := cfg.SetAlias("x", ""); err == nil {
		t.Error("expected an error for an empty target")
	}

	if err := cfg.DeleteAlias("p"); err != nil {
		t.Fatalf("DeleteAlias: %v", err)
	}
	if err := cfg.DeleteAlias("p"); err == nil {
		t.Error("expected an error deleting an unknown alias")
	}
}

func TestAliasList(t *testing.T) {
	cfg := &Config{Aliases: map[string]string{"z": "zeta", "a": "alpha"}}

	got := cfg.AliasList()
	want := []AliasPair{{Alias: "a", Target: "alpha"}, {Alias: "z", Target: "zeta"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AliasList = %v, want %v", got, want)
	}
	if len((&Config{}).AliasList()) != 0 {
		t.Error("empty config should list no aliases")
	}
}

// The generated file has to explain the one thing that is otherwise invisible:
// that confirm exists and is off.
func TestSaveWritesADocumentedHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if err := cfg.SetAlias("p", "prod"); err != nil {
		t.Fatalf("SetAlias: %v", err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "confirm: true") {
		t.Errorf("header does not mention confirm:\n%s", data)
	}
	// Saving twice must not stack headers.
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if got := strings.Count(string(again), "# kube-ctx configuration."); got != 1 {
		t.Errorf("header written %d times, want 1", got)
	}
	// And the file must still parse.
	reloaded, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom after save: %v", err)
	}
	if reloaded.ResolveAlias("p") != "prod" {
		t.Error("alias did not survive the round trip")
	}
}

// Once the defaults have been substituted in they are indistinguishable from
// configured rules, so the distinction has to be recorded at load time.
func TestHasGuardsDistinguishesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	if cfg.HasGuards() {
		t.Error("a missing file has no configured guards")
	}

	cfg.AddGuard(Guard{Contexts: []string{"cluster-7"}, Level: LevelDanger})
	if !cfg.HasGuards() {
		t.Error("AddGuard must mark the guards as configured")
	}
	// AddGuard prepends, so the new rule wins over the built-in patterns.
	if got := cfg.Guards[0].Describe(); got != "cluster-7" {
		t.Errorf("Guards[0] = %q, want the new rule first", got)
	}
	if len(cfg.Guards) != len(DefaultGuards())+1 {
		t.Errorf("got %d guards, want the defaults plus one", len(cfg.Guards))
	}
}

func TestRemoveGuard(t *testing.T) {
	cfg := &Config{path: filepath.Join(t.TempDir(), "config.yaml")}
	cfg.AddGuard(Guard{Contexts: []string{"cluster-7"}, Level: LevelDanger})

	if _, err := cfg.RemoveGuard("abc"); err == nil {
		t.Error("a non-numeric position must be rejected")
	}
	if _, err := cfg.RemoveGuard("0"); err == nil {
		t.Error("position 0 must be rejected; the list is 1-based")
	}
	if _, err := cfg.RemoveGuard("99"); err == nil {
		t.Error("a position past the end must be rejected")
	}

	before := len(cfg.Guards)
	removed, err := cfg.RemoveGuard("1")
	if err != nil {
		t.Fatalf("RemoveGuard: %v", err)
	}
	if removed.Describe() != "cluster-7" {
		t.Errorf("removed %q", removed.Describe())
	}
	if len(cfg.Guards) != before-1 {
		t.Errorf("got %d guards, want %d", len(cfg.Guards), before-1)
	}
}

func TestGuardDescribe(t *testing.T) {
	tests := []struct {
		guard Guard
		want  string
	}{
		{Guard{Contexts: []string{"a", "b"}}, "a, b"},
		{Guard{Prefix: "acme-"}, "acme-*"},
		{Guard{Suffix: "-live"}, "*-live"},
		{Guard{Match: "^x$"}, "^x$"},
	}
	for _, tt := range tests {
		if got := tt.guard.Describe(); got != tt.want {
			t.Errorf("Describe() = %q, want %q", got, tt.want)
		}
	}
}
