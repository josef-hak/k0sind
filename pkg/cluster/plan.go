package cluster

import (
	"fmt"
	"strings"

	"github.com/k0sproject/k0sind/pkg/docker"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const (
	// Network is the shared docker bridge network all k0sind nodes join.
	// This matches kind's default network name so k0sind and kind clusters
	// can reach each other (e.g. a shared local registry).
	Network = "kind"
	// LabelCluster tags every container with its cluster name.
	LabelCluster = "io.k0sind.cluster"
	// LabelRole records the node role (control-plane / worker).
	LabelRole = "io.k0sind.role"
	// DefaultImage is the k0s image used when the config does not override it.
	// Bump to the current stable k0s release when cutting a k0sind version.
	DefaultImage = "docker.io/k0sproject/k0s:v1.36.3-k0s.1-bash"

	// apiContainerPort is the port the k0s API server listens on inside the container.
	apiContainerPort = 6443
)

// planNode is the fully-resolved description of one node container, derived from
// a kind config. Everything except the worker join token is known up front; the
// token is injected at run time via runSpec.
type planNode struct {
	Name          string
	Role          v1alpha4.NodeRole
	Image         string
	Privileged    bool
	Volumes       []string
	Tmpfs         []string
	Ports         []string
	Labels        map[string]string
	single        bool   // true when the whole cluster is this one node
	k0sConfigPath string // host path to k0s.yaml; mounted into the container when non-empty
}

// isControlPlane reports whether the node runs the k0s controller.
func (n planNode) isControlPlane() bool { return n.Role == v1alpha4.ControlPlaneRole }

// plan translates a kind cluster into ordered node descriptors. The control-plane
// node is always first so it can be started (and a join token minted) before workers.
func plan(clusterName string, c *v1alpha4.Cluster, image, k0sConfigPath string) []planNode {
	single := len(c.Nodes) == 1
	var nodes []planNode
	workerIdx := 0
	for _, n := range c.Nodes {
		img := image
		if n.Image != "" {
			img = n.Image
		}
		var name string
		if n.Role == v1alpha4.ControlPlaneRole {
			name = clusterName + "-control-plane"
		} else {
			workerIdx++
			if workerIdx == 1 {
				name = clusterName + "-worker"
			} else {
				name = fmt.Sprintf("%s-worker%d", clusterName, workerIdx)
			}
		}
		pn := planNode{
			Name:       name,
			Role:       n.Role,
			Image:      img,
			Privileged: true,
			Volumes:    []string{"/var/lib/k0s", "/var/log/pods"},
			Tmpfs:      []string{"/run"},
			Labels: map[string]string{
				LabelCluster: clusterName,
				LabelRole:    string(n.Role),
			},
			single: single,
		}
		for _, m := range n.ExtraMounts {
			pn.Volumes = append(pn.Volumes, mountArg(m))
		}
		if n.Role == v1alpha4.ControlPlaneRole {
			pn.k0sConfigPath = k0sConfigPath
			pn.Ports = append(pn.Ports, apiServerPortArg(c.Networking))
		}
		for _, p := range n.ExtraPortMappings {
			pn.Ports = append(pn.Ports, portArg(p))
		}
		// Keep control-plane first regardless of config ordering.
		if n.Role == v1alpha4.ControlPlaneRole {
			nodes = append([]planNode{pn}, nodes...)
		} else {
			nodes = append(nodes, pn)
		}
	}
	return nodes
}

// runSpec builds the docker.RunSpec for the node. token is only used by workers.
// k0sConfigContainerPath is where the k0s config file is mounted inside the
// container. We avoid /etc/k0s/ because the k0s image already has that
// directory, causing Docker to mount a directory instead of a file.
const k0sConfigContainerPath = "/tmp/k0s-config.yaml"

func (n planNode) runSpec(token string) docker.RunSpec {
	volumes := n.Volumes
	if n.k0sConfigPath != "" {
		volumes = append(volumes, n.k0sConfigPath+":"+k0sConfigContainerPath+":ro")
	}
	spec := docker.RunSpec{
		Name:       n.Name,
		Hostname:   n.Name,
		Image:      n.Image,
		Privileged: n.Privileged,
		Network:    Network,
		Labels:     n.Labels,
		Volumes:    volumes,
		Tmpfs:      n.Tmpfs,
		Ports:      n.Ports,
		Cmd:        n.k0sCommand(token),
	}
	return spec
}

// k0sCommand returns the k0s subcommand for the node. A single-node cluster runs
// the controller with worker enabled and no taints so workloads schedule on it
// (matching kind's single-node behavior); with dedicated workers the control-plane
// keeps its default taint so pods land on the workers.
func (n planNode) k0sCommand(token string) []string {
	if n.isControlPlane() {
		cmd := []string{"k0s", "controller", "--enable-worker"}
		if n.single {
			cmd = append(cmd, "--no-taints")
		}
		if n.k0sConfigPath != "" {
			cmd = append(cmd, "--config", k0sConfigContainerPath)
		}
		return cmd
	}
	return []string{"k0s", "worker", token}
}

// mountArg renders a kind Mount as a docker -v value.
func mountArg(m v1alpha4.Mount) string {
	arg := fmt.Sprintf("%s:%s", m.HostPath, m.ContainerPath)
	if m.Readonly {
		arg += ":ro"
	}
	return arg
}

// portArg renders a kind PortMapping as a docker -p value:
// [listenAddress:]hostPort:containerPort[/protocol].
func portArg(p v1alpha4.PortMapping) string {
	var b strings.Builder
	if p.ListenAddress != "" {
		b.WriteString(p.ListenAddress)
		b.WriteString(":")
	}
	if p.HostPort != 0 {
		fmt.Fprintf(&b, "%d", p.HostPort)
	}
	fmt.Fprintf(&b, ":%d", p.ContainerPort)
	if p.Protocol != "" && !strings.EqualFold(string(p.Protocol), "TCP") {
		b.WriteString("/")
		b.WriteString(strings.ToLower(string(p.Protocol)))
	}
	return b.String()
}

// apiServerPortArg maps the k0s API server to the host. A zero APIServerPort lets
// docker choose a free host port (discovered later via `docker port`).
func apiServerPortArg(net v1alpha4.Networking) string {
	addr := net.APIServerAddress
	if net.APIServerPort != 0 {
		return fmt.Sprintf("%s:%d:%d", addr, net.APIServerPort, apiContainerPort)
	}
	return fmt.Sprintf("%s::%d", addr, apiContainerPort)
}
