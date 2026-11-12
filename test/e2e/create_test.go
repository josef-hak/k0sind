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
	t.Parallel()
	runScenario(t, "ci-single", nil, 1, 120*time.Second)
}

func TestCreateOneControlPlaneTwoWorkers(t *testing.T) {
	t.Parallel()
	runScenario(t, "ci-multi", []string{"--config", "../../examples/multi-node.yaml"}, 3, 180*time.Second)
}

func TestCreateWithCalico(t *testing.T) {
	t.Parallel()
	runScenario(t, "ci-calico", []string{"--k0s-config", "../../examples/k0s-calico.yaml"}, 1, 120*time.Second)
	// Verify Calico is the active network provider (not the default kube-router).
	assertPodExists(t, "ci-calico-control-plane", "kube-system", "calico-node")
}

func TestCreateWithOpenEBS(t *testing.T) {
	t.Parallel()
	runScenario(t, "ci-openebs", []string{"--k0s-config", "../../examples/k0s-openebs.yaml"}, 1, 180*time.Second)
	// Verify the OpenEBS helm extension deployed pods.
	assertPodExists(t, "ci-openebs-control-plane", "openebs", "openebs")
}

// assertPodExists checks that at least one pod whose name contains substr
// exists in the given namespace.
func assertPodExists(t *testing.T, cp, namespace, substr string) {
	t.Helper()
	out, err := docker(t, "exec", cp, "k0s", "kubectl", "get", "pods",
		"-n", namespace, "--no-headers")
	if err != nil {
		t.Fatalf("get pods -n %s: %v\n%s", namespace, err, out)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, substr) {
			t.Logf("%s pod %q present in namespace %q", markOK, substr, namespace)
			return
		}
	}
	t.Fatalf("%s no pod containing %q found in namespace %q:\n%s", markFail, substr, namespace, out)
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
		t.Fatalf("%s create cluster: %v", markFail, err)
	}
	t.Logf("%s cluster %q created", markOK, name)

	// Count Ready nodes reported by k0s.
	out, err := docker(t, "exec", cp, "k0s", "kubectl", "get", "nodes", "--no-headers")
	if err != nil {
		t.Fatalf("%s get nodes: %v", markFail, err)
	}
	ready := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if f := strings.Fields(line); len(f) >= 2 && f[1] == "Ready" {
			ready++
		}
	}
	if ready != wantNodes {
		t.Fatalf("%s expected %d Ready nodes, got %d\n%s", markFail, wantNodes, ready, out)
	}
	t.Logf("%s %d/%d nodes Ready", markOK, ready, wantNodes)

	// A workload must be schedulable...
	if _, err := docker(t, "exec", cp, "k0s", "kubectl", "run", "probe",
		"--image=registry.k8s.io/pause:3.9", "--restart=Never"); err != nil {
		t.Fatalf("%s run probe pod: %v", markFail, err)
	}
	// ...and the whole cluster (system pods across every node + our probe) must
	// converge to Ready.
	waitClusterPodsReady(t, cp, 120*time.Second)
}

// tickInterval is how often to log a heartbeat while nothing moves.
const tickInterval = 20 * time.Second

// waitClusterPodsReady polls every 2s until every pod is Ready or Completed.
// It logs the full table on the first poll and at the end; in between only the
// pods that moved, plus a heartbeat when nothing does. A cluster coming up then
// tells its own story instead of scrolling the same dozen settled pods by.
func waitClusterPodsReady(t *testing.T, cp string, timeout time.Duration) {
	t.Helper()
	start := time.Now()
	deadline := start.Add(timeout)
	var prev []pod
	lastTick := start

	for {
		pods, ready, done, err := clusterPods(t, cp)
		if err != nil {
			t.Fatalf("%s %v", markFail, err)
		}
		switch {
		case len(pods) > 0 && ready+done == len(pods):
			t.Logf("%s all %d pods ready in %s\n%s", markOK, len(pods),
				time.Since(start).Round(time.Second), podTable(pods))
			return
		case prev == nil:
			t.Logf("%s waiting for pods to be Ready — %d/%d ready\n%s",
				markWait, ready, len(pods), podTable(pods))
			prev, lastTick = pods, time.Now()
		default:
			if changed := podChanges(pods, prev); len(changed) > 0 {
				t.Logf("%s %d/%d ready (%s)\n%s", markWait, ready, len(pods),
					time.Since(start).Round(time.Second), podTable(changed))
				prev, lastTick = pods, time.Now()
			} else if time.Since(lastTick) >= tickInterval {
				t.Logf("%s unchanged — %d/%d ready (%s)", markWait, ready, len(pods),
					time.Since(start).Round(time.Second))
				lastTick = time.Now()
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s cluster pods did not all become Ready within %s\n%s",
				markFail, timeout, podTable(pods))
		}
		time.Sleep(2 * time.Second)
	}
}
