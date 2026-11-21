package cluster

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/k0sproject/k0sind/pkg/config"
)

// ExportLogs writes everything needed to diagnose a cluster into dir, creating
// it if missing; an empty dir means a fresh temp directory. It returns the
// directory written to.
//
// The collection is deliberately container-level rather than cluster-level. A
// cluster worth exporting logs from is usually one whose API server never came
// up, so anything that goes through kubectl is the first thing to stop working
// — `docker logs` and /var/log/pods keep answering when it does.
//
// Nothing here aborts on failure: a command that fails writes its error into
// its own file and collection continues, because which commands fail is itself
// part of the diagnosis.
func (p *Provider) ExportLogs(name, dir string) (string, error) {
	if name == "" {
		name = config.DefaultClusterName
	}
	nodes, err := p.ListNodes(name)
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", fmt.Errorf("no nodes found for cluster %q", name)
	}

	if dir == "" {
		dir, err = os.MkdirTemp("", "k0sind-logs-"+name+"-")
		if err != nil {
			return "", fmt.Errorf("create temp dir: %w", err)
		}
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %q: %w", dir, err)
	}

	c := &collector{provider: p}
	p.logf("Exporting logs for cluster %q to %s", name, dir)

	c.capture(filepath.Join(dir, "docker-info.txt"), p.docker.Info)

	for _, node := range nodes {
		nodeDir := filepath.Join(dir, node)
		if err := os.MkdirAll(nodeDir, 0o755); err != nil {
			return dir, fmt.Errorf("create %q: %w", nodeDir, err)
		}
		at := func(f string) string { return filepath.Join(nodeDir, f) }

		c.capture(at("inspect.json"), func() (string, error) { return p.docker.Inspect(node) })
		c.capture(at("serial.log"), func() (string, error) { return p.docker.Logs(node) })
		c.capture(at("k0s-status.txt"), c.exec(node, "k0s", "status"))
		c.capture(at("k0s-version.txt"), c.exec(node, "k0s", "version"))
		c.capture(at("images.log"), c.exec(node, "k0s", "ctr", "-n", "k8s.io", "images", "ls"))

		// The per-container stdout of every pod that ran on this node. k0s
		// mounts this path, so it survives a control plane that never answered.
		if err := p.docker.CopyFrom(node, podLogDir, at("pods")); err != nil {
			c.failed = append(c.failed, fmt.Sprintf("%s: copy %s: %v", node, podLogDir, err))
		}

		if !strings.HasSuffix(node, controlPlaneSuffix) {
			continue
		}
		// Cluster-wide state, which only the control plane can answer for and
		// only while the API server is up. Collected last for that reason.
		for file, args := range map[string][]string{
			"kubectl-nodes.txt":          {"get", "nodes", "-o", "wide"},
			"kubectl-pods.txt":           {"get", "pods", "-A", "-o", "wide"},
			"kubectl-events.txt":         {"get", "events", "-A", "--sort-by=.lastTimestamp"},
			"kubectl-describe-nodes.txt": {"describe", "nodes"},
			"kubectl-describe-pods.txt":  {"describe", "pods", "-A"},
		} {
			c.capture(at(file), c.exec(node, append([]string{"k0s", "kubectl"}, args...)...))
		}
	}

	if len(c.failed) > 0 {
		p.logf("%d collector(s) reported an error; see the files for details:", len(c.failed))
		for _, f := range c.failed {
			p.logf("  %s", f)
		}
	}
	p.logf("Logs exported to %s", dir)
	return dir, nil
}

// podLogDir is where the kubelet keeps each container's stdout. The node
// containers mount it (see plan.go), so it is readable even when nothing else
// in the cluster is.
const podLogDir = "/var/log/pods"

// controlPlaneSuffix identifies the node that can answer kubectl queries.
const controlPlaneSuffix = "-control-plane"

// collector runs the individual commands and remembers which ones failed.
type collector struct {
	provider *Provider
	failed   []string
}

// exec adapts a command inside a node to the shape capture wants.
func (c *collector) exec(node string, cmd ...string) func() (string, error) {
	return func() (string, error) { return c.provider.docker.Exec(node, cmd...) }
}

// capture writes fn's output to path. A failing command still produces a file,
// holding the error: an empty kubectl-pods.txt and one saying the API server
// refused the connection mean very different things to whoever reads the dump.
func (c *collector) capture(path string, fn func() (string, error)) {
	out, err := fn()
	if err != nil {
		out += fmt.Sprintf("\n[k0sind] command failed: %v\n", err)
		c.failed = append(c.failed, fmt.Sprintf("%s: %v", filepath.Base(path), err))
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		c.failed = append(c.failed, fmt.Sprintf("write %s: %v", path, err))
	}
}
