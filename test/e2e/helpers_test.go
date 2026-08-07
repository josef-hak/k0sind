//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"testing"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// run executes a command, streaming output to the test log.
func run(t *testing.T, bin string, args ...string) error {
	t.Helper()
	t.Logf("$ %s %v", bin, args)
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	return err
}

// docker runs a docker command and returns its combined output.
func docker(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	return string(out), err
}
