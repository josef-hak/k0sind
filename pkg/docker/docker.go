// Package docker is a thin wrapper around the `docker` CLI. Like kind, k0sind
// does not embed the Docker SDK; it shells out to the docker binary. All exec
// calls go through a Runner interface so the argv-building logic can be unit
// tested without a real daemon.
package docker

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Runner executes `docker` commands. The real implementation shells out; tests
// substitute a fake.
type Runner interface {
	// Output runs `docker <args...>` and returns trimmed combined stdout.
	Output(args ...string) (string, error)
	// Stream runs `docker <args...>` connecting stdout/stderr to the terminal.
	Stream(args ...string) error
}

// execRunner is the production Runner backed by os/exec.
type execRunner struct{}

func (execRunner) Output(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func (execRunner) Stream(args ...string) error {
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stderr // progress goes to stderr, keeping stdout clean for data
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Client offers high-level operations over a Runner.
type Client struct {
	r Runner
}

// New returns a Client backed by the real docker CLI.
func New() *Client { return &Client{r: execRunner{}} }

// NewWithRunner returns a Client backed by a custom Runner (for tests).
func NewWithRunner(r Runner) *Client { return &Client{r: r} }

// RunSpec is a declarative description of a `docker run` invocation. Building it
// is pure data (see plan.go), which makes it unit-testable via DockerArgs.
type RunSpec struct {
	Name       string
	Hostname   string
	Image      string
	Privileged bool
	Network    string
	Labels     map[string]string
	Volumes    []string // values for -v
	Tmpfs      []string // values for --tmpfs
	Ports      []string // values for -p
	Cmd        []string // command + args appended after the image
}

// DockerArgs renders the RunSpec into the full argv passed to `docker`.
func (s RunSpec) DockerArgs() []string {
	args := []string{"run", "-d", "--name", s.Name}
	if s.Hostname != "" {
		args = append(args, "--hostname", s.Hostname)
	}
	if s.Privileged {
		args = append(args, "--privileged")
	}
	if s.Network != "" {
		args = append(args, "--network", s.Network)
	}
	for _, k := range sortedKeys(s.Labels) {
		args = append(args, "--label", fmt.Sprintf("%s=%s", k, s.Labels[k]))
	}
	for _, v := range s.Volumes {
		args = append(args, "-v", v)
	}
	for _, t := range s.Tmpfs {
		args = append(args, "--tmpfs", t)
	}
	for _, p := range s.Ports {
		args = append(args, "-p", p)
	}
	args = append(args, s.Image)
	args = append(args, s.Cmd...)
	return args
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// DaemonReady verifies the docker CLI can reach a running daemon.
func (c *Client) DaemonReady() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker CLI not found in PATH: %w", err)
	}
	if _, err := c.r.Output("info", "--format", "{{.ServerVersion}}"); err != nil {
		return fmt.Errorf("docker daemon not reachable: %w", err)
	}
	return nil
}

// NetworkEnsure creates the named bridge network if it does not already exist.
func (c *Client) NetworkEnsure(name string) error {
	out, err := c.r.Output("network", "ls", "--filter", "name=^"+name+"$", "--format", "{{.Name}}")
	if err != nil {
		return err
	}
	if out == name {
		return nil
	}
	_, err = c.r.Output("network", "create", name)
	return err
}

// NetworkRemove deletes the named network. It is a no-op if the network is
// missing or still in use.
func (c *Client) NetworkRemove(name string) error {
	_, err := c.r.Output("network", "rm", name)
	return err
}

// Pull fetches the image, streaming progress.
func (c *Client) Pull(image string) error {
	return c.r.Stream("pull", image)
}

// Run creates and starts a container from the spec, returning its ID.
func (c *Client) Run(spec RunSpec) (string, error) {
	return c.r.Output(spec.DockerArgs()...)
}

// Exec runs a command inside a container and returns its trimmed stdout.
func (c *Client) Exec(container string, cmd ...string) (string, error) {
	return c.r.Output(append([]string{"exec", container}, cmd...)...)
}

// Container is a single row from `docker ps`.
type Container struct {
	ID     string
	Name   string
	Labels map[string]string
}

// PS lists containers (including stopped) matching the given label filters, each
// given as "key=value".
func (c *Client) PS(labelFilters ...string) ([]Container, error) {
	args := []string{"ps", "-a", "--no-trunc", "--format", "{{.ID}}\t{{.Names}}\t{{.Labels}}"}
	for _, f := range labelFilters {
		args = append(args, "--filter", "label="+f)
	}
	out, err := c.r.Output(args...)
	if err != nil {
		return nil, err
	}
	var result []Container
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) < 2 {
			continue
		}
		ctr := Container{ID: fields[0], Name: fields[1], Labels: map[string]string{}}
		if len(fields) == 3 {
			ctr.Labels = parseLabels(fields[2])
		}
		result = append(result, ctr)
	}
	return result, nil
}

// parseLabels parses docker's comma-separated "k=v,k2=v2" label format.
func parseLabels(s string) map[string]string {
	labels := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		if k, v, ok := strings.Cut(kv, "="); ok {
			labels[k] = v
		}
	}
	return labels
}

// Port returns the host address and port bound to the given container port,
// e.g. Port("cp", 6443) -> ("127.0.0.1", "49153", nil).
func (c *Client) Port(container string, containerPort int) (addr, port string, err error) {
	out, err := c.r.Output("port", container, fmt.Sprintf("%d", containerPort))
	if err != nil {
		return "", "", err
	}
	// Output looks like "127.0.0.1:49153" (possibly multiple lines for tcp/udp).
	first := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
	host, p, ok := strings.Cut(strings.TrimSpace(first), ":")
	if !ok {
		return "", "", fmt.Errorf("unexpected `docker port` output: %q", out)
	}
	return host, p, nil
}

// Remove force-removes containers by ID or name (with their anonymous volumes).
func (c *Client) Remove(ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := c.r.Output(append([]string{"rm", "-f", "-v"}, ids...)...)
	return err
}
