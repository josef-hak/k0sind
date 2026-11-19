package cluster

import (
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

func TestPlanSingleNode(t *testing.T) {
	c := &v1alpha4.Cluster{
		Nodes:      []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}},
		Networking: v1alpha4.Networking{APIServerAddress: "127.0.0.1"},
	}
	nodes := plan("dev", c, "img:1", "")
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.Name != "dev-control-plane" {
		t.Fatalf("unexpected name %q", n.Name)
	}
	if !n.single {
		t.Fatal("expected single=true for one-node cluster")
	}
	got := n.k0sCommand("")
	want := []string{"k0s", "controller", "--enable-worker", "--no-taints",
		"--kubelet-root-dir", KubeletRootDir}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cmd = %v, want %v", got, want)
	}
	// API server port must be mapped on the control-plane.
	if len(n.Ports) != 1 || !strings.HasSuffix(n.Ports[0], ":6443") {
		t.Fatalf("expected an api port mapping, got %v", n.Ports)
	}
}

func TestPlanControlPlaneWithWorkers(t *testing.T) {
	c := &v1alpha4.Cluster{
		Nodes: []v1alpha4.Node{
			{Role: v1alpha4.WorkerRole},
			{Role: v1alpha4.ControlPlaneRole},
			{Role: v1alpha4.WorkerRole},
		},
		Networking: v1alpha4.Networking{APIServerAddress: "127.0.0.1"},
	}
	nodes := plan("ci", c, "img:1", "")
	if len(nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(nodes))
	}
	// Control-plane must be first regardless of config order.
	if !nodes[0].isControlPlane() {
		t.Fatalf("expected control-plane first, got %q", nodes[0].Name)
	}
	names := []string{nodes[0].Name, nodes[1].Name, nodes[2].Name}
	want := []string{"ci-control-plane", "ci-worker", "ci-worker2"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	// Control-plane keeps its taint when workers exist (no --no-taints).
	cp := nodes[0].k0sCommand("")
	if reflect.DeepEqual(cp[len(cp)-1], "--no-taints") {
		t.Fatalf("control-plane should keep taints when workers exist: %v", cp)
	}
	// Workers join via token.
	w := nodes[1].k0sCommand("tok123")
	if !reflect.DeepEqual(w, []string{"k0s", "worker", "tok123", "--kubelet-root-dir", KubeletRootDir}) {
		t.Fatalf("worker cmd = %v", w)
	}
}

