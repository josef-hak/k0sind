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
	nodes := plan("dev", c, "img:1")
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
	want := []string{"k0s", "controller", "--enable-worker", "--no-taints"}
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
	nodes := plan("ci", c, "img:1")
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
	if !reflect.DeepEqual(w, []string{"k0s", "worker", "tok123"}) {
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
	spec := plan("dev", c, "img:1")[0].runSpec("")
	args := strings.Join(spec.DockerArgs(), " ")
	for _, want := range []string{"--privileged", "--network k0sind", "-v /data:/data", "-p 80:80", "img:1", "k0s controller --enable-worker --no-taints"} {
		if !strings.Contains(args, want) {
			t.Errorf("docker args missing %q\nfull: %s", want, args)
		}
	}
}
