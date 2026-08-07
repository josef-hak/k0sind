// Package cluster orchestrates k0s-in-docker clusters described by kind configs.
package cluster

import (
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/k0sproject/k0sind/pkg/config"
	"github.com/k0sproject/k0sind/pkg/docker"
	"github.com/k0sproject/k0sind/pkg/kubeconfig"
)

// Provider creates and manages k0sind clusters.
type Provider struct {
	docker *docker.Client
	out    io.Writer
}

// NewProvider returns a Provider backed by the real docker CLI, logging to stderr.
func NewProvider() *Provider {
	return &Provider{docker: docker.New(), out: os.Stderr}
}

// CreateOptions configures cluster creation.
type CreateOptions struct {
	Name       string
	ConfigPath string
	Image      string        // overrides config/default image
	Wait       time.Duration // 0 = return once nodes are started, don't wait for Ready
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
		image = DefaultImage
	}
	nodes := plan(opts.Name, cfg, image)
	controlPlane := nodes[0] // plan guarantees control-plane is first
	workers := nodes[1:]

	p.logf("Creating cluster %q (1 control-plane, %d worker(s)) ...", opts.Name, len(workers))

	p.logf(" • Ensuring node image (%s)", image)
	if err := p.docker.Pull(image); err != nil {
		// Non-fatal: the image may already be present locally.
		p.logf("   WARNING: could not pull %s: %v", image, err)
	}

	p.logf(" • Ensuring cluster network %q", Network)
	if err := p.docker.NetworkEnsure(Network); err != nil {
		return fmt.Errorf("ensure network: %w", err)
	}

	// Start control-plane.
	p.logf(" • Starting control-plane node %q", controlPlane.Name)
	if _, err := p.docker.Run(controlPlane.runSpec("")); err != nil {
		return err
	}
	p.logf(" • Waiting for k0s to start on the control-plane")
	if err := waitK0sRunning(p.docker, controlPlane.Name, 3*time.Minute, p.logf); err != nil {
		return p.rollback(opts.Name, fmt.Errorf("control-plane did not come up: %w", err))
	}
	p.logf(" • Control-plane is up")

	// Join workers using a fresh token per worker.
	for i, n := range workers {
		p.logf(" • Joining worker %q (%d/%d)", n.Name, i+1, len(workers))
		token, err := p.docker.Exec(controlPlane.Name, "k0s", "token", "create", "--role=worker")
		if err != nil {
			return p.rollback(opts.Name, fmt.Errorf("create worker token: %w", err))
		}
		if _, err := p.docker.Run(n.runSpec(token)); err != nil {
			return p.rollback(opts.Name, err)
		}
	}

	if opts.Wait > 0 {
		p.logf(" • Waiting up to %s for %d node(s) to be Ready", opts.Wait, len(nodes))
		if err := waitNodesReady(p.docker, controlPlane.Name, len(nodes), opts.Wait, p.logf); err != nil {
			return p.rollback(opts.Name, err)
		}
	}

	p.logf(" • Exporting kubeconfig (context %q)", kubeconfig.ContextName(opts.Name))
	if err := p.exportKubeconfig(opts.Name, controlPlane.Name); err != nil {
		return p.rollback(opts.Name, fmt.Errorf("export kubeconfig: %w", err))
	}

	ctx := kubeconfig.ContextName(opts.Name)
	p.logf("\nCluster %q is ready.", opts.Name)
	p.logf("Set kubectl context with:\n  kubectl config use-context %s", ctx)
	p.logf("Then try:\n  kubectl --context %s get nodes", ctx)
	return nil
}

// exportKubeconfig discovers the mapped API port and writes the merged context.
func (p *Provider) exportKubeconfig(clusterName, controlPlane string) error {
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
	return kubeconfig.Merge(clusterName, []byte(raw), host, port)
}

// ExportKubeconfig re-exports the kubeconfig for an existing cluster.
func (p *Provider) ExportKubeconfig(name string) error {
	if name == "" {
		name = config.DefaultClusterName
	}
	cp := name + "-control-plane"
	if err := p.exportKubeconfig(name, cp); err != nil {
		return err
	}
	p.logf("Exported kubeconfig context %q.", kubeconfig.ContextName(name))
	return nil
}

// rollback tears down a partially-created cluster and wraps the original error.
func (p *Provider) rollback(name string, cause error) error {
	p.logf("Rolling back cluster %q due to error ...", name)
	if err := p.Delete(name); err != nil {
		p.logf("WARNING: rollback cleanup failed: %v", err)
	}
	return cause
}

// Delete removes all containers for a cluster, its kubeconfig context, and the
// shared network if no other cluster is using it.
func (p *Provider) Delete(name string) error {
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
	if err := kubeconfig.Remove(name); err != nil {
		p.logf("WARNING: could not update kubeconfig: %v", err)
	}

	// Drop the shared network when no k0sind clusters remain.
	if remaining, err := p.docker.PS(LabelCluster); err == nil && len(remaining) == 0 {
		_ = p.docker.NetworkRemove(Network)
	}
	p.logf("Deleted cluster %q (%d container(s)).", name, len(ids))
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
