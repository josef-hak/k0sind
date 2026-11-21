package cluster

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/k0sproject/k0sind/pkg/docker"
)

// recordingRunner captures every docker invocation so tests can assert on the
// commands a Provider issues.
type recordingRunner struct {
	calls  [][]string
	output string
	err    error
}

func (r *recordingRunner) Output(args ...string) (string, error) {
	r.calls = append(r.calls, args)
	return r.output, r.err
}

func (r *recordingRunner) OutputWithStdin(_ string, args ...string) (string, error) {
	r.calls = append(r.calls, args)
	return r.output, r.err
}

func (r *recordingRunner) Stream(args ...string) error {
	r.calls = append(r.calls, args)
	return r.err
}

func testProvider() (*Provider, *recordingRunner) {
	r := &recordingRunner{}
	return newProviderWithDocker(docker.NewWithRunner(r), io.Discard), r
}

func TestPrepareNodeMakesRootShared(t *testing.T) {
	p, r := testProvider()
	if err := p.prepareNode("dev-control-plane"); err != nil {
		t.Fatalf("prepareNode: %v", err)
	}
	if len(r.calls) != 1 {
		t.Fatalf("expected 1 docker call, got %d: %v", len(r.calls), r.calls)
	}
	want := []string{"exec", "dev-control-plane", "mount", "--make-rshared", "/"}
	if !reflect.DeepEqual(r.calls[0], want) {
		t.Fatalf("call = %v, want %v", r.calls[0], want)
	}
}

func TestPrepareNodeWrapsError(t *testing.T) {
	p, r := testProvider()
	r.err = io.ErrUnexpectedEOF
	err := p.prepareNode("ci-worker")
	if err == nil {
		t.Fatal("expected an error")
	}
	// The node name belongs in the message: with several nodes the bare docker
	// error does not say which one failed.
	if !strings.Contains(err.Error(), "ci-worker") {
		t.Fatalf("error %q does not name the node", err)
	}
}

func TestExportLogsCollectsPerNodeArtifacts(t *testing.T) {
	p, r := testProvider()
	r.output = "abc123\tdev-control-plane\tio.k0sind.cluster=dev"
	dir := t.TempDir()

	got, err := p.ExportLogs("dev", dir)
	if err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}
	if got != dir {
		t.Errorf("returned %q, want %q", got, dir)
	}

	for _, f := range []string{
		"docker-info.txt",
		"dev-control-plane/inspect.json",
		"dev-control-plane/serial.log",
		"dev-control-plane/k0s-status.txt",
		"dev-control-plane/images.log",
		"dev-control-plane/kubectl-pods.txt",
		"dev-control-plane/kubectl-events.txt",
	} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s: %v", f, err)
		}
	}

	// The two collectors that keep working when the control plane does not.
	wantCalls := [][]string{
		{"logs", "dev-control-plane"},
		{"cp", "dev-control-plane:/var/log/pods", filepath.Join(dir, "dev-control-plane", "pods")},
	}
	for _, want := range wantCalls {
		found := false
		for _, call := range r.calls {
			if reflect.DeepEqual(call, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing docker call %v\ngot: %v", want, r.calls)
		}
	}
}

// A node that does not answer must still produce files: which collector failed,
// and why, is itself the diagnosis.
func TestExportLogsRecordsFailuresInTheFiles(t *testing.T) {
	p, r := testProvider()
	r.output = "abc123\tdev-control-plane\tio.k0sind.cluster=dev"
	dir := t.TempDir()
	if _, err := p.ExportLogs("dev", dir); err != nil {
		t.Fatalf("ExportLogs: %v", err)
	}

	// Same again, with every docker call failing.
	r.calls, r.err = nil, io.ErrUnexpectedEOF
	failDir := t.TempDir()
	_, err := p.ExportLogs("dev", failDir)
	if err == nil {
		t.Fatal("expected an error when the cluster cannot be listed")
	}

	status := filepath.Join(dir, "dev-control-plane", "k0s-status.txt")
	if _, err := os.Stat(status); err != nil {
		t.Fatalf("k0s-status.txt should exist even so: %v", err)
	}
}

func TestExportLogsRejectsUnknownCluster(t *testing.T) {
	p, r := testProvider()
	r.output = "" // no containers match
	if _, err := p.ExportLogs("nope", t.TempDir()); err == nil {
		t.Fatal("expected an error for a cluster with no nodes")
	}
}
