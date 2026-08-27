package docker

import (
	"reflect"
	"strings"
	"testing"
)

// fakeRunner records the args of the last call and returns canned output.
type fakeRunner struct {
	lastArgs  []string
	lastStdin string
	output    string
	err       error
}

func (f *fakeRunner) Output(args ...string) (string, error) {
	f.lastArgs = args
	return f.output, f.err
}
func (f *fakeRunner) OutputWithStdin(stdin string, args ...string) (string, error) {
	f.lastArgs = args
	f.lastStdin = stdin
	return f.output, f.err
}
func (f *fakeRunner) Stream(args ...string) error {
	f.lastArgs = args
	return f.err
}

func TestDockerArgs(t *testing.T) {
	spec := RunSpec{
		Name:       "dev-control-plane",
		Hostname:   "dev-control-plane",
		Image:      "img:1",
		Privileged: true,
		Network:    "k0sind",
		Labels:     map[string]string{"io.k0sind.role": "control-plane", "io.k0sind.cluster": "dev"},
		Volumes:    []string{"/var/lib/k0s"},
		Tmpfs:      []string{"/run"},
		Ports:      []string{"127.0.0.1::6443"},
		Cmd:        []string{"k0s", "controller", "--enable-worker"},
	}
	got := spec.DockerArgs()
	want := []string{
		"run", "-d", "--name", "dev-control-plane",
		"--hostname", "dev-control-plane",
		"--privileged",
		"--network", "k0sind",
		// labels rendered in sorted key order
		"--label", "io.k0sind.cluster=dev",
		"--label", "io.k0sind.role=control-plane",
		"-v", "/var/lib/k0s",
		"--tmpfs", "/run",
		"-p", "127.0.0.1::6443",
		"img:1",
		"k0s", "controller", "--enable-worker",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DockerArgs()\n got: %v\nwant: %v", got, want)
	}
}

func TestPSParsing(t *testing.T) {
	f := &fakeRunner{output: "abc123\tdev-control-plane\tio.k0sind.cluster=dev,io.k0sind.role=control-plane\ndef456\tdev-worker\tio.k0sind.cluster=dev,io.k0sind.role=worker"}
	c := NewWithRunner(f)
	got, err := c.PS("io.k0sind.cluster=dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(got))
	}
	if got[0].Name != "dev-control-plane" || got[0].Labels["io.k0sind.role"] != "control-plane" {
		t.Fatalf("unexpected parse: %+v", got[0])
	}
	// Filter must be passed as a label filter.
	if !strings.Contains(strings.Join(f.lastArgs, " "), "label=io.k0sind.cluster=dev") {
		t.Fatalf("filter not applied: %v", f.lastArgs)
	}
}

func TestPortParsing(t *testing.T) {
	f := &fakeRunner{output: "127.0.0.1:49153"}
	c := NewWithRunner(f)
	host, port, err := c.Port("dev-control-plane", 6443)
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" || port != "49153" {
		t.Fatalf("got %s:%s", host, port)
	}
}

func TestSaveArgs(t *testing.T) {
	f := &fakeRunner{}
	c := NewWithRunner(f)
	if err := c.Save([]string{"img:1", "img:2"}, "/tmp/out.tar"); err != nil {
		t.Fatal(err)
	}
	want := []string{"save", "-o", "/tmp/out.tar", "img:1", "img:2"}
	if !reflect.DeepEqual(f.lastArgs, want) {
		t.Fatalf("Save args\n got: %v\nwant: %v", f.lastArgs, want)
	}
}

func TestCopyToArgs(t *testing.T) {
	f := &fakeRunner{}
	c := NewWithRunner(f)
	if err := c.CopyTo("/tmp/out.tar", "dev-control-plane", "/tmp/img.tar"); err != nil {
		t.Fatal(err)
	}
	want := []string{"cp", "/tmp/out.tar", "dev-control-plane:/tmp/img.tar"}
	if !reflect.DeepEqual(f.lastArgs, want) {
		t.Fatalf("CopyTo args\n got: %v\nwant: %v", f.lastArgs, want)
	}
}

func TestApplyManifest(t *testing.T) {
	f := &fakeRunner{}
	c := NewWithRunner(f)
	if _, err := c.ApplyManifest("dev-control-plane", "kind: Namespace\n"); err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "-i", "dev-control-plane", "k0s", "kubectl", "apply", "-f", "-"}
	if !reflect.DeepEqual(f.lastArgs, want) {
		t.Fatalf("ApplyManifest args\n got: %v\nwant: %v", f.lastArgs, want)
	}
	if f.lastStdin != "kind: Namespace\n" {
		t.Fatalf("ApplyManifest stdin = %q", f.lastStdin)
	}
}

func TestNetworkEnsureIdempotent(t *testing.T) {
	// Network already exists -> no create call.
	f := &fakeRunner{output: "k0sind"}
	c := NewWithRunner(f)
	if err := c.NetworkEnsure("k0sind"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(f.lastArgs, " "), "create") {
		t.Fatalf("should not create existing network: %v", f.lastArgs)
	}
}
