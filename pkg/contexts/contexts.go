// Package contexts implements the read and edit operations kube-ctx performs
// on the contexts of a merged kubeconfig.
//
// Every function here takes an already-loaded *api.Config and mutates it in
// memory only. Persisting is the caller's job (pkg/kubeconfig), which keeps
// these operations trivially testable and keeps the "when do we write" decision
// in one place.
package contexts

import (
	"fmt"
	"sort"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Context is a flattened view of one kubeconfig context, resolved enough to
// print without further lookups.
// The tags matter: without them "kctx list -o json" emits Go-style keys while
// "kctx doctor -o json" emits lowerCamel, so one binary answers to two
// conventions and "kctx list -o json | jq .[].name" quietly returns null.
type Context struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	User      string `json:"user"`
	Namespace string `json:"namespace"`
	Server    string `json:"server"`
	Current   bool   `json:"current"`
}

// List returns every context sorted by name.
func List(cfg *clientcmdapi.Config) []Context {
	out := make([]Context, 0, len(cfg.Contexts))
	for name, c := range cfg.Contexts {
		ctx := Context{
			Name:      name,
			Cluster:   c.Cluster,
			User:      c.AuthInfo,
			Namespace: c.Namespace,
			Current:   name == cfg.CurrentContext,
		}
		if cluster, ok := cfg.Clusters[c.Cluster]; ok {
			ctx.Server = cluster.Server
		}
		if ctx.Namespace == "" {
			ctx.Namespace = "default"
		}
		out = append(out, ctx)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names returns every context name, sorted.
func Names(cfg *clientcmdapi.Config) []string {
	names := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Exists reports whether name is a known context.
func Exists(cfg *clientcmdapi.Config, name string) bool {
	_, ok := cfg.Contexts[name]
	return ok
}

// Switch points current-context at name and returns the context that was
// current before. Switching to the already-current context is a no-op that
// still succeeds, so callers can be careless about it.
func Switch(cfg *clientcmdapi.Config, name string) (previous string, err error) {
	if !Exists(cfg, name) {
		return "", fmt.Errorf("no context named %q", name)
	}
	previous = cfg.CurrentContext
	cfg.CurrentContext = name
	return previous, nil
}

// SetNamespace sets the default namespace of one context. An empty ctxName
// means the current context.
func SetNamespace(cfg *clientcmdapi.Config, ctxName, namespace string) error {
	if ctxName == "" {
		ctxName = cfg.CurrentContext
	}
	if ctxName == "" {
		return fmt.Errorf("no current context is set")
	}
	c, ok := cfg.Contexts[ctxName]
	if !ok {
		return fmt.Errorf("no context named %q", ctxName)
	}
	c.Namespace = namespace
	return nil
}

// Namespace returns the default namespace of one context, or "default" when the
// context does not pin one. An empty ctxName means the current context.
func Namespace(cfg *clientcmdapi.Config, ctxName string) (string, error) {
	if ctxName == "" {
		ctxName = cfg.CurrentContext
	}
	c, ok := cfg.Contexts[ctxName]
	if !ok {
		return "", fmt.Errorf("no context named %q", ctxName)
	}
	if c.Namespace == "" {
		return "default", nil
	}
	return c.Namespace, nil
}

// Rename moves a context to a new name, carrying current-context along when the
// renamed context is the active one.
func Rename(cfg *clientcmdapi.Config, oldName, newName string) error {
	if newName == "" {
		return fmt.Errorf("the new context name must not be empty")
	}
	if oldName == newName {
		return fmt.Errorf("context %q already has that name", oldName)
	}
	c, ok := cfg.Contexts[oldName]
	if !ok {
		return fmt.Errorf("no context named %q", oldName)
	}
	if _, taken := cfg.Contexts[newName]; taken {
		return fmt.Errorf("context %q already exists", newName)
	}

	cfg.Contexts[newName] = c
	delete(cfg.Contexts, oldName)
	if cfg.CurrentContext == oldName {
		cfg.CurrentContext = newName
	}
	return nil
}

// Orphans lists cluster and user entries that no remaining context refers to.
type Orphans struct {
	Clusters []string
	Users    []string
}

// Empty reports whether nothing was orphaned.
func (o Orphans) Empty() bool { return len(o.Clusters) == 0 && len(o.Users) == 0 }

// Delete removes contexts and reports which cluster and user stanzas are left
// unreferenced. The stanzas are *not* removed — deleting shared credentials as
// a side effect of dropping one context is exactly the kind of surprise this
// tool exists to avoid — so the caller decides what to do with the list.
//
// Deleting the current context clears current-context rather than silently
// promoting another one.
func Delete(cfg *clientcmdapi.Config, names ...string) (Orphans, error) {
	for _, name := range names {
		if !Exists(cfg, name) {
			return Orphans{}, fmt.Errorf("no context named %q", name)
		}
	}
	for _, name := range names {
		delete(cfg.Contexts, name)
		if cfg.CurrentContext == name {
			cfg.CurrentContext = ""
		}
	}
	return findOrphans(cfg), nil
}

// findOrphans returns clusters and users that no context references.
func findOrphans(cfg *clientcmdapi.Config) Orphans {
	usedClusters := make(map[string]bool, len(cfg.Contexts))
	usedUsers := make(map[string]bool, len(cfg.Contexts))
	for _, c := range cfg.Contexts {
		usedClusters[c.Cluster] = true
		usedUsers[c.AuthInfo] = true
	}

	var o Orphans
	for name := range cfg.Clusters {
		if !usedClusters[name] {
			o.Clusters = append(o.Clusters, name)
		}
	}
	for name := range cfg.AuthInfos {
		if !usedUsers[name] {
			o.Users = append(o.Users, name)
		}
	}
	sort.Strings(o.Clusters)
	sort.Strings(o.Users)
	return o
}

// PruneOrphans removes the given cluster and user stanzas from the config.
func PruneOrphans(cfg *clientcmdapi.Config, o Orphans) {
	for _, name := range o.Clusters {
		delete(cfg.Clusters, name)
	}
	for _, name := range o.Users {
		delete(cfg.AuthInfos, name)
	}
}
