// Package cluster orchestrates k0s-in-docker clusters described by kind configs.
package cluster

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/k0sproject/k0sind/internal/status"
	"github.com/k0sproject/k0sind/pkg/config"
	"github.com/k0sproject/k0sind/pkg/docker"
	"github.com/k0sproject/k0sind/pkg/kubeconfig"
)

// Provider creates and manages k0sind clusters.
type Provider struct {
	docker *docker.Client
	out    io.Writer
	status *status.Status
}

// NewProvider returns a Provider backed by the real docker CLI, logging to stderr.
func NewProvider() *Provider {
	out := os.Stderr
	return &Provider{docker: docker.New(), out: out, status: status.New(out)}
}

// newProviderWithDocker returns a Provider on a caller-supplied docker client,
// for tests that drive it with a fake Runner.
func newProviderWithDocker(c *docker.Client, out io.Writer) *Provider {
	return &Provider{docker: c, out: out, status: status.New(out)}
}

// step runs a named phase with spinner/✓ progress, marking it ✗ on error.
func (p *Provider) step(msg string, fn func() error) error {
	p.status.Start(msg)
	if err := fn(); err != nil {
		p.status.Fail()
		return err
	}
	p.status.Done()
	return nil
}

// CreateOptions configures cluster creation.
type CreateOptions struct {
	Name          string
	ConfigPath    string
	K0sConfigPath string        // optional path to a k0s.yaml config file
	Image         string        // overrides config/default image
	Wait          time.Duration // 0 = return once nodes are started, don't wait for Ready
	Kubeconfig    string        // kubeconfig to write; empty = $KUBECONFIG or ~/.kube/config
	// KubeletRootDir overrides where the kubelet keeps its state; empty keeps
	// the k0s default of <data-dir>/kubelet.
	KubeletRootDir string
}

func (p *Provider) logf(format string, a ...any) {
	fmt.Fprintf(p.out, format+"\n", a...)
}

// Create builds a cluster: control-plane first, then token-joined workers, then
// exports a kubeconfig context. On failure after containers start, it rolls back.
func (p *Provider) Create(opts CreateOptions) error {
	if opts.Name == "" {
		opts.Name = config.DefaultClusterName
	}
	cfg, warnings, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		p.logf("WARNING: %s", w)
	}
	if err := p.docker.DaemonReady(); err != nil {
		return err
	}

	existing, err := p.docker.PS(LabelCluster + "=" + opts.Name)
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return fmt.Errorf("cluster %q already exists (found %d container(s)); delete it first", opts.Name, len(existing))
	}

	image := opts.Image
	if image == "" {
		image = DefaultNodeImage()
	}
	k0sCfg := opts.K0sConfigPath
	if k0sCfg != "" {
		if abs, err := filepath.Abs(k0sCfg); err == nil {
			k0sCfg = abs
		}
	}
	if patches := config.KubeletPatches(cfg); len(patches) > 0 {
		k0sCfg, err = p.writeKubeletProfile(opts.Name, k0sCfg, patches)
		if err != nil {
			return err
		}
	}
	nodes := plan(opts.Name, cfg, image, k0sCfg, opts.KubeletRootDir)
	controlPlane := nodes[0] // plan guarantees control-plane is first
	workers := nodes[1:]

	p.logf("Creating cluster %q (1 control-plane, %d worker(s)) ...", opts.Name, len(workers))

	if err := p.step(fmt.Sprintf("Ensuring node image (%s)", image), func() error {
		// Best-effort: the image may already be present locally, and the
		// control-plane run below will fail clearly if it is genuinely missing.
		_ = p.docker.Pull(image)
		return nil
	}); err != nil {
		return err
	}

	network := NetworkName()
	if NetworkOverridden() {
		p.logf("WARNING: Overriding docker network due to %s", NetworkEnv)
	}
	if err := p.step(fmt.Sprintf("Ensuring cluster network %q", network), func() error {
		return p.docker.NetworkEnsure(network)
	}); err != nil {
		return err
	}

	if err := p.step(fmt.Sprintf("Starting control-plane node %q", controlPlane.Name), func() error {
		if _, e := p.docker.Run(controlPlane.runSpec("")); e != nil {
			return e
		}
		return p.prepareNode(controlPlane.Name)
	}); err != nil {
		return p.rollback(opts.Name, opts.Kubeconfig, err)
	}

	if err := p.step("Waiting for the control-plane to be ready", func() error {
		return waitK0sRunning(p.docker, controlPlane.Name, 3*time.Minute, p.status.Update)
	}); err != nil {
		return p.rollback(opts.Name, opts.Kubeconfig, fmt.Errorf("control-plane did not come up: %w", err))
	}

	// Join workers using a fresh token per worker.
	for i, n := range workers {
		n := n
		if err := p.step(fmt.Sprintf("Joining worker node %q (%d/%d)", n.Name, i+1, len(workers)), func() error {
			token, e := p.docker.Exec(controlPlane.Name, "k0s", "token", "create", "--role=worker")
			if e != nil {
				return fmt.Errorf("create worker token: %w", e)
			}
			if _, e = p.docker.Run(n.runSpec(token)); e != nil {
				return e
			}
			return p.prepareNode(n.Name)
		}); err != nil {
			return p.rollback(opts.Name, opts.Kubeconfig, err)
		}
	}

	if opts.Wait > 0 {
		if err := p.step(fmt.Sprintf("Waiting for %d node(s) to be Ready", len(nodes)), func() error {
			return waitNodesReady(p.docker, controlPlane.Name, len(nodes), opts.Wait, p.status.Update)
		}); err != nil {
			return p.rollback(opts.Name, opts.Kubeconfig, err)
		}
	}

	if err := p.step(fmt.Sprintf("Exporting kubeconfig (context %q)", kubeconfig.ContextName(opts.Name)), func() error {
		return p.exportKubeconfig(opts.Name, controlPlane.Name, opts.Kubeconfig)
	}); err != nil {
		return p.rollback(opts.Name, opts.Kubeconfig, fmt.Errorf("export kubeconfig: %w", err))
	}

	ctx := kubeconfig.ContextName(opts.Name)
	p.logf("\nCluster %q is ready.", opts.Name)
	p.logf("Set kubectl context with:\n  kubectl config use-context %s", ctx)
	p.logf("Then try:\n  kubectl --context %s get nodes", ctx)
	return nil
}

