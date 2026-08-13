package contexts

import (
	"reflect"
	"testing"

	"github.com/somaz94/kube-ctx/internal/testutil"
)

func fixture() *testutil.Spec {
	return &testutil.Spec{
		Current: "dev",
		Contexts: []testutil.Ctx{
			{Name: "prod", Namespace: "monitoring"},
			{Name: "dev"},
			{Name: "staging", Cluster: "shared", User: "shared-user"},
		},
	}
}

func TestList(t *testing.T) {
	got := List(testutil.Config(*fixture()))

	if len(got) != 3 {
		t.Fatalf("got %d contexts, want 3", len(got))
	}
	if got[0].Name != "dev" || got[1].Name != "prod" || got[2].Name != "staging" {
		t.Errorf("not sorted by name: %v", []string{got[0].Name, got[1].Name, got[2].Name})
	}
	if !got[0].Current {
		t.Error("dev should be marked current")
	}
	if got[1].Current {
		t.Error("prod should not be marked current")
	}
	if got[0].Namespace != "default" {
		t.Errorf("unset namespace = %q, want default", got[0].Namespace)
	}
	if got[1].Namespace != "monitoring" {
		t.Errorf("prod namespace = %q, want monitoring", got[1].Namespace)
	}
	if got[0].Server != "https://dev.example.com:6443" {
		t.Errorf("server = %q", got[0].Server)
	}
	if got[2].Cluster != "shared" || got[2].User != "shared-user" {
		t.Errorf("staging cluster/user = %q/%q", got[2].Cluster, got[2].User)
	}
}

func TestListServerMissingCluster(t *testing.T) {
	cfg := testutil.Config(*fixture())
	delete(cfg.Clusters, "dev-cluster")

	for _, c := range List(cfg) {
		if c.Name == "dev" && c.Server != "" {
			t.Errorf("server = %q, want empty for a dangling cluster ref", c.Server)
		}
	}
}

func TestNamesAndExists(t *testing.T) {
	cfg := testutil.Config(*fixture())

	if got, want := Names(cfg), []string{"dev", "prod", "staging"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Names = %v, want %v", got, want)
	}
	if !Exists(cfg, "prod") {
		t.Error("Exists(prod) = false")
	}
	if Exists(cfg, "nope") {
		t.Error("Exists(nope) = true")
	}
}

func TestSwitch(t *testing.T) {
	cfg := testutil.Config(*fixture())

	prev, err := Switch(cfg, "prod")
	if err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if prev != "dev" {
		t.Errorf("previous = %q, want dev", prev)
	}
	if cfg.CurrentContext != "prod" {
		t.Errorf("current = %q, want prod", cfg.CurrentContext)
	}

	// Switching to the current context stays a success.
	if _, err := Switch(cfg, "prod"); err != nil {
		t.Errorf("re-switch: %v", err)
	}
	if _, err := Switch(cfg, "nope"); err == nil {
		t.Error("expected an error for an unknown context")
	}
}

func TestSetNamespace(t *testing.T) {
	cfg := testutil.Config(*fixture())

	if err := SetNamespace(cfg, "", "kube-system"); err != nil {
		t.Fatalf("SetNamespace(current): %v", err)
	}
	if got := cfg.Contexts["dev"].Namespace; got != "kube-system" {
		t.Errorf("dev namespace = %q, want kube-system", got)
	}

	if err := SetNamespace(cfg, "prod", "logging"); err != nil {
		t.Fatalf("SetNamespace(prod): %v", err)
	}
	if got := cfg.Contexts["prod"].Namespace; got != "logging" {
		t.Errorf("prod namespace = %q, want logging", got)
	}

	if err := SetNamespace(cfg, "nope", "x"); err == nil {
		t.Error("expected an error for an unknown context")
	}

	cfg.CurrentContext = ""
	if err := SetNamespace(cfg, "", "x"); err == nil {
		t.Error("expected an error when no context is current")
	}
}

