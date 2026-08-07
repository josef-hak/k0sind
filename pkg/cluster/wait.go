package cluster

import (
	"fmt"
	"strings"
	"time"

	"github.com/k0sproject/k0sind/pkg/docker"
)

// pollInterval is how often readiness checks re-run.
const pollInterval = 2 * time.Second

// poll invokes fn until it returns true, an unrecoverable state, or timeout.
func poll(timeout time.Duration, fn func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ok, err := fn()
		if ok {
			return nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("timed out after %s: %w", timeout, lastErr)
			}
			return fmt.Errorf("timed out after %s", timeout)
		}
		time.Sleep(pollInterval)
	}
}

// heartbeat is how often the wait helpers emit a "still waiting" progress line.
const heartbeat = 10 * time.Second

// waitK0sRunning blocks until `k0s status` reports the node is up, emitting a
// progress line via logf roughly every heartbeat while it waits.
func waitK0sRunning(d *docker.Client, container string, timeout time.Duration, logf func(string, ...any)) error {
	start := time.Now()
	last := start
	return poll(timeout, func() (bool, error) {
		out, err := d.Exec(container, "k0s", "status")
		up := err == nil && (strings.Contains(out, "Version:") || strings.Contains(strings.ToLower(out), "running"))
		if !up && logf != nil && time.Since(last) >= heartbeat {
			logf("   ... still starting k0s on %s (%s elapsed)", container, time.Since(start).Round(time.Second))
			last = time.Now()
		}
		return up, err
	})
}

// waitNodesReady blocks until at least want nodes report Ready via kubectl,
// emitting the running Ready count via logf roughly every heartbeat.
func waitNodesReady(d *docker.Client, container string, want int, timeout time.Duration, logf func(string, ...any)) error {
	start := time.Now()
	last := start
	return poll(timeout, func() (bool, error) {
		out, err := d.Exec(container, "k0s", "kubectl", "get", "nodes", "--no-headers")
		if err != nil {
			return false, err
		}
		ready := 0
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "Ready" {
				ready++
			}
		}
		if ready < want && logf != nil && time.Since(last) >= heartbeat {
			logf("   ... %d/%d node(s) Ready (%s elapsed)", ready, want, time.Since(start).Round(time.Second))
			last = time.Now()
		}
		return ready >= want, nil
	})
}
