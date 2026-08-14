package kubeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/somaz94/kube-ctx/internal/testutil"
)

// fixture writes n kubeconfig files into a temp dir and points $KUBECONFIG at
// them in precedence order. It returns the file paths.
func fixture(t *testing.T, specs ...testutil.Spec) []string {
	t.Helper()
	dir := t.TempDir()

	paths := make([]string, 0, len(specs))
	for i, spec := range specs {
		p := filepath.Join(dir, "config-"+string(rune('a'+i)))
		testutil.Write(t, p, testutil.Config(spec))
		paths = append(paths, p)
	}
	t.Setenv("KUBECONFIG", strings.Join(paths, string(os.PathListSeparator)))

	// Redirect backups away from the developer's real home directory.
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	return paths
}

func TestLoadMergesEveryFile(t *testing.T) {
	fixture(t,
		testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}},
		testutil.Spec{Contexts: []testutil.Ctx{{Name: "prod"}}},
	)

	cfg, err := New("").Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Contexts) != 2 {
		t.Fatalf("got %d contexts, want 2", len(cfg.Contexts))
	}
	// The first file in the list wins for scalar fields such as current-context.
	if cfg.CurrentContext != "dev" {
		t.Errorf("CurrentContext = %q, want dev", cfg.CurrentContext)
	}
}

func TestSaveWritesBackToOriginatingFile(t *testing.T) {
	files := fixture(t,
		testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}},
		testutil.Spec{Contexts: []testutil.Ctx{{Name: "prod"}}},
	)

	l := New("")
	cfg, err := l.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Change a field that lives in the *second* file. clientcmd must route the
	// write there rather than collapsing everything into the first file.
	cfg.Contexts["prod"].Namespace = "monitoring"
	if err := l.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	second := testutil.Read(t, files[1])
	if got := second.Contexts["prod"].Namespace; got != "monitoring" {
		t.Errorf("second file namespace = %q, want monitoring", got)
	}
	first := testutil.Read(t, files[0])
	if _, leaked := first.Contexts["prod"]; leaked {
		t.Error("prod context leaked into the first kubeconfig file")
	}
}

func TestSaveCurrentContext(t *testing.T) {
	files := fixture(t, testutil.Spec{
		Current:  "dev",
		Contexts: []testutil.Ctx{{Name: "dev"}, {Name: "prod"}},
	})

	l := New("")
	cfg, err := l.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.CurrentContext = "prod"
	if err := l.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := testutil.Read(t, files[0]).CurrentContext; got != "prod" {
		t.Errorf("CurrentContext = %q, want prod", got)
	}
}

func TestExplicitPathOverridesEnv(t *testing.T) {
	files := fixture(t, testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})

	other := filepath.Join(t.TempDir(), "explicit")
	testutil.Write(t, other, testutil.Config(testutil.Spec{
		Current: "explicit", Contexts: []testutil.Ctx{{Name: "explicit"}},
	}))

	l := New(other)
	cfg, err := l.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.CurrentContext != "explicit" {
		t.Errorf("CurrentContext = %q, want explicit", cfg.CurrentContext)
	}
	if got := l.Precedence(); len(got) != 1 || got[0] != other {
		t.Errorf("Precedence = %v, want [%s]", got, other)
	}
	if _, err := os.Stat(files[0]); err != nil {
		t.Errorf("env kubeconfig should be untouched: %v", err)
	}
}