// k0sConfigFile is where a generated k0s config for a cluster lives. It is
// bind-mounted into the control-plane, so it has to outlive Create: docker
// re-resolves a bind path when the container restarts, and would silently
// create a directory there if the file were gone.
func k0sConfigFile(cluster string) string {
	return filepath.Join(os.TempDir(), "k0sind-"+cluster+"-k0s.yaml")
}

// writeKubeletProfile folds KubeletConfiguration patches from the kind config
// into the k0s config as the "default" worker profile, which every node picks
// up, and returns the path to the result. base is the user's --k0s-config, if
// any; its other settings are preserved.
func (p *Provider) writeKubeletProfile(cluster, base string, patches []string) (string, error) {
	var raw []byte
	if base != "" {
		var err error
		if raw, err = os.ReadFile(base); err != nil {
			return "", fmt.Errorf("read k0s config %q: %w", base, err)
		}
	}
	merged, err := config.K0sConfigWithKubelet(raw, patches)
	if err != nil {
		return "", err
	}
	path := k0sConfigFile(cluster)
	if err := os.WriteFile(path, merged, 0o644); err != nil {
		return "", fmt.Errorf("write generated k0s config: %w", err)
	}
	p.logf("Applied %d kubeadmConfigPatch(es) as the %q worker profile", len(patches), "default")
	return path, nil
}

// prepareNode makes the node's root mount shared, right after the container
// starts and before the kubelet sets up any pod. The k0s image leaves the node's
// mounts private, which makes the kubelet refuse every pod that hostPath-mounts
// with mount propagation:
//
//	path "/var/run/netns" is mounted on "/run" but it is not a shared or slave mount
//
// CNI agents, storage drivers and node agents (istio-cni, velero, node-exporter)
// all need it. kind does the same in its node entrypoint.
func (p *Provider) prepareNode(node string) error {
	if _, err := p.docker.Exec(node, "mount", "--make-rshared", "/"); err != nil {
		return fmt.Errorf("make %s mounts rshared: %w", node, err)
	}
	return nil
}

// exportKubeconfig discovers the mapped API port and writes the merged context.
func (p *Provider) exportKubeconfig(clusterName, controlPlane, kubeconfigPath string) error {
	host, port, err := p.docker.Port(controlPlane, apiContainerPort)
	if err != nil {
		return fmt.Errorf("discover API server port: %w", err)
	}
	// Docker may report 0.0.0.0; normalize to loopback for local access.
	if host == "0.0.0.0" || host == "" || host == "::" {
		host = config.DefaultAPIServerAddress
	}
	raw, err := p.docker.Exec(controlPlane, "k0s", "kubeconfig", "admin")
	if err != nil {
		return err
	}
	return kubeconfig.Merge(clusterName, []byte(raw), host, port, kubeconfigPath)
}

// GetKubeconfig returns the rewritten admin kubeconfig YAML for an existing
// cluster. When internal is true, the original in-container API server address
// is preserved instead of being rewritten to the host-mapped port.
func (p *Provider) GetKubeconfig(name string, internal bool) ([]byte, error) {
	if name == "" {
		name = config.DefaultClusterName
	}
	cp := name + "-control-plane"
	raw, err := p.docker.Exec(cp, "k0s", "kubeconfig", "admin")
	if err != nil {
		return nil, err
	}
	var host, port string
	if !internal {
		host, port, err = p.docker.Port(cp, apiContainerPort)
		if err != nil {
			return nil, fmt.Errorf("discover API server port: %w", err)
		}
		if host == "0.0.0.0" || host == "" || host == "::" {
			host = config.DefaultAPIServerAddress
		}
	}
	return kubeconfig.Rewrite(name, []byte(raw), host, port)
}

