// Package transfer moves contexts between kubeconfigs.
//
// Two directions, one set of rules: Merge folds a foreign kubeconfig into the
// user's own set (what "kctx import" does), and Extract lifts a subset of the
// user's set out into a standalone one ("kctx export"). Both work on an
// already-loaded *api.Config and mutate nothing on disk — persisting is
// pkg/kubeconfig's job, the same split the rest of the packages use.
//
// The hard part is not the copying, it is the names. A kubeconfig has three
// independent namespaces (contexts, clusters, users) and the interesting
// sources collide in all of them: every kubeadm cluster calls its cluster
// "kubernetes" and its user "kubernetes-admin", and every kind cluster calls
// its context "kind-<name>". "kubectl config view --flatten" merges those by
// last-writer-wins, which silently repoints an existing context at a different
// API server. Merge never overwrites a stanza whose contents differ; it
// disambiguates the incoming one and repoints only the context it is importing.
package transfer

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Action is what Merge did with one context.
type Action string

const (
	// ActionAdded is a context that did not exist in the destination.
	ActionAdded Action = "added"
	// ActionOverwritten is an existing context replaced under --overwrite.
	ActionOverwritten Action = "overwritten"
	// ActionUnchanged is a context already present with the same contents, so
	// re-importing the same file is a no-op rather than a collision.
	ActionUnchanged Action = "unchanged"
)

// Entry records what happened to one context.
//
// Source is kept separate from Name because --prefix and --as mean the two
// differ, and "which of the names in that file did this come from" is the
// question you have afterwards.
type Entry struct {
	Source  string `json:"source"`
	Name    string `json:"name"`
	Cluster string `json:"cluster"`
	User    string `json:"user"`
	Action  Action `json:"action"`
}

// Renamed reports whether the context landed under a different name.
func (e Entry) Renamed() bool { return e.Source != e.Name }

// Options selects and renames what Merge imports.
type Options struct {
	// Contexts limits the import to these source contexts. Empty means all.
	Contexts []string
	// Prefix is prepended to every imported context name.
	Prefix string
	// Rename is the name to import under, legal only for a single context.
	Rename string
	// Overwrite replaces destination contexts that already exist.
	Overwrite bool
}

// Result is what Merge did, in the order the contexts were imported.
type Result struct {
	Entries []Entry `json:"entries"`
}

// Changed reports whether anything would actually be written.
func (r Result) Changed() bool {
	for _, e := range r.Entries {
		if e.Action != ActionUnchanged {
			return true
		}
	}
	return false
}

// Merge copies the selected contexts of src into dst, along with the cluster
// and user stanzas they reference.
//
// dst is left untouched when anything fails: the work happens on a copy and is
// committed at the end, because half of an imported kubeconfig is harder to
// clean up than none of one.
func Merge(dst, src *clientcmdapi.Config, opts Options) (Result, error) {
	names, err := selectContexts(src, opts.Contexts)
	if err != nil {
		return Result{}, err
	}
	if opts.Rename != "" && len(names) != 1 {
		return Result{}, fmt.Errorf("--as names a single context, but %d were selected; narrow it with --context or use --prefix", len(names))
	}

	work := dst.DeepCopy()
	// A Config built by hand rather than by clientcmd can arrive with nil maps,
	// and DeepCopy preserves that faithfully — the first import would panic on
	// assignment instead of returning an error.
	if work.Clusters == nil {
		work.Clusters = map[string]*clientcmdapi.Cluster{}
	}
	if work.AuthInfos == nil {
		work.AuthInfos = map[string]*clientcmdapi.AuthInfo{}
	}
	if work.Contexts == nil {
		work.Contexts = map[string]*clientcmdapi.Context{}
	}

	entries := make([]Entry, 0, len(names))
	var collisions []string

	for _, name := range names {
		target := opts.Prefix + name
		if opts.Rename != "" {
			target = opts.Rename
		}

		_, exists := work.Contexts[target]
		switch {
		case exists && sameContext(work, src, target, name):
			// Importing the same file twice should be boring, not an error.
			existing := work.Contexts[target]
			entries = append(entries, Entry{
				Source: name, Name: target, Cluster: existing.Cluster,
				User: existing.AuthInfo, Action: ActionUnchanged,
			})
			continue
		case exists && !opts.Overwrite:
			// Collected rather than returned: reported one at a time, the user
			// re-runs the import once per context in the file.
			collisions = append(collisions, target)
			continue
		}

		cluster, err := adoptCluster(work, src, src.Contexts[name].Cluster)
		if err != nil {
			return Result{}, err
		}
		user, err := adoptUser(work, src, src.Contexts[name].AuthInfo)
		if err != nil {
			return Result{}, err
		}

		imported := src.Contexts[name].DeepCopy()
		// Cleared so clientcmd writes the new stanza into the user's own
		// kubeconfig. Left as-is it names the file this came from, and
		// ModifyConfig would faithfully write the import back into the source.
		imported.LocationOfOrigin = ""
		imported.Cluster = cluster
		imported.AuthInfo = user
		work.Contexts[target] = imported

		action := ActionAdded
		if exists {
			action = ActionOverwritten
		}
		entries = append(entries, Entry{
			Source: name, Name: target, Cluster: cluster, User: user, Action: action,
		})
	}

	if len(collisions) > 0 {
		return Result{}, fmt.Errorf("context %s already exists; import under another name with --prefix or --as, or replace it with --overwrite",
			strings.Join(quoteAll(collisions), ", "))
	}

	// Only the three namespaces Merge touches are committed; current-context and
	// preferences belong to the destination and are none of an import's business.
	dst.Clusters, dst.AuthInfos, dst.Contexts = work.Clusters, work.AuthInfos, work.Contexts
	return Result{Entries: entries}, nil
}

