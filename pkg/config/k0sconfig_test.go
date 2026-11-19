package config

import (
	"strings"
	"testing"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
)

const kubeletPatch = `
kind: KubeletConfiguration
apiVersion: kubelet.config.k8s.io/v1beta1
evictionHard:
  imagefs.available: "1Gi"
  nodefs.available: "1Gi"
`

const clusterPatch = `
kind: ClusterConfiguration
apiVersion: kubeadm.k8s.io/v1beta3
apiServer:
  extraArgs:
    v: "4"
`

func TestKubeletPatchesSelectsOnlyKubeletConfiguration(t *testing.T) {
	c := &v1alpha4.Cluster{KubeadmConfigPatches: []string{clusterPatch, kubeletPatch}}
	got := KubeletPatches(c)
	if len(got) != 1 || !strings.Contains(got[0], "evictionHard") {
		t.Fatalf("expected only the KubeletConfiguration patch, got %v", got)
	}
}

// profileValues digs the default worker profile out of a rendered k0s config.
func profileValues(t *testing.T, out []byte) map[string]any {
	t.Helper()
	var cfg struct {
		Spec struct {
			WorkerProfiles []struct {
				Name   string         `json:"name"`
				Values map[string]any `json:"values"`
			} `json:"workerProfiles"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(out, &cfg); err != nil {
		t.Fatalf("parse rendered config: %v\n%s", err, out)
	}
	for _, p := range cfg.Spec.WorkerProfiles {
		if p.Name == "default" {
			return p.Values
		}
	}
	t.Fatalf("no %q worker profile in:\n%s", "default", out)
	return nil
}

func TestK0sConfigWithKubeletFromScratch(t *testing.T) {
	out, err := K0sConfigWithKubelet(nil, []string{kubeletPatch})
	if err != nil {
		t.Fatalf("K0sConfigWithKubelet: %v", err)
	}
	values := profileValues(t, out)
	ev, ok := values["evictionHard"].(map[string]any)
	if !ok {
		t.Fatalf("evictionHard missing from %v", values)
	}
	if ev["imagefs.available"] != "1Gi" {
		t.Fatalf("evictionHard = %v", ev)
	}
	// apiVersion/kind identify the patch; they are not kubelet settings.
	if _, ok := values["kind"]; ok {
		t.Fatalf("patch kind leaked into the profile: %v", values)
	}
	if !strings.Contains(string(out), "k0s.k0sproject.io/v1beta1") {
		t.Fatalf("rendered config is not a k0s ClusterConfig:\n%s", out)
	}
}

func TestK0sConfigWithKubeletKeepsExistingConfig(t *testing.T) {
	base := []byte(`
apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
metadata:
  name: k0s
spec:
  network:
    provider: calico
`)
	out, err := K0sConfigWithKubelet(base, []string{kubeletPatch})
	if err != nil {
		t.Fatalf("K0sConfigWithKubelet: %v", err)
	}
	if !strings.Contains(string(out), "calico") {
		t.Fatalf("existing k0s settings were dropped:\n%s", out)
	}
	if _, ok := profileValues(t, out)["evictionHard"]; !ok {
		t.Fatalf("kubelet patch not applied:\n%s", out)
	}
}

func TestK0sConfigWithKubeletMergesIntoExistingDefaultProfile(t *testing.T) {
	base := []byte(`
apiVersion: k0s.k0sproject.io/v1beta1
kind: ClusterConfig
spec:
  workerProfiles:
    - name: default
      values:
        shutdownGracePeriod: 30s
`)
	out, err := K0sConfigWithKubelet(base, []string{kubeletPatch})
	if err != nil {
		t.Fatalf("K0sConfigWithKubelet: %v", err)
	}
	values := profileValues(t, out)
	if values["shutdownGracePeriod"] != "30s" {
		t.Fatalf("existing profile values were replaced: %v", values)
	}
	if _, ok := values["evictionHard"]; !ok {
		t.Fatalf("patch not merged into the existing profile: %v", values)
	}
}

func TestK0sConfigWithKubeletNoPatchesIsPassThrough(t *testing.T) {
	base := []byte("spec: {}\n")
	out, err := K0sConfigWithKubelet(base, nil)
	if err != nil {
		t.Fatalf("K0sConfigWithKubelet: %v", err)
	}
	if string(out) != string(base) {
		t.Fatalf("config changed without patches:\n%s", out)
	}
}
