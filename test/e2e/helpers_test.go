//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"
)

// Progress marks, matching the ones the k0rdent test suites use. Emoji rather
// than ANSI-coloured ✓/✗ so the colour survives a redirected log file.
const (
	markOK   = "✅"
	markFail = "❌"
	markWait = "⏳"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// run executes a command, streaming output to the test log.
func run(t *testing.T, bin string, args ...string) error {
	t.Helper()
	t.Logf("$ %s %v", bin, args)
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	return err
}

// docker runs a docker command and returns its combined output.
func docker(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	return string(out), err
}

// pod is one row of the cluster's pod summary.
type pod struct {
	name  string // namespace/name
	state string // ready, phase and restarts: the fields a change is judged on
	age   string
	node  string
}

// line renders the row. Age is shown but never compared, so a pod that only
// got older does not read as a change.
func (p pod) line() string {
	return fmt.Sprintf("      %-46s %-30s %-5s %s", p.name, p.state, p.age, p.node)
}

// podsJSON is the slice of `kubectl get pods -o json` we care about.
type podsJSON struct {
	Items []struct {
		Metadata struct {
			Name              string    `json:"name"`
			Namespace         string    `json:"namespace"`
			CreationTimestamp time.Time `json:"creationTimestamp"`
		} `json:"metadata"`
		Spec struct {
			NodeName string `json:"nodeName"`
		} `json:"spec"`
		Status struct {
			Phase             string `json:"phase"`
			ContainerStatuses []struct {
				Ready        bool `json:"ready"`
				RestartCount int  `json:"restartCount"`
				State        map[string]struct {
					Reason string `json:"reason"`
				} `json:"state"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

// clusterPods returns every pod in the cluster, sorted, so that an API server
// reordering does not read as a change. ready counts the pods that are fully
// up, done the ones that exited successfully.
func clusterPods(t *testing.T, cp string) (pods []pod, ready, done int, err error) {
	t.Helper()
	out, err := docker(t, "exec", cp, "k0s", "kubectl", "get", "pods", "-A", "-o", "json")
	if err != nil {
		return nil, 0, 0, fmt.Errorf("get pods: %w\n%s", err, out)
	}
	var list podsJSON
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		return nil, 0, 0, fmt.Errorf("parse pods: %w", err)
	}
	for _, it := range list.Items {
		cs := it.Status.ContainerStatuses
		up, restarts := 0, 0
		var reasons []string
		for _, c := range cs {
			if c.Ready {
				up++
			}
			restarts += c.RestartCount
			for k, s := range c.State {
				if k != "running" && s.Reason != "" {
					reasons = append(reasons, s.Reason)
				}
			}
		}
		sort.Strings(reasons)
		state := fmt.Sprintf("%s %d/%d", it.Status.Phase, up, len(cs))
		if restarts > 0 {
			state += fmt.Sprintf(" r%d", restarts)
		}
		if len(reasons) > 0 {
			state += " " + strings.Join(reasons, ",")
		}
		pods = append(pods, pod{
			name:  it.Metadata.Namespace + "/" + it.Metadata.Name,
			state: state,
			age:   age(it.Metadata.CreationTimestamp),
			node:  it.Spec.NodeName,
		})
		switch {
		case it.Status.Phase == "Succeeded":
			done++
		case it.Status.Phase == "Running" && len(cs) > 0 && up == len(cs):
			ready++
		}
	}
	sort.Slice(pods, func(i, j int) bool { return pods[i].name < pods[j].name })
	return pods, ready, done, nil
}

func age(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

// podTable renders rows as an aligned block.
func podTable(pods []pod) string {
	lines := make([]string, 0, len(pods))
	for _, p := range pods {
		lines = append(lines, p.line())
	}
	return strings.Join(lines, "\n")
}

// podChanges returns the rows that differ from the previous poll: pods that
// changed state, pods that appeared, and pods that are gone. A cluster coming
// up is mostly pods that already settled, and reprinting a dozen "Running 1/1"
// lines every two seconds buries the one that actually moved.
func podChanges(now, before []pod) []pod {
	prev := make(map[string]string, len(before))
	for _, p := range before {
		prev[p.name] = p.state
	}
	var changed []pod
	seen := make(map[string]bool, len(now))
	for _, p := range now {
		seen[p.name] = true
		if s, ok := prev[p.name]; !ok || s != p.state {
			changed = append(changed, p)
		}
	}
	for _, p := range before {
		if !seen[p.name] {
			changed = append(changed, pod{name: p.name, state: "gone", age: "-"})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].name < changed[j].name })
	return changed
}
