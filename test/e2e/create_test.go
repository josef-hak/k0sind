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

	// A workload must be schedulable.
	if _, err := docker(t, "exec", cp, "k0s", "kubectl", "run", "probe",
		"--image=registry.k8s.io/pause:3.9", "--restart=Never"); err != nil {
		t.Fatalf("run probe pod: %v", err)
	}
}