func TestBackupCopiesEveryFileAndRotates(t *testing.T) {
	files := fixture(t,
		testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}},
		testutil.Spec{Contexts: []testutil.Ctx{{Name: "prod"}}},
	)

	l := New("")
	l.backupKeep = 2

	for i := 0; i < 4; i++ {
		if err := l.Backup(); err != nil {
			t.Fatalf("Backup %d: %v", i, err)
		}
	}

	root := filepath.Join(os.Getenv("XDG_STATE_HOME"), "kube-ctx", backupDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read backup root: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("kept %d backup generations, want 2", len(entries))
	}

	newest := filepath.Join(root, entries[len(entries)-1].Name())
	got, err := os.ReadDir(newest)
	if err != nil {
		t.Fatalf("read backup generation: %v", err)
	}
	if len(got) != len(files) {
		t.Fatalf("backed up %d files, want %d", len(got), len(files))
	}
	for _, e := range got {
		info, err := e.Info()
		if err != nil {
			t.Fatalf("stat backup entry: %v", err)
		}
		if perm := info.Mode().Perm(); perm != filePerm {
			t.Errorf("backup %s perm = %o, want %o", e.Name(), perm, filePerm)
		}
	}
}

func TestBackupSkipsMissingFiles(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	t.Setenv("KUBECONFIG", missing)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))

	if err := New("").Backup(); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	root := filepath.Join(dir, "state", "kube-ctx", backupDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read backup root: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("created %d empty backup generations, want 0", len(entries))
	}
}

func TestSaveWithBackup(t *testing.T) {
	fixture(t, testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})

	l := New("")
	cfg, err := l.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	delete(cfg.Contexts, "dev")
	if err := l.Save(cfg, WithBackup()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	root := filepath.Join(os.Getenv("XDG_STATE_HOME"), "kube-ctx", backupDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read backup root: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("got %d backup generations, want 1", len(entries))
	}
}

func TestRestConfigForNamedContext(t *testing.T) {
	fixture(t, testutil.Spec{
		Current: "dev",
		Contexts: []testutil.Ctx{
			{Name: "dev", Server: "https://dev.example.com:6443"},
			{Name: "prod", Server: "https://prod.example.com:6443"},
		},
	})

	l := New("")

	rc, err := l.RestConfig("prod")
	if err != nil {
		t.Fatalf("RestConfig(prod): %v", err)
	}
	if rc.Host != "https://prod.example.com:6443" {
		t.Errorf("Host = %q, want the prod server", rc.Host)
	}

	rc, err = l.RestConfig("")
	if err != nil {
		t.Fatalf("RestConfig(current): %v", err)
	}
	if rc.Host != "https://dev.example.com:6443" {
		t.Errorf("Host = %q, want the dev server", rc.Host)
	}

	if _, err := l.RestConfig("nope"); err == nil {
		t.Error("expected an error for an unknown context")
	}
}

func TestCopyFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := copyFile(filepath.Join(dir, "missing"), filepath.Join(dir, "dst")); err == nil {
		t.Error("expected an error copying a missing source")
	}

	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("x"), filePerm); err != nil {
		t.Fatalf("write src: %v", err)
	}
	if err := copyFile(src, filepath.Join(dir, "no-such-dir", "dst")); err == nil {
		t.Error("expected an error copying into a missing directory")
	}
}

func TestRotateBackupsMissingRoot(t *testing.T) {
	if err := rotateBackups(filepath.Join(t.TempDir(), "absent"), 1); err == nil {
		t.Error("expected an error for a missing backup root")
	}
}

func TestPathOptionsIsExposed(t *testing.T) {
	fixture(t, testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})

	l := New("")
	if l.PathOptions() == nil {
		t.Fatal("PathOptions returned nil")
	}
	if l.PathOptions().LoadingRules == nil {
		t.Error("PathOptions has no loading rules")
	}
}

func TestLoadRejectsMalformedKubeconfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("clusters: [unclosed\n"), filePerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("KUBECONFIG", path)

	if _, err := New("").Load(); err == nil {
		t.Error("expected a parse error")
	}
}

func TestSaveIntoUnwritableLocation(t *testing.T) {
	files := fixture(t, testutil.Spec{
		Current:  "dev",
		Contexts: []testutil.Ctx{{Name: "dev"}, {Name: "prod"}},
	})

	l := New("")
	cfg, err := l.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// clientcmd writes through a temporary file in the same directory, so a
	// read-only directory is what actually blocks the write.
	dir := filepath.Dir(files[0])
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	cfg.CurrentContext = "prod"
	if err := l.Save(cfg); err == nil {
		t.Error("expected a write error")
	}
}

