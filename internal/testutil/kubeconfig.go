// Package testutil builds throwaway kubeconfig fixtures for tests.
//
// Tests point $KUBECONFIG at files produced here, so a test run never reads or
// writes the developer's real ~/.kube/config.
package testutil

import (
	"testing"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Ctx describes one context plus the cluster and user it points at.
type Ctx struct {
	Name      string
	Cluster   string
	User      string
	Namespace string
	Server    string
}

// Spec describes a whole kubeconfig fixture.
type Spec struct {
	Current  string
	Contexts []Ctx
}

// Config builds an in-memory kubeconfig from spec. Cluster and user names
// default to "<context>-cluster" and "<context>-user" so callers only spell out
// the fields a given test actually cares about.
func Config(spec Spec) *clientcmdapi.Config {
	cfg := clientcmdapi.NewConfig()
	cfg.CurrentContext = spec.Current

	for _, c := range spec.Contexts {
		cluster := c.Cluster
		if cluster == "" {
			cluster = c.Name + "-cluster"
		}
		user := c.User
		if user == "" {
			user = c.Name + "-user"
		}
		server := c.Server
		if server == "" {
			server = "https://" + c.Name + ".example.com:6443"
		}

		cfg.Clusters[cluster] = &clientcmdapi.Cluster{Server: server}
		cfg.AuthInfos[user] = &clientcmdapi.AuthInfo{Token: "fixture-token"}
		cfg.Contexts[c.Name] = &clientcmdapi.Context{
			Cluster:   cluster,
			AuthInfo:  user,
			Namespace: c.Namespace,
		}
	}
	return cfg
}

// Write serializes cfg to path, failing the test on error.
func Write(t testing.TB, path string, cfg *clientcmdapi.Config) {
	t.Helper()
	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		t.Fatalf("write kubeconfig %s: %v", path, err)
	}
}

// Read loads a kubeconfig from path, failing the test on error.
func Read(t testing.TB, path string) *clientcmdapi.Config {
	t.Helper()
	cfg, err := clientcmd.LoadFromFile(path)
	if err != nil {
		t.Fatalf("read kubeconfig %s: %v", path, err)
	}
	return cfg
}