func TestPortArg(t *testing.T) {
	cases := []struct {
		in   v1alpha4.PortMapping
		want string
	}{
		{v1alpha4.PortMapping{ContainerPort: 80, HostPort: 8080}, "8080:80"},
		{v1alpha4.PortMapping{ContainerPort: 80, HostPort: 80, ListenAddress: "127.0.0.1"}, "127.0.0.1:80:80"},
		{v1alpha4.PortMapping{ContainerPort: 53, HostPort: 53, Protocol: "UDP"}, "53:53/udp"},
		{v1alpha4.PortMapping{ContainerPort: 443}, ":443"},
	}
	for _, c := range cases {
		if got := portArg(c.in); got != c.want {
			t.Errorf("portArg(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestMountArg(t *testing.T) {
	if got := mountArg(v1alpha4.Mount{HostPath: "/h", ContainerPath: "/c"}); got != "/h:/c" {
		t.Errorf("got %q", got)
	}
	if got := mountArg(v1alpha4.Mount{HostPath: "/h", ContainerPath: "/c", Readonly: true}); got != "/h:/c:ro" {
		t.Errorf("got %q", got)
	}
}

func TestRunSpecExtraMountsAndPorts(t *testing.T) {
	c := &v1alpha4.Cluster{
		Nodes: []v1alpha4.Node{{
			Role:              v1alpha4.ControlPlaneRole,
			ExtraMounts:       []v1alpha4.Mount{{HostPath: "/data", ContainerPath: "/data"}},
			ExtraPortMappings: []v1alpha4.PortMapping{{ContainerPort: 80, HostPort: 80}},
		}},
		Networking: v1alpha4.Networking{APIServerAddress: "127.0.0.1"},
	}
	spec := plan("dev", c, "img:1", "")[0].runSpec("")
	args := strings.Join(spec.DockerArgs(), " ")
	for _, want := range []string{"--privileged", "--network kind", "-v /data:/data", "-p 80:80", "img:1", "k0s controller --enable-worker --no-taints"} {
		if !strings.Contains(args, want) {
			t.Errorf("docker args missing %q\nfull: %s", want, args)
		}
	}
}

func TestDefaultNodeImage(t *testing.T) {
	cases := []struct{ env, want string }{
		{"", DefaultImage},
		{"v1.33.0-k0s.0", "docker.io/k0sproject/k0s:v1.33.0-k0s.0"},
		{"v1.33.0+k0s.0", "docker.io/k0sproject/k0s:v1.33.0-k0s.0"}, // release notation
		{"1.33.0+k0s.0", "docker.io/k0sproject/k0s:v1.33.0-k0s.0"},
		{" v1.33.0-k0s.0 ", "docker.io/k0sproject/k0s:v1.33.0-k0s.0"},
	}
	for _, c := range cases {
		t.Setenv(K0sVersionEnv, c.env)
		if got := DefaultNodeImage(); got != c.want {
			t.Errorf("K0S_VERSION=%q: got %q, want %q", c.env, got, c.want)
		}
	}
}

// Charts that have to name the kubelet root (velero's node-agent, CSI drivers,
// datadog's system-probe) assume the upstream path, so every node must put the
// kubelet there rather than under the k0s data dir.
func TestKubeletRootDirIsUpstreamDefault(t *testing.T) {
	c := &v1alpha4.Cluster{
		Nodes: []v1alpha4.Node{
			{Role: v1alpha4.ControlPlaneRole},
			{Role: v1alpha4.WorkerRole},
		},
		Networking: v1alpha4.Networking{APIServerAddress: "127.0.0.1"},
	}
	if KubeletRootDir != "/var/lib/kubelet" {
		t.Fatalf("KubeletRootDir = %q, want the upstream default", KubeletRootDir)
	}
	for _, n := range plan("dev", c, "img:1", "") {
		cmd := strings.Join(n.k0sCommand("tok"), " ")
		if !strings.Contains(cmd, "--kubelet-root-dir "+KubeletRootDir) {
			t.Errorf("%s: cmd %q does not set the kubelet root", n.Name, cmd)
		}
		// The kubelet state belongs on a volume, like the k0s data dir, not on
		// the container's overlay filesystem.
		var found bool
		for _, v := range n.Volumes {
			if v == KubeletRootDir {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: %q missing from volumes %v", n.Name, KubeletRootDir, n.Volumes)
		}
	}
}

// kind lets the bridge be overridden with KIND_EXPERIMENTAL_DOCKER_NETWORK;
// k0sind reads the same variable so existing scripts port over unchanged.
func TestNetworkNameHonoursKindEnv(t *testing.T) {
	if got := NetworkName(); got != Network {
		t.Fatalf("NetworkName() = %q, want the default %q", got, Network)
	}
	if NetworkOverridden() {
		t.Fatal("NetworkOverridden() is true without the variable set")
	}

	t.Setenv(NetworkEnv, "  custom-net  ")
	if got := NetworkName(); got != "custom-net" {
		t.Fatalf("NetworkName() = %q, want the override", got)
	}
	if !NetworkOverridden() {
		t.Fatal("NetworkOverridden() is false with the variable set")
	}

	// An empty value is not an override.
	t.Setenv(NetworkEnv, "")
	if got := NetworkName(); got != Network {
		t.Fatalf("NetworkName() = %q, want the default for an empty value", got)
	}
}

func TestRunSpecUsesOverriddenNetwork(t *testing.T) {
	t.Setenv(NetworkEnv, "custom-net")
	c := &v1alpha4.Cluster{
		Nodes:      []v1alpha4.Node{{Role: v1alpha4.ControlPlaneRole}},
		Networking: v1alpha4.Networking{APIServerAddress: "127.0.0.1"},
	}
	args := strings.Join(plan("dev", c, "img:1", "")[0].runSpec("").DockerArgs(), " ")
	if !strings.Contains(args, "--network custom-net") {
		t.Fatalf("docker args do not use the overridden network: %s", args)
	}
}
