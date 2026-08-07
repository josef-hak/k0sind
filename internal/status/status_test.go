package status

import (
	"bytes"
	"strings"
	"testing"
)

// A bytes.Buffer is not an *os.File, so New treats it as non-TTY and uses the
// plain-line renderer — which is exactly what CI logs get.
func TestNonTTYRendering(t *testing.T) {
	var buf bytes.Buffer
	s := New(&buf)
	if s.tty {
		t.Fatal("bytes.Buffer must be detected as non-TTY")
	}

	s.Start("Ensuring node image")
	s.Update("... still working (10s elapsed)")
	s.Done()

	s.Start("Doomed phase")
	s.Fail()

	got := buf.String()
	for _, want := range []string{
		" ✓ Ensuring node image\n",
		"   ... still working (10s elapsed)\n",
		" ✗ Doomed phase\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull:\n%s", want, got)
		}
	}
}
