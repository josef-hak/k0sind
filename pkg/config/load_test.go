package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "kind.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaultSingleControlPlane(t *testing.T) {
	c, warnings, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Nodes) != 1 || c.Nodes[0].Role != v1alpha4.ControlPlaneRole {
		t.Fatalf("expected 1 control-plane node, got %+v", c.Nodes)
	}
	if c.Networking.APIServerAddress != DefaultAPIServerAddress {
		t.Fatalf("expected default api address, got %q", c.Networking.APIServerAddress)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
}

func TestLoadMultiNode(t *testing.T) {
	p := writeConfig(t, `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: worker
- role: worker
`)
	c, _, err := Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(c.Nodes))
	}
}

func TestLoadRejectsMultipleControlPlanes(t *testing.T) {
	p := writeConfig(t, `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
- role: control-plane
`)
	_, _, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "single control-plane") {
		t.Fatalf("expected single-control-plane error, got %v", err)
	}
}

func TestLoadWarnsOnUnsupportedFields(t *testing.T) {
	p := writeConfig(t, `
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
kubeadmConfigPatches:
- |
  kind: ClusterConfiguration
networking:
  disableDefaultCNI: true
nodes:
- role: control-plane
`)
	_, warnings, err := Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "kubeadmConfigPatches") || !strings.Contains(joined, "disableDefaultCNI") {
		t.Fatalf("expected warnings about kubeadmConfigPatches and disableDefaultCNI, got %v", warnings)
	}
}
