package kubeconfig

import (
	"path/filepath"
	"testing"

	"k8s.io/client-go/tools/clientcmd"
)

const adminKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: k0s
  cluster:
    server: https://172.18.0.2:6443
    certificate-authority-data: Zm9v
users:
- name: admin
  user:
    client-certificate-data: Zm9v
    client-key-data: Zm9v
contexts:
- name: k0s
  context:
    cluster: k0s
    user: admin
current-context: k0s
`

func TestMergeAndRemove(t *testing.T) {
	dir := t.TempDir()
	kubeconfigPath := filepath.Join(dir, "config")
	t.Setenv("KUBECONFIG", kubeconfigPath)

	if err := Merge("dev", []byte(adminKubeconfig), "127.0.0.1", "49153"); err != nil {
		t.Fatalf("merge: %v", err)
	}

	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		t.Fatalf("load merged: %v", err)
	}
	key := ContextName("dev") // k0sind-dev
	if cfg.CurrentContext != key {
		t.Fatalf("current-context = %q, want %q", cfg.CurrentContext, key)
	}
	cl, ok := cfg.Clusters[key]
	if !ok {
		t.Fatalf("cluster %q missing; have %v", key, keys(cfg.Clusters))
	}
	if cl.Server != "https://127.0.0.1:49153" {
		t.Fatalf("server not rewritten: %q", cl.Server)
	}
	if _, ok := cfg.Contexts[key]; !ok {
		t.Fatalf("context %q missing", key)
	}

	// Merging a second cluster must preserve the first.
	if err := Merge("prod", []byte(adminKubeconfig), "127.0.0.1", "50000"); err != nil {
		t.Fatalf("merge second: %v", err)
	}
	cfg, _ = clientcmd.LoadFromFile(kubeconfigPath)
	if _, ok := cfg.Clusters[ContextName("dev")]; !ok {
		t.Fatalf("first cluster lost after second merge")
	}

	// Remove drops only the named cluster.
	if err := Remove("dev"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	cfg, _ = clientcmd.LoadFromFile(kubeconfigPath)
	if _, ok := cfg.Clusters[ContextName("dev")]; ok {
		t.Fatalf("cluster dev still present after remove")
	}
	if _, ok := cfg.Clusters[ContextName("prod")]; !ok {
		t.Fatalf("cluster prod should remain")
	}
}

func TestRewrite(t *testing.T) {
	data, err := Rewrite("dev", []byte(adminKubeconfig), "127.0.0.1", "49153")
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	cfg, err := clientcmd.Load(data)
	if err != nil {
		t.Fatalf("parse rewritten kubeconfig: %v", err)
	}
	key := ContextName("dev")
	if cfg.CurrentContext != key {
		t.Fatalf("current-context = %q, want %q", cfg.CurrentContext, key)
	}
	cl, ok := cfg.Clusters[key]
	if !ok {
		t.Fatalf("cluster %q missing", key)
	}
	if cl.Server != "https://127.0.0.1:49153" {
		t.Fatalf("server = %q, want %q", cl.Server, "https://127.0.0.1:49153")
	}
	if _, ok := cfg.AuthInfos[key]; !ok {
		t.Fatalf("user %q missing", key)
	}
	if ctx, ok := cfg.Contexts[key]; !ok {
		t.Fatalf("context %q missing", key)
	} else if ctx.Cluster != key || ctx.AuthInfo != key {
		t.Fatalf("context references wrong cluster/user: %q/%q", ctx.Cluster, ctx.AuthInfo)
	}
}

func TestRemoveMissingFileIsNoError(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "does-not-exist"))
	if err := Remove("dev"); err != nil {
		t.Fatalf("expected nil for missing kubeconfig, got %v", err)
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
