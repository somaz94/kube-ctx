// Package namespaces lists the namespaces of a cluster, with an on-disk cache
// so tab-completion and the picker stay instant on a slow or unreachable API
// server.
//
// kubens can only offer namespaces the API server hands back right now; a VPN
// hiccup leaves it with nothing to show. Here a stale cache is preferred over
// an empty list, and the caller is told which one it got.
package namespaces

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/somaz94/kube-ctx/pkg/paths"
)

const (
	// DefaultTTL is how long a cached namespace list is considered fresh.
	DefaultTTL = 10 * time.Minute
	// cacheSubdir is the CacheDir subdirectory holding per-context lists.
	cacheSubdir = "namespaces"
	// filePerm keeps cache files owner-only, matching the rest of kube-ctx.
	filePerm = 0o600
)

// Source describes where a namespace list came from.
type Source int

const (
	// SourceLive means the API server answered.
	SourceLive Source = iota
	// SourceCacheFresh means the cache was within its TTL and no call was made.
	SourceCacheFresh
	// SourceCacheStale means the API server could not be reached and an expired
	// cache was used instead.
	SourceCacheStale
)

// String renders the source for humans.
func (s Source) String() string {
	switch s {
	case SourceLive:
		return "live"
	case SourceCacheFresh:
		return "cache"
	case SourceCacheStale:
		return "stale cache"
	default:
		return "unknown"
	}
}

// ListFunc fetches namespace names from a cluster.
type ListFunc func(ctx context.Context) ([]string, error)

// Result is a namespace list plus where it came from.
type Result struct {
	Namespaces []string
	Source     Source
	// Err is the live-call failure that forced a fall back to a stale cache.
	Err error
}

// Options tunes Fetch.
type Options struct {
	// TTL overrides DefaultTTL.
	TTL time.Duration
	// Refresh skips the fresh-cache shortcut and always calls the API server.
	Refresh bool
	// CacheDir overrides the resolved cache directory. Mainly for tests.
	CacheDir string
}

// Fetch returns the namespaces of ctxName, using the cache when it is fresh and
// falling back to a stale cache when the live call fails.
func Fetch(ctx context.Context, ctxName string, live ListFunc, opts Options) Result {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}

	cached, age, cacheErr := readCache(opts.CacheDir, ctxName)
	if cacheErr == nil && !opts.Refresh && age < ttl {
		return Result{Namespaces: cached, Source: SourceCacheFresh}
	}

	names, err := live(ctx)
	if err != nil {
		if cacheErr == nil {
			return Result{Namespaces: cached, Source: SourceCacheStale, Err: err}
		}
		return Result{Source: SourceLive, Err: err}
	}

	sort.Strings(names)
	// A cache write failure must not fail the command: the data is already in
	// hand and the cache is only an optimization.
	_ = writeCache(opts.CacheDir, ctxName, names)
	return Result{Namespaces: names, Source: SourceLive}
}

// Live returns a ListFunc backed by a real API server connection.
func Live(rc *rest.Config) ListFunc {
	return func(ctx context.Context) ([]string, error) {
		client, err := kubernetes.NewForConfig(rc)
		if err != nil {
			return nil, fmt.Errorf("build client: %w", err)
		}
		list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
		names := make([]string, 0, len(list.Items))
		for _, ns := range list.Items {
			names = append(names, ns.Name)
		}
		return names, nil
	}
}

// cacheEntry is the on-disk cache format.
type cacheEntry struct {
	Fetched    time.Time `json:"fetched"`
	Namespaces []string  `json:"namespaces"`
}

// cachePath returns the cache file for one context.
func cachePath(dir, ctxName string) (string, error) {
	if dir == "" {
		base, err := paths.CacheDir()
		if err != nil {
			return "", err
		}
		dir = base
	}
	return filepath.Join(dir, cacheSubdir, paths.SanitizeName(ctxName)+".json"), nil
}

// readCache returns the cached namespaces and how old they are.
func readCache(dir, ctxName string) ([]string, time.Duration, error) {
	path, err := cachePath(dir, ctxName)
	if err != nil {
		return nil, 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, 0, fmt.Errorf("parse namespace cache: %w", err)
	}
	return entry.Namespaces, time.Since(entry.Fetched), nil
}

// writeCache stores the namespace list for one context.
func writeCache(dir, ctxName string, names []string) error {
	path, err := cachePath(dir, ctxName)
	if err != nil {
		return err
	}
	if err := paths.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	data, err := json.Marshal(cacheEntry{Fetched: time.Now(), Namespaces: names})
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, filePerm)
}