// ExportKubeconfig re-exports the kubeconfig for an existing cluster.
func (p *Provider) ExportKubeconfig(name, kubeconfigPath string) error {
	if name == "" {
		name = config.DefaultClusterName
	}
	cp := name + "-control-plane"
	if err := p.exportKubeconfig(name, cp, kubeconfigPath); err != nil {
		return err
	}
	p.logf("Exported kubeconfig context %q.", kubeconfig.ContextName(name))
	return nil
}

// rollback tears down a partially-created cluster and wraps the original error.
func (p *Provider) rollback(name, kubeconfigPath string, cause error) error {
	p.logf("Rolling back cluster %q due to error ...", name)
	if err := p.Delete(name, kubeconfigPath); err != nil {
		p.logf("WARNING: rollback cleanup failed: %v", err)
	}
	return cause
}

// Delete removes all containers for a cluster, its kubeconfig context, and the
// shared network if no other cluster is using it.
func (p *Provider) Delete(name, kubeconfigPath string) error {
	if name == "" {
		name = config.DefaultClusterName
	}
	containers, err := p.docker.PS(LabelCluster + "=" + name)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(containers))
	for _, c := range containers {
		ids = append(ids, c.ID)
	}
	if err := p.docker.Remove(ids...); err != nil {
		return err
	}
	if err := kubeconfig.Remove(name, kubeconfigPath); err != nil {
		p.logf("WARNING: could not update kubeconfig: %v", err)
	}
	// Nothing references a generated k0s config once the nodes are gone.
	_ = os.Remove(k0sConfigFile(name))

	if len(ids) == 0 {
		p.logf("No cluster named %q found.", name)
		return nil
	}

	// Drop the shared network when no k0sind clusters remain. A network named
	// by the caller is theirs to manage, so leave it alone.
	if !NetworkOverridden() {
		if remaining, err := p.docker.PS(LabelCluster); err == nil && len(remaining) == 0 {
			_ = p.docker.NetworkRemove(Network)
		}
	}
	p.logf("Deleted cluster %q (%d container(s)).", name, len(ids))
	return nil
}

// LoadOptions configures LoadImages.
type LoadOptions struct {
	Name   string   // cluster name
	Images []string // local docker image references to load
	Nodes  []string // target node container names; empty = all nodes in the cluster
}

// containerArchivePath is where the saved image tar is staged inside each node
// before being imported into containerd, then removed.
const containerArchivePath = "/tmp/k0sind-load-image.tar"

// LoadImages saves the given images from the local docker daemon and imports
// them directly into the containerd content store of each target node (via
// `k0s ctr -n k8s.io images import`), so pods can reference them without a
// registry pull. This mirrors kind's `load docker-image`.
func (p *Provider) LoadImages(opts LoadOptions) error {
	if len(opts.Images) == 0 {
		return fmt.Errorf("no images specified")
	}
	name := opts.Name
	if name == "" {
		name = config.DefaultClusterName
	}

	nodes := opts.Nodes
	if len(nodes) == 0 {
		var err error
		nodes, err = p.ListNodes(name)
		if err != nil {
			return err
		}
	}
	if len(nodes) == 0 {
		return fmt.Errorf("cluster %q has no nodes (is it running?)", name)
	}

	tmpTar, err := os.CreateTemp("", "k0sind-load-*.tar")
	if err != nil {
		return fmt.Errorf("create temp archive: %w", err)
	}
	tmpTar.Close()
	tarPath := tmpTar.Name()
	defer os.Remove(tarPath)

	if err := p.step(fmt.Sprintf("Saving %d image(s)", len(opts.Images)), func() error {
		return p.docker.Save(opts.Images, tarPath)
	}); err != nil {
		return err
	}

	for _, node := range nodes {
		node := node
		if err := p.step(fmt.Sprintf("Loading image(s) into %q", node), func() error {
			if err := p.docker.CopyTo(tarPath, node, containerArchivePath); err != nil {
				return err
			}
			defer func() { _, _ = p.docker.Exec(node, "rm", "-f", containerArchivePath) }()
			_, err := p.docker.Exec(node, "k0s", "ctr", "-n", "k8s.io", "images", "import", containerArchivePath)
			return err
		}); err != nil {
			return err
		}
	}

	p.logf("Loaded image(s) into %d node(s).", len(nodes))
	return nil
}

// List returns the names of all k0sind clusters.
func (p *Provider) List() ([]string, error) {
	containers, err := p.docker.PS(LabelCluster)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, c := range containers {
		if name := c.Labels[LabelCluster]; name != "" {
			seen[name] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// ListNodes returns the container names of a cluster's nodes.
func (p *Provider) ListNodes(name string) ([]string, error) {
	if name == "" {
		name = config.DefaultClusterName
	}
	containers, err := p.docker.PS(LabelCluster + "=" + name)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(containers))
	for _, c := range containers {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return names, nil
}