func TestNamespace(t *testing.T) {
	cfg := testutil.Config(*fixture())

	got, err := Namespace(cfg, "")
	if err != nil {
		t.Fatalf("Namespace(current): %v", err)
	}
	if got != "default" {
		t.Errorf("current namespace = %q, want default", got)
	}

	if got, err = Namespace(cfg, "prod"); err != nil || got != "monitoring" {
		t.Errorf("Namespace(prod) = %q, %v", got, err)
	}
	if _, err := Namespace(cfg, "nope"); err == nil {
		t.Error("expected an error for an unknown context")
	}
}

func TestRename(t *testing.T) {
	cfg := testutil.Config(*fixture())

	if err := Rename(cfg, "dev", "development"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if Exists(cfg, "dev") {
		t.Error("old name still present")
	}
	if !Exists(cfg, "development") {
		t.Error("new name missing")
	}
	if cfg.CurrentContext != "development" {
		t.Errorf("current = %q, want development — rename must carry current-context", cfg.CurrentContext)
	}
	if got := cfg.Contexts["development"].Cluster; got != "dev-cluster" {
		t.Errorf("cluster ref lost: %q", got)
	}
}

func TestRenameErrors(t *testing.T) {
	tests := []struct {
		name     string
		from, to string
	}{
		{"unknown source", "nope", "x"},
		{"empty target", "dev", ""},
		{"same name", "dev", "dev"},
		{"target taken", "dev", "prod"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testutil.Config(*fixture())
			if err := Rename(cfg, tt.from, tt.to); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestRenameNonCurrentKeepsCurrent(t *testing.T) {
	cfg := testutil.Config(*fixture())

	if err := Rename(cfg, "prod", "production"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if cfg.CurrentContext != "dev" {
		t.Errorf("current = %q, want dev", cfg.CurrentContext)
	}
}

func TestDeleteReportsOrphans(t *testing.T) {
	cfg := testutil.Config(*fixture())

	orphans, err := Delete(cfg, "prod")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if Exists(cfg, "prod") {
		t.Error("prod not deleted")
	}
	if want := []string{"prod-cluster"}; !reflect.DeepEqual(orphans.Clusters, want) {
		t.Errorf("orphan clusters = %v, want %v", orphans.Clusters, want)
	}
	if want := []string{"prod-user"}; !reflect.DeepEqual(orphans.Users, want) {
		t.Errorf("orphan users = %v, want %v", orphans.Users, want)
	}
	if orphans.Empty() {
		t.Error("Empty() = true with orphans present")
	}
	if cfg.CurrentContext != "dev" {
		t.Errorf("current = %q, want dev", cfg.CurrentContext)
	}
}

func TestDeleteCurrentClearsCurrent(t *testing.T) {
	cfg := testutil.Config(*fixture())

	if _, err := Delete(cfg, "dev"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if cfg.CurrentContext != "" {
		t.Errorf("current = %q, want empty after deleting the current context", cfg.CurrentContext)
	}
}

func TestDeleteUnknownIsAtomic(t *testing.T) {
	cfg := testutil.Config(*fixture())

	if _, err := Delete(cfg, "dev", "nope"); err == nil {
		t.Fatal("expected an error")
	}
	if !Exists(cfg, "dev") {
		t.Error("dev was deleted even though the batch failed")
	}
}

func TestDeleteSharedClusterIsNotOrphaned(t *testing.T) {
	cfg := testutil.Config(testutil.Spec{
		Current: "a",
		Contexts: []testutil.Ctx{
			{Name: "a", Cluster: "shared", User: "shared"},
			{Name: "b", Cluster: "shared", User: "shared"},
		},
	})

	orphans, err := Delete(cfg, "a")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !orphans.Empty() {
		t.Errorf("shared cluster/user reported as orphaned: %+v", orphans)
	}
}

func TestPruneOrphans(t *testing.T) {
	cfg := testutil.Config(*fixture())

	orphans, err := Delete(cfg, "prod")
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	PruneOrphans(cfg, orphans)

	if _, ok := cfg.Clusters["prod-cluster"]; ok {
		t.Error("orphaned cluster survived the prune")
	}
	if _, ok := cfg.AuthInfos["prod-user"]; ok {
		t.Error("orphaned user survived the prune")
	}
	if _, ok := cfg.Clusters["dev-cluster"]; !ok {
		t.Error("in-use cluster was pruned")
	}
}
