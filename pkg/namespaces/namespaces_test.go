package namespaces

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func staticList(names ...string) ListFunc {
	return func(context.Context) ([]string, error) { return names, nil }
}

func failingList(err error) ListFunc {
	return func(context.Context) ([]string, error) { return nil, err }
}

func TestFetchLiveWritesCache(t *testing.T) {
	dir := t.TempDir()

	got := Fetch(context.Background(), "dev", staticList("kube-system", "default"), Options{CacheDir: dir})
	if got.Source != SourceLive {
		t.Errorf("Source = %v, want live", got.Source)
	}
	if want := []string{"default", "kube-system"}; !reflect.DeepEqual(got.Namespaces, want) {
		t.Errorf("Namespaces = %v, want %v (sorted)", got.Namespaces, want)
	}

	// The second call must be served from the fresh cache without calling live.
	got = Fetch(context.Background(), "dev", failingList(errors.New("must not be called")), Options{CacheDir: dir})
	if got.Source != SourceCacheFresh {
		t.Errorf("Source = %v, want cache", got.Source)
	}
	if got.Err != nil {
		t.Errorf("unexpected error: %v", got.Err)
	}
}

func TestFetchRefreshBypassesFreshCache(t *testing.T) {
	dir := t.TempDir()
	Fetch(context.Background(), "dev", staticList("old"), Options{CacheDir: dir})

	got := Fetch(context.Background(), "dev", staticList("new"), Options{CacheDir: dir, Refresh: true})
	if got.Source != SourceLive {
		t.Errorf("Source = %v, want live", got.Source)
	}
	if !reflect.DeepEqual(got.Namespaces, []string{"new"}) {
		t.Errorf("Namespaces = %v, want [new]", got.Namespaces)
	}
}

func TestFetchFallsBackToStaleCache(t *testing.T) {
	dir := t.TempDir()
	Fetch(context.Background(), "dev", staticList("default"), Options{CacheDir: dir})

	// A zero TTL makes the entry written above stale immediately.
	boom := errors.New("dial tcp: i/o timeout")
	got := Fetch(context.Background(), "dev", failingList(boom), Options{CacheDir: dir, TTL: time.Nanosecond})

	if got.Source != SourceCacheStale {
		t.Errorf("Source = %v, want stale cache", got.Source)
	}
	if !reflect.DeepEqual(got.Namespaces, []string{"default"}) {
		t.Errorf("Namespaces = %v, want [default]", got.Namespaces)
	}
	if !errors.Is(got.Err, boom) {
		t.Errorf("Err = %v, want the live failure surfaced", got.Err)
	}
}

func TestFetchLiveFailureWithoutCache(t *testing.T) {
	boom := errors.New("unreachable")
	got := Fetch(context.Background(), "dev", failingList(boom), Options{CacheDir: t.TempDir()})

	if got.Namespaces != nil {
		t.Errorf("Namespaces = %v, want nil", got.Namespaces)
	}
	if !errors.Is(got.Err, boom) {
		t.Errorf("Err = %v, want %v", got.Err, boom)
	}
}

func TestFetchIgnoresCorruptCache(t *testing.T) {
	dir := t.TempDir()
	path, err := cachePath(dir, "dev")
	if err != nil {
		t.Fatalf("cachePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), filePerm); err != nil {
		t.Fatalf("write corrupt cache: %v", err)
	}

	got := Fetch(context.Background(), "dev", staticList("default"), Options{CacheDir: dir})
	if got.Source != SourceLive {
		t.Errorf("Source = %v, want live", got.Source)
	}
}

func TestCachePathEscapesContextName(t *testing.T) {
	dir := t.TempDir()
	// EKS context names are ARNs, full of colons and slashes.
	arn := "arn:aws:eks:ap-northeast-2:123456789012:cluster/prod"

	path, err := cachePath(dir, arn)
	if err != nil {
		t.Fatalf("cachePath: %v", err)
	}
	if got, want := filepath.Dir(path), filepath.Join(dir, cacheSubdir); got != want {
		t.Errorf("cache file escaped its directory: %q", path)
	}

	// A round trip through the cache proves the escaped name is writable.
	if err := writeCache(dir, arn, []string{"default"}); err != nil {
		t.Fatalf("writeCache: %v", err)
	}
	names, _, err := readCache(dir, arn)
	if err != nil {
		t.Fatalf("readCache: %v", err)
	}
	if !reflect.DeepEqual(names, []string{"default"}) {
		t.Errorf("round trip = %v", names)
	}
}

func TestCachePathUsesXDGWhenUnset(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", base)

	path, err := cachePath("", "dev")
	if err != nil {
		t.Fatalf("cachePath: %v", err)
	}
	if want := filepath.Join(base, "kube-ctx", cacheSubdir, "dev.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

func TestSourceString(t *testing.T) {
	tests := map[Source]string{
		SourceLive:       "live",
		SourceCacheFresh: "cache",
		SourceCacheStale: "stale cache",
		Source(99):       "unknown",
	}
	for src, want := range tests {
		if got := src.String(); got != want {
			t.Errorf("Source(%d).String() = %q, want %q", src, got, want)
		}
	}
}

func TestLiveAgainstFakeAPIServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"kind":       "NamespaceList",
			"apiVersion": "v1",
			"items": []map[string]any{
				{"metadata": map[string]any{"name": "default"}},
				{"metadata": map[string]any{"name": "kube-system"}},
			},
		})
	}))
	defer srv.Close()

	got, err := Live(&rest.Config{Host: srv.URL})(context.Background())
	if err != nil {
		t.Fatalf("Live: %v", err)
	}
	if want := []string{"default", "kube-system"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Live = %v, want %v", got, want)
	}
}

func TestLiveSurfacesAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	if _, err := Live(&rest.Config{Host: srv.URL})(context.Background()); err == nil {
		t.Fatal("expected an error")
	}

	// An unusable rest.Config must fail at client construction.
	bad := &rest.Config{
		Host:         srv.URL,
		ExecProvider: &clientcmdapi.ExecConfig{},
		AuthProvider: &clientcmdapi.AuthProviderConfig{},
	}
	if _, err := Live(bad)(context.Background()); err == nil {
		t.Error("expected an error building a client from a conflicting config")
	}
}
