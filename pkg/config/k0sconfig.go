package config

import (
	"fmt"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
)

// kubeletConfigKind is the kubeadm patch kind k0s has an equivalent for.
const kubeletConfigKind = "KubeletConfiguration"

// defaultWorkerProfile is the profile k0s applies to every node that does not
// ask for another one (`k0s controller|worker --profile`, default "default").
const defaultWorkerProfile = "default"

// KubeletPatches returns the KubeletConfiguration documents found in a kind
// config's cluster-level kubeadmConfigPatches, in order. Patches for other
// kubeadm kinds (ClusterConfiguration, InitConfiguration, ...) have no k0s
// counterpart and are left to the ignored-field warnings.
func KubeletPatches(c *v1alpha4.Cluster) []string {
	var out []string
	for _, p := range c.KubeadmConfigPatches {
		if isKubeletConfig(p) {
			out = append(out, p)
		}
	}
	return out
}

// isKubeletConfig reports whether a patch document targets KubeletConfiguration.
func isKubeletConfig(doc string) bool {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := yaml.Unmarshal([]byte(doc), &head); err != nil {
		return false
	}
	return head.Kind == kubeletConfigKind
}

// K0sConfigWithKubelet folds KubeletConfiguration patches into a k0s
// ClusterConfig as the "default" worker profile, which every node picks up.
// base is an existing k0s config (from --k0s-config) or empty; its other
// settings are preserved and an existing "default" profile is merged into
// rather than replaced.
//
// Patches are merged key by key at the top level, later ones winning. kind
// applies a strategic merge, which differs for nested structures; the fields
// this is used for in practice (evictionHard, feature gates, log settings) are
// top-level, and the alternative - vendoring kubeadm's patch machinery - buys
// little for a tool that has no kubeadm.
func K0sConfigWithKubelet(base []byte, patches []string) ([]byte, error) {
	if len(patches) == 0 {
		return base, nil
	}

	cfg := map[string]any{}
	if len(base) > 0 {
		if err := yaml.Unmarshal(base, &cfg); err != nil {
			return nil, fmt.Errorf("parse k0s config: %w", err)
		}
	}
	setDefault(cfg, "apiVersion", "k0s.k0sproject.io/v1beta1")
	setDefault(cfg, "kind", "ClusterConfig")
	if _, ok := cfg["metadata"]; !ok {
		cfg["metadata"] = map[string]any{"name": "k0s"}
	}

	spec, _ := cfg["spec"].(map[string]any)
	if spec == nil {
		spec = map[string]any{}
	}
	profiles, _ := spec["workerProfiles"].([]any)

	values, idx := existingDefaultProfile(profiles)
	for _, p := range patches {
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(p), &doc); err != nil {
			return nil, fmt.Errorf("parse kubeadmConfigPatch: %w", err)
		}
		// apiVersion/kind identify the patch, they are not kubelet settings.
		delete(doc, "apiVersion")
		delete(doc, "kind")
		for k, v := range doc {
			values[k] = v
		}
	}

	profile := map[string]any{"name": defaultWorkerProfile, "values": values}
	if idx >= 0 {
		profiles[idx] = profile
	} else {
		profiles = append(profiles, profile)
	}
	spec["workerProfiles"] = profiles
	cfg["spec"] = spec

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("render k0s config: %w", err)
	}
	return out, nil
}

// existingDefaultProfile returns the values of the "default" worker profile and
// its index, or an empty map and -1 when the config has no such profile.
func existingDefaultProfile(profiles []any) (map[string]any, int) {
	for i, p := range profiles {
		m, ok := p.(map[string]any)
		if !ok || m["name"] != defaultWorkerProfile {
			continue
		}
		if v, ok := m["values"].(map[string]any); ok {
			return v, i
		}
		return map[string]any{}, i
	}
	return map[string]any{}, -1
}

func setDefault(m map[string]any, key, value string) {
	if _, ok := m[key]; !ok {
		m[key] = value
	}
}
