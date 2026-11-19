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
	assertKubeletRootDir(t, "ci-single-control-plane")
}

// assertKubeletRootDir checks the kubelet keeps its state at the upstream path.
// Charts that have to name it (velero's node-agent, CSI drivers, datadog's
// system-probe) assume that path, and silently misbehave when it moves.
func assertKubeletRootDir(t *testing.T, cp string) {
	t.Helper()
	if out, err := docker(t, "exec", cp, "ls", "-d", "/var/lib/kubelet/pods"); err != nil {
		t.Fatalf("%s kubelet root is not at /var/lib/kubelet: %v\n%s", markFail, err, out)
	}
	if out, err := docker(t, "exec", cp, "ls", "-d", "/var/lib/k0s/kubelet"); err == nil {
		t.Fatalf("%s kubelet state still under the k0s data dir:\n%s", markFail, out)
	}
	t.Logf("%s kubelet root of %s is /var/lib/kubelet", markOK, cp)
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

func TestMountPropagation(t *testing.T) {
	t.Parallel()
	runScenario(t, "ci-mount", nil, 1, 120*time.Second)
	cp := "ci-mount-control-plane"
	assertRootMountShared(t, cp)
	assertPropagationWorkloadRuns(t, cp)
}

// assertRootMountShared checks the node's root mount is shared. The kubelet
// refuses pods that hostPath-mount with propagation unless it is.
func assertRootMountShared(t *testing.T, cp string) {
	t.Helper()
	out, err := docker(t, "exec", cp, "sh", "-c", "grep -m1 ' / / ' /proc/self/mountinfo")
	if err != nil {
		t.Fatalf("%s read mountinfo: %v\n%s", markFail, err, out)
	}
	// The optional fields of a mountinfo line carry "shared:N" for a shared mount.
	if !strings.Contains(out, "shared:") {
		t.Fatalf("%s root mount of %s is not shared:\n%s", markFail, cp, out)
	}
	t.Logf("%s root mount of %s is shared", markOK, cp)
}

// propagationProbe hostPath-mounts with HostToContainer propagation, the shape
// CNI agents and node agents use (istio-cni, velero, node-exporter). Without a
// shared root mount the kubelet fails it with "path ... is mounted on ... but
// it is not a shared or slave mount" and the pod never starts.
const propagationProbe = `
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: propagation-probe
  namespace: default
spec:
  selector:
    matchLabels:
      app: propagation-probe
  template:
    metadata:
      labels:
        app: propagation-probe
    spec:
      tolerations:
        - operator: Exists
      containers:
        - name: probe
          image: registry.k8s.io/pause:3.9
          volumeMounts:
            - name: netns
              mountPath: /host/netns
              mountPropagation: HostToContainer
      volumes:
        - name: netns
          hostPath:
            path: /var/run/netns
            type: DirectoryOrCreate
`

func assertPropagationWorkloadRuns(t *testing.T, cp string) {
	t.Helper()
	out, err := dockerStdin(t, propagationProbe, "exec", "-i", cp,
		"k0s", "kubectl", "apply", "-f", "-")
	if err != nil {
		t.Fatalf("%s apply propagation probe: %v\n%s", markFail, err, out)
	}
	deadline := time.Now().Add(120 * time.Second)
	for {
		out, err := docker(t, "exec", cp, "k0s", "kubectl", "get", "pods",
			"-l", "app=propagation-probe", "--no-headers")
		switch {
		case err != nil:
			t.Fatalf("%s get probe pods: %v\n%s", markFail, err, out)
		case strings.Contains(out, "Running"):
			t.Logf("%s propagation probe running: %s", markOK, strings.TrimSpace(out))
			return
		case strings.Contains(out, "CreateContainerError"):
			t.Fatalf("%s propagation probe rejected by the kubelet:\n%s", markFail, out)
		case time.Now().After(deadline):
			t.Fatalf("%s propagation probe did not start in time:\n%s", markFail, out)
		}
		time.Sleep(2 * time.Second)
	}
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
