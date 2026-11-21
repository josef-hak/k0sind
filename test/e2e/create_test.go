//go:build e2e

// Package e2e exercises k0sind against a real docker daemon. It is guarded by
// the `e2e` build tag so `go test ./...` (the unit job) never spawns containers.
// Run with: go test -tags e2e ./test/e2e/... -timeout 20m
package e2e

import (
	"os"
	"path/filepath"
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
	assertKubeletRootDir(t, "ci-single-control-plane", "/var/lib/k0s/kubelet", "/var/lib/kubelet")
}

// assertKubeletRootDir checks where the kubelet keeps its state. Charts that
// have to name it - velero's node-agent, CSI drivers, datadog's system-probe -
// silently misbehave when it is not where they expect.
func assertKubeletRootDir(t *testing.T, cp, want, notWant string) {
	t.Helper()
	if out, err := docker(t, "exec", cp, "ls", "-d", want+"/pods"); err != nil {
		t.Fatalf("%s kubelet root is not at %s: %v\n%s", markFail, want, err, out)
	}
	// The directory itself is no evidence: the kubelet keeps its device-plugin
	// sockets at /var/lib/kubelet whatever its root is. pods/ is the marker.
	if out, err := docker(t, "exec", cp, "ls", "-d", notWant+"/pods"); err == nil {
		t.Fatalf("%s kubelet state also at %s:\n%s", markFail, notWant, out)
	}
	t.Logf("%s kubelet root of %s is %s", markOK, cp, want)
}

// TestKubeletRootDirOverride is not parallel: it creates its own cluster to
// check the flag, the way a chart set ported from kind would need it.
func TestKubeletRootDirOverride(t *testing.T) {
	bin := k0sindBin(t)
	const name = "ci-kroot"
	t.Cleanup(func() { _ = run(t, bin, "delete", "cluster", "--name", name) })
	if err := run(t, bin, "create", "cluster", "--name", name,
		"--kubelet-root-dir", "/var/lib/kubelet", "--wait", "120s"); err != nil {
		t.Fatalf("%s create cluster: %v", markFail, err)
	}
	assertKubeletRootDir(t, name+"-control-plane", "/var/lib/kubelet", "/var/lib/k0s/kubelet")
}

