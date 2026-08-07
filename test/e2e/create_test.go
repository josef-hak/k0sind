//go:build e2e

// Package e2e exercises k0sind against a real docker daemon. It is guarded by
// the `e2e` build tag so `go test ./...` (the unit job) never spawns containers.
// Run with: go test -tags e2e ./test/e2e/... -timeout 20m
package e2e

import (
	"strings"
	"testing"
	"time"
)

// k0sindBin is the binary under test; override with K0SIND_BIN in CI.
func k0sindBin(t *testing.T) string {
	if b := envOr("K0SIND_BIN", ""); b != "" {
		return b
	}
	t.Fatal("K0SIND_BIN must point to the built k0sind binary")
	return ""
}

func TestCreateSingleNode(t *testing.T) {
	runScenario(t, "ci-single", nil, 1, 120*time.Second)
}

func TestCreateOneControlPlaneTwoWorkers(t *testing.T) {
	runScenario(t, "ci-multi", []string{"--config", "../../examples/multi-node.yaml"}, 3, 180*time.Second)
}

// runScenario creates a cluster, asserts the expected number of Ready nodes,
// schedules a pod, then deletes the cluster and verifies cleanup.
func runScenario(t *testing.T, name string, extraArgs []string, wantNodes int, wait time.Duration) {
	bin := k0sindBin(t)
	cp := name + "-control-plane"

	// Always clean up, even on failure.
	t.Cleanup(func() {
		_ = run(t, bin, "delete", "cluster", "--name", name)
		if out, _ := docker(t, "ps", "-aq", "--filter", "label=io.k0sind.cluster="+name); strings.TrimSpace(out) != "" {
			t.Errorf("containers for %q survived deletion: %q", name, out)
		}
	})

	args := append([]string{"create", "cluster", "--name", name, "--wait", wait.String()}, extraArgs...)
	if err := run(t, bin, args...); err != nil {
		t.Fatalf("create cluster: %v", err)
	}

	// Count Ready nodes reported by k0s.
	out, err := docker(t, "exec", cp, "k0s", "kubectl", "get", "nodes", "--no-headers")
	if err != nil {
		t.Fatalf("get nodes: %v", err)
	}
	ready := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[1] == "Ready" {
			ready++
		}
	}
	if ready != wantNodes {
		t.Fatalf("expected %d Ready nodes, got %d\n%s", wantNodes, ready, out)
	}

	// A workload must be schedulable...
	if _, err := docker(t, "exec", cp, "k0s", "kubectl", "run", "probe",
		"--image=registry.k8s.io/pause:3.9", "--restart=Never"); err != nil {
		t.Fatalf("run probe pod: %v", err)
	}
	// ...and the whole cluster (system pods across every node + our probe) must
	// converge to Ready.
	waitClusterPodsReady(t, cp, 120*time.Second)
}

// waitClusterPodsReady polls `kubectl get pods -A -o wide` every 2s, logging the
// full output (so all nodes' pods are visible), until every pod is Ready or
// Completed — or the timeout is hit.
func waitClusterPodsReady(t *testing.T, cp string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		out, _ := docker(t, "exec", cp, "k0s", "kubectl", "get", "pods", "-A", "-o", "wide")
		t.Logf("waiting for all cluster pods to be Ready:\n%s", strings.TrimRight(out, "\n"))
		if allPodsReady(out) {
			t.Logf("all cluster pods are Ready")
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cluster pods did not all become Ready within %s", timeout)
		}
		time.Sleep(2 * time.Second)
	}
}

// allPodsReady parses `kubectl get pods -A` output (NAMESPACE NAME READY STATUS
// ...) and reports whether every listed pod is Running with all containers ready
// (READY N/N, N>0) or has Completed. Returns false for header-only/empty output.
func allPodsReady(out string) bool {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) <= 1 {
		return false // header only or nothing scheduled yet
	}
	for _, line := range lines[1:] { // skip the header row
		f := strings.Fields(line)
		if len(f) < 4 {
			continue
		}
		ready, status := f[2], f[3]
		switch status {
		case "Completed", "Succeeded":
			continue
		case "Running":
			n, d, ok := strings.Cut(ready, "/")
			if !ok || n != d || n == "0" {
				return false
			}
		default:
			return false
		}
	}
	return true
}