func TestBackupWithUnwritableStateDir(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "kube-ctx")
	if err := os.WriteFile(blocker, []byte("x"), filePerm); err != nil {
		t.Fatalf("write: %v", err)
	}
	fixture(t, testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})
	t.Setenv("XDG_STATE_HOME", dir)

	if err := New("").Backup(); err == nil {
		t.Error("expected an error when the backup directory cannot be created")
	}
}

func TestReadFileIgnoresTheEnvironment(t *testing.T) {
	files := fixture(t,
		testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}},
		testutil.Spec{Contexts: []testutil.Ctx{{Name: "prod"}}},
	)

	// Load() would merge both files; ReadFile has to see only the one it was
	// given, or an import source would arrive already mixed with the config it
	// is about to be merged into.
	cfg, err := ReadFile(files[1])
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(cfg.Contexts) != 1 {
		t.Errorf("contexts = %v, want only the ones in that file", cfg.Contexts)
	}
	if _, err := ReadFile(filepath.Join(filepath.Dir(files[0]), "absent")); err == nil {
		t.Error("reading a file that is not there must fail")
	}
}

func TestEncodeRoundTrips(t *testing.T) {
	fixture(t, testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})
	cfg, err := New("").Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	yamlData, err := Encode(cfg)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(string(yamlData), "current-context: dev") {
		t.Errorf("YAML = %s", yamlData)
	}

	jsonData, err := EncodeJSON(cfg)
	if err != nil {
		t.Fatalf("EncodeJSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(jsonData, &doc); err != nil {
		t.Fatalf("EncodeJSON produced invalid JSON: %v\n%s", err, jsonData)
	}
	if doc["current-context"] != "dev" {
		t.Errorf("current-context = %v", doc["current-context"])
	}
}

func TestFlattenInlinesReferencedFiles(t *testing.T) {
	files := fixture(t, testutil.Spec{Current: "dev", Contexts: []testutil.Ctx{{Name: "dev"}}})
	ca := filepath.Join(filepath.Dir(files[0]), "ca.crt")
	if err := os.WriteFile(ca, []byte("cert"), filePerm); err != nil {
		t.Fatalf("write ca: %v", err)
	}

	l := New("")
	cfg, err := l.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.Clusters["dev-cluster"].CertificateAuthority = "ca.crt"

	if err := Flatten(cfg); err != nil {
		t.Fatalf("Flatten: %v", err)
	}
	if got := string(cfg.Clusters["dev-cluster"].CertificateAuthorityData); got != "cert" {
		t.Errorf("CertificateAuthorityData = %q, want the file contents", got)
	}

	// A path that resolves to nothing has to fail loudly: the point of
	// flattening is a file that works somewhere else.
	cfg.Clusters["dev-cluster"].CertificateAuthorityData = nil
	cfg.Clusters["dev-cluster"].CertificateAuthority = "absent.crt"
	if err := Flatten(cfg); err == nil {
		t.Error("flattening a missing certificate must fail")
	}
}

func TestWriteFileRefusesToClobber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exported.yaml")

	if err := WriteFile(path, []byte("first"), false); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != filePerm {
		t.Errorf("mode = %v, want %v", perm, os.FileMode(filePerm))
	}

	err = WriteFile(path, []byte("second"), false)
	if err == nil {
		t.Fatal("an existing file must not be replaced")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error = %v, want it to name the flag that allows it", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "first" {
		t.Errorf("contents = %q, want the original", data)
	}

	if err := WriteFile(path, []byte("second"), true); err != nil {
		t.Fatalf("WriteFile with overwrite: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "second" {
		t.Errorf("contents = %q, want the replacement", data)
	}

	// A path whose directory does not exist is the other way this fails.
	if err := WriteFile(filepath.Join(path, "nested"), []byte("x"), false); err == nil {
		t.Error("writing under a non-directory must fail")
	}
}