// TestExportLogs checks the dump on a cluster that has been stopped, which is
// the state it exists for: the control plane answers nothing, and what is left
// must still reach the disk.
func TestExportLogs(t *testing.T) {
	bin := k0sindBin(t)
	const name = "ci-logs"
	cp := name + "-control-plane"
	t.Cleanup(func() { _ = run(t, bin, "delete", "cluster", "--name", name) })

	if err := run(t, bin, "create", "cluster", "--name", name, "--wait", "120s"); err != nil {
		t.Fatalf("%s create cluster: %v", markFail, err)
	}
	if out, err := docker(t, "stop", cp); err != nil {
		t.Fatalf("%s stop %s: %v\n%s", markFail, cp, err, out)
	}

	dir := t.TempDir()
	if err := run(t, bin, "export", "logs", "--name", name, dir); err != nil {
		t.Fatalf("%s export logs: %v", markFail, err)
	}

	// serial.log and the pod logs come from the container and its volume, so a
	// dead node does not take them with it; the kubectl dumps are allowed to
	// hold nothing but the error that explains why.
	for _, f := range []string{"docker-info.txt", cp + "/serial.log", cp + "/inspect.json"} {
		info, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			t.Errorf("%s missing %s: %v", markFail, f, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s %s is empty", markFail, f)
		}
	}
	pods, err := filepath.Glob(filepath.Join(dir, cp, "pods", "*", "*", "*.log"))
	if err != nil || len(pods) == 0 {
		t.Fatalf("%s no pod logs collected (%v)", markFail, err)
	}
	t.Logf("%s exported %d pod log(s) from a stopped node", markOK, len(pods))
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

// TestKubeconfigFlag is not parallel: it sets KUBECONFIG for the k0sind calls
// and checks that file is left alone.
//
// This is the shape that bit a caller managing two clusters: KUBECONFIG pointed
// at the first cluster's file, creating the second merged into it and switched
// its current-context, so every later kubectl talked to the wrong cluster.
func TestKubeconfigFlag(t *testing.T) {
	bin := k0sindBin(t)
	const name = "ci-kcfg"
	dir := t.TempDir()
	inUse := filepath.Join(dir, "in-use.yaml")
	target := filepath.Join(dir, "target.yaml")
	env := []string{"KUBECONFIG=" + inUse}

	// Something unrelated already lives in the kubeconfig k0sind would pick.
	if err := os.WriteFile(inUse, []byte(unrelatedKubeconfig), 0o600); err != nil {
		t.Fatalf("seed kubeconfig: %v", err)
	}

	t.Cleanup(func() {
		_ = runEnv(t, bin, env, "delete", "cluster", "--name", name, "--kubeconfig", target)
	})

	if err := runEnv(t, bin, env, "create", "cluster", "--name", name,
		"--kubeconfig", target, "--wait", "120s"); err != nil {
		t.Fatalf("%s create cluster: %v", markFail, err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("%s --kubeconfig file not written: %v", markFail, err)
	}
	if !strings.Contains(string(got), "k0sind-"+name) {
		t.Fatalf("%s context missing from the --kubeconfig file:\n%s", markFail, got)
	}

	after, err := os.ReadFile(inUse)
	if err != nil {
		t.Fatalf("read seeded kubeconfig: %v", err)
	}
	if string(after) != unrelatedKubeconfig {
		t.Fatalf("%s the kubeconfig from the environment was modified:\n%s", markFail, after)
	}
	t.Logf("%s cluster %q written to --kubeconfig, $KUBECONFIG untouched", markOK, name)
}

// unrelatedKubeconfig stands in for a kubeconfig the caller is already using.
const unrelatedKubeconfig = `apiVersion: v1
kind: Config
clusters: []
contexts: []
current-context: somewhere-else
preferences: {}
users: []
`

// TestNetworkOverride is not parallel: it drives k0sind through the process
// environment and checks a bridge it creates itself.
func TestNetworkOverride(t *testing.T) {
	bin := k0sindBin(t)
	const name = "ci-net"
	const network = "k0sind-e2e-net"
	env := []string{"KIND_EXPERIMENTAL_DOCKER_NETWORK=" + network}

	t.Cleanup(func() {
		_ = runEnv(t, bin, env, "delete", "cluster", "--name", name)
		// A network named by the caller is theirs: k0sind must leave it.
		if out, err := docker(t, "network", "inspect", network, "--format", "{{.Name}}"); err != nil {
			t.Errorf("%s k0sind removed the caller's network %q: %v\n%s", markFail, network, err, out)
		}
		_, _ = docker(t, "network", "rm", network)
	})

	if out, err := docker(t, "network", "create", network); err != nil {
		t.Fatalf("%s create network: %v\n%s", markFail, err, out)
	}
	if err := runEnv(t, bin, env, "create", "cluster", "--name", name, "--wait", "120s"); err != nil {
		t.Fatalf("%s create cluster: %v", markFail, err)
	}

	out, err := docker(t, "inspect", name+"-control-plane", "--format", "{{json .NetworkSettings.Networks}}")
	if err != nil {
		t.Fatalf("%s inspect node: %v\n%s", markFail, err, out)
	}
	if !strings.Contains(out, network) {
		t.Fatalf("%s node is not on %q:\n%s", markFail, network, out)
	}
	if strings.Contains(out, `"kind"`) {
		t.Fatalf("%s node is still on the default bridge:\n%s", markFail, out)
	}
	t.Logf("%s node of %q is on the overridden bridge %q", markOK, name, network)
}

func TestKubeletConfigPatch(t *testing.T) {
	t.Parallel()
	runScenario(t, "ci-kubelet", []string{"--config", "../../examples/kubelet-config.yaml"}, 1, 120*time.Second)
	assertKubeletEviction(t, "ci-kubelet-control-plane")
}

// assertKubeletEviction reads the kubelet's live configuration and checks the
// eviction thresholds from the kind config's KubeletConfiguration patch landed.
// configz is the kubelet's own view, so this proves the whole path: kind patch
// -> k0s worker profile -> running kubelet.
func assertKubeletEviction(t *testing.T, cp string) {
	t.Helper()
	out, err := docker(t, "exec", cp, "k0s", "kubectl", "get", "--raw",
		"/api/v1/nodes/"+cp+"/proxy/configz")
	if err != nil {
		t.Fatalf("%s read kubelet configz: %v\n%s", markFail, err, out)
	}
	for _, want := range []string{`"imagefs.available":"1Gi"`, `"nodefs.available":"1Gi"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s kubelet config is missing %s:\n%s", markFail, want, out)
		}
	}
	t.Logf("%s kubelet of %s uses the patched eviction thresholds", markOK, cp)
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
		case strings.Contains(out, "Evicted"):
			// Terminal, and nothing to do with mount propagation: the node is
			// under resource pressure. On a host with a full-ish disk the
			// kubelet's default imagefs threshold (15% of the *host* fs) trips
			// and it evicts whatever it can.
			t.Fatalf("%s probe evicted - the node is under pressure, not a propagation failure:\n%s",
				markFail, out)
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
