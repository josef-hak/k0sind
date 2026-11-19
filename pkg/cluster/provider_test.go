package cluster

import (
	"io"
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