// Extract builds a standalone kubeconfig holding only the named contexts and
// the cluster and user stanzas they reference.
//
// LocationOfOrigin is deliberately carried over, the opposite of what Merge
// does: it is what resolves a relative certificate path, so clearing it here
// would break flattening an exported config.
func Extract(cfg *clientcmdapi.Config, names []string) (*clientcmdapi.Config, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("no contexts to export")
	}

	out := clientcmdapi.NewConfig()
	for _, name := range names {
		c, ok := cfg.Contexts[name]
		if !ok {
			return nil, fmt.Errorf("no context named %q", name)
		}
		out.Contexts[name] = c.DeepCopy()

		if c.Cluster != "" {
			cluster, ok := cfg.Clusters[c.Cluster]
			if !ok {
				return nil, fmt.Errorf("context %q references cluster %q, which the kubeconfig does not define", name, c.Cluster)
			}
			out.Clusters[c.Cluster] = cluster.DeepCopy()
		}
		// An empty user is legal — that is how an anonymous context is spelled.
		if c.AuthInfo != "" {
			user, ok := cfg.AuthInfos[c.AuthInfo]
			if !ok {
				return nil, fmt.Errorf("context %q references user %q, which the kubeconfig does not define", name, c.AuthInfo)
			}
			out.AuthInfos[c.AuthInfo] = user.DeepCopy()
		}
	}

	// A kubeconfig with no current-context is one kubectl refuses to use, and
	// the point of an export is handing someone something that works. The
	// original current-context wins when it survived the extraction.
	out.CurrentContext = names[0]
	if _, ok := out.Contexts[cfg.CurrentContext]; ok {
		out.CurrentContext = cfg.CurrentContext
	}
	return out, nil
}

