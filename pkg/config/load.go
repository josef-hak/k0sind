// Package config loads kind-compatible cluster configuration. k0sind reuses the
// upstream kind v1alpha4 types verbatim so existing kind config files parse
// unchanged; only a subset of fields is honored (topology, ports, mounts) and
// kind-specific fields that have no k0s equivalent produce warnings.
package config

import (
	"fmt"
	"os"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
)

const (
	// DefaultClusterName mirrors kind's default cluster name behavior.
	DefaultClusterName = "k0sind"
	// DefaultAPIServerAddress is where the API server is exposed on the host.
	DefaultAPIServerAddress = "127.0.0.1"
)

// Load reads a kind-style config from path (empty path = a default single
// control-plane cluster), applies defaults, and validates it. It returns the
// cluster, a list of human-readable warnings about ignored fields, and an error
// for unsupported topologies.
func Load(path string) (*v1alpha4.Cluster, []string, error) {
	c := &v1alpha4.Cluster{}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("read config %q: %w", path, err)
		}
		if err := yaml.UnmarshalStrict(data, c); err != nil {
			return nil, nil, fmt.Errorf("parse kind config %q: %w", path, err)
		}
	}
	applyDefaults(c)
	warnings := unsupportedWarnings(c)
	if err := validate(c); err != nil {
		return nil, warnings, err
	}
	return c, warnings, nil
}

// applyDefaults fills in the same defaults kind uses for the fields we honor.
func applyDefaults(c *v1alpha4.Cluster) {
	if len(c.Nodes) == 0 {
		c.Nodes = []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}}
	}
	for i := range c.Nodes {
		if c.Nodes[i].Role == "" {
			c.Nodes[i].Role = v1alpha4.ControlPlaneRole
		}
	}
	if c.Networking.APIServerAddress == "" {
		c.Networking.APIServerAddress = DefaultAPIServerAddress
	}
}

// validate enforces the 0.1.0 topology limits.
func validate(c *v1alpha4.Cluster) error {
	var controlPlanes, workers int
	for _, n := range c.Nodes {
		switch n.Role {
		case v1alpha4.ControlPlaneRole:
			controlPlanes++
		case v1alpha4.WorkerRole:
			workers++
		default:
			return fmt.Errorf("unknown node role %q (expected control-plane or worker)", n.Role)
		}
	}
	switch {
	case controlPlanes == 0:
		return fmt.Errorf("config has no control-plane node")
	case controlPlanes > 1:
		return fmt.Errorf("k0sind 0.1.0 supports a single control-plane node, got %d "+
			"(multi-controller HA is planned for a later release)", controlPlanes)
	}
	return nil
}

// unsupportedWarnings reports kind fields with no k0s mapping that are ignored.
func unsupportedWarnings(c *v1alpha4.Cluster) []string {
	var w []string
	add := func(field string) {
		w = append(w, fmt.Sprintf("%q is a kind/kubeadm-specific field with no k0s equivalent; ignored", field))
	}
	if len(c.KubeadmConfigPatches) > 0 || len(c.KubeadmConfigPatchesJSON6902) > 0 {
		add("kubeadmConfigPatches")
	}
	if len(c.ContainerdConfigPatches) > 0 || len(c.ContainerdConfigPatchesJSON6902) > 0 {
		add("containerdConfigPatches")
	}
	if len(c.FeatureGates) > 0 {
		add("featureGates")
	}
	if len(c.RuntimeConfig) > 0 {
		add("runtimeConfig")
	}
	if c.Networking.DisableDefaultCNI {
		add("networking.disableDefaultCNI")
	}
	if c.Networking.KubeProxyMode != "" {
		add("networking.kubeProxyMode")
	}
	if c.Networking.PodSubnet != "" {
		add("networking.podSubnet")
	}
	if c.Networking.ServiceSubnet != "" {
		add("networking.serviceSubnet")
	}
	if c.Networking.IPFamily != "" && c.Networking.IPFamily != v1alpha4.IPv4Family {
		add("networking.ipFamily (only ipv4 is supported)")
	}
	for i := range c.Nodes {
		if len(c.Nodes[i].KubeadmConfigPatches) > 0 || len(c.Nodes[i].KubeadmConfigPatchesJSON6902) > 0 {
			add(fmt.Sprintf("nodes[%d].kubeadmConfigPatches", i))
		}
		if len(c.Nodes[i].Labels) > 0 {
			add(fmt.Sprintf("nodes[%d].labels", i))
		}
	}
	return w
}