// selectContexts resolves the requested subset against src, preserving the
// order the user asked for. An empty request means every context, sorted, so
// that a multi-context import assigns disambiguating suffixes deterministically.
func selectContexts(src *clientcmdapi.Config, want []string) ([]string, error) {
	if len(src.Contexts) == 0 {
		return nil, fmt.Errorf("the source kubeconfig has no contexts")
	}
	if len(want) == 0 {
		names := make([]string, 0, len(src.Contexts))
		for name := range src.Contexts {
			names = append(names, name)
		}
		sort.Strings(names)
		return names, nil
	}

	seen := make(map[string]bool, len(want))
	out := make([]string, 0, len(want))
	for _, name := range want {
		if _, ok := src.Contexts[name]; !ok {
			return nil, fmt.Errorf("no context named %q in the source kubeconfig", name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out, nil
}

// adoptCluster makes src's cluster available in work under a name the imported
// context can point at, and returns that name.
func adoptCluster(work, src *clientcmdapi.Config, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	incoming, ok := src.Clusters[name]
	if !ok {
		return "", fmt.Errorf("the source kubeconfig references cluster %q, which it does not define", name)
	}
	if existing, taken := work.Clusters[name]; taken {
		if equalCluster(existing, incoming) {
			return name, nil
		}
		// A different cluster already owns the name — two kubeadm clusters both
		// called "kubernetes" is the common case. Replacing it would repoint
		// every context that referenced it at a different API server, so the
		// incoming one gets a name of its own instead.
		//
		// Unless this exact cluster was already adopted under a suffixed name:
		// contexts in one source file nearly always share a cluster, and minting
		// a suffix per context leaves a pile of identical stanzas behind.
		if adopted, ok := findEqualCluster(work, incoming); ok {
			return adopted, nil
		}
		name = freeName(name, func(candidate string) bool {
			_, taken := work.Clusters[candidate]
			return taken
		})
	}
	copied := incoming.DeepCopy()
	copied.LocationOfOrigin = ""
	work.Clusters[name] = copied
	return name, nil
}

// adoptUser is adoptCluster for the user namespace, which collides just as
// often: every kubeadm kubeconfig calls its user "kubernetes-admin".
func adoptUser(work, src *clientcmdapi.Config, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	incoming, ok := src.AuthInfos[name]
	if !ok {
		return "", fmt.Errorf("the source kubeconfig references user %q, which it does not define", name)
	}
	if existing, taken := work.AuthInfos[name]; taken {
		if equalUser(existing, incoming) {
			return name, nil
		}
		if adopted, ok := findEqualUser(work, incoming); ok {
			return adopted, nil
		}
		name = freeName(name, func(candidate string) bool {
			_, taken := work.AuthInfos[candidate]
			return taken
		})
	}
	copied := incoming.DeepCopy()
	copied.LocationOfOrigin = ""
	work.AuthInfos[name] = copied
	return name, nil
}

// findEqualCluster returns the name an identical cluster is already stored
// under. Names are walked in sorted order so that a config holding two equal
// copies still resolves to the same one on every run.
func findEqualCluster(cfg *clientcmdapi.Config, want *clientcmdapi.Cluster) (string, bool) {
	names := make([]string, 0, len(cfg.Clusters))
	for name := range cfg.Clusters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if equalCluster(cfg.Clusters[name], want) {
			return name, true
		}
	}
	return "", false
}

// findEqualUser is findEqualCluster for the user namespace.
func findEqualUser(cfg *clientcmdapi.Config, want *clientcmdapi.AuthInfo) (string, bool) {
	names := make([]string, 0, len(cfg.AuthInfos))
	for name := range cfg.AuthInfos {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if equalUser(cfg.AuthInfos[name], want) {
			return name, true
		}
	}
	return "", false
}

// sameContext reports whether target already is the source context, down to the
// cluster and the user it resolves to.
//
// The referenced stanza *names* are deliberately not compared: an earlier
// import may have landed the same cluster as "kubernetes-2", and a context
// pointing at it is still the same context. Comparing names would turn every
// second import of a file into a collision.
func sameContext(work, src *clientcmdapi.Config, target, name string) bool {
	dstCtx, ok := work.Contexts[target]
	if !ok {
		return false
	}
	srcCtx := src.Contexts[name]
	if dstCtx.Namespace != srcCtx.Namespace {
		return false
	}
	if !equalCluster(work.Clusters[dstCtx.Cluster], src.Clusters[srcCtx.Cluster]) {
		return false
	}
	return equalUser(work.AuthInfos[dstCtx.AuthInfo], src.AuthInfos[srcCtx.AuthInfo])
}

// equalCluster compares two cluster stanzas by contents.
//
// LocationOfOrigin is zeroed first: clientcmd stamps every stanza with the file
// it was read from, so a raw DeepEqual between an imported stanza and the user's
// own is always false — which would make every stanza look like a conflict and
// leave a trail of "-2" duplicates behind each import.
func equalCluster(a, b *clientcmdapi.Cluster) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	x, y := a.DeepCopy(), b.DeepCopy()
	x.LocationOfOrigin, y.LocationOfOrigin = "", ""
	return reflect.DeepEqual(x, y)
}

// equalUser is equalCluster for the user namespace.
func equalUser(a, b *clientcmdapi.AuthInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	x, y := a.DeepCopy(), b.DeepCopy()
	x.LocationOfOrigin, y.LocationOfOrigin = "", ""
	return reflect.DeepEqual(x, y)
}

// freeName returns the first "<base>-N" that taken rejects, starting at 2.
// Numbering from 2 reads as a second copy of the thing; "-1" reads as the first.
func freeName(base string, taken func(string) bool) string {
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken(candidate) {
			return candidate
		}
	}
}

// quoteAll quotes each name for an error message listing several of them.
func quoteAll(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("%q", name))
	}
	return out
}
