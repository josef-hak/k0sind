// Package status renders kind-style progress: an animated spinner per phase on
// a TTY that resolves to a green ✓ (or red ✗) when the phase ends, and plain
// line output when stderr is not a terminal (e.g. CI, pipes).
package status

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const (
	colGreen = "\x1b[32m"
	colRed   = "\x1b[31m"
	colCyan  = "\x1b[36m"
	colReset = "\x1b[0m"
	clearEOL = "\x1b[K"
)

// spinnerFrames are the braille frames kind uses.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Status drives per-phase progress output to w.
type Status struct {
	w   io.Writer
	tty bool

	mu    sync.Mutex
	title string // phase name, shown with the final ✓/✗
	msg   string // current spinner label (starts as title, changed by Update)
	stop  chan struct{}
	wg    sync.WaitGroup
}

// New returns a Status writing to w, auto-detecting whether w is a terminal.
func New(w io.Writer) *Status {
	tty := false
	if f, ok := w.(*os.File); ok {
		tty = term.IsTerminal(int(f.Fd()))
	}
	return &Status{w: w, tty: tty}
}

// Start begins a phase with the given message. On a TTY it launches the spinner
// animation; otherwise it stays silent until End.
func (s *Status) Start(msg string) {
	s.mu.Lock()
	s.title = msg
	s.msg = msg
	s.mu.Unlock()
	if !s.tty {
		return
	}
	s.stop = make(chan struct{})
	s.wg.Add(1)
	go s.animate()
}

// Update changes the message shown for the current phase. It satisfies the
// func(string, ...any) shape used by the wait helpers. On a TTY it updates the
// spinner label; otherwise it prints a throttled progress line.
func (s *Status) Update(format string, a ...any) {
	msg := strings.TrimSpace(fmt.Sprintf(format, a...))
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
	if !s.tty {
		fmt.Fprintf(s.w, "   %s\n", msg)
	}
}

// Done finalizes the current phase as successful (green ✓).
func (s *Status) Done() { s.end(true) }

// Fail finalizes the current phase as failed (red ✗).
func (s *Status) Fail() { s.end(false) }

func (s *Status) end(ok bool) {
	if s.tty && s.stop != nil {
		close(s.stop)
		s.wg.Wait()
		s.stop = nil
	}
	s.mu.Lock()
	msg := s.title
	s.mu.Unlock()

	mark, color := "✓", colGreen
	if !ok {
		mark, color = "✗", colRed
	}
	if s.tty {
		fmt.Fprintf(s.w, "\r%s %s%s%s %s\n", clearEOL, color, mark, colReset, msg)
	} else {
		fmt.Fprintf(s.w, " %s %s\n", mark, msg)
	}
}

func (s *Status) animate() {
	defer s.wg.Done()
	t := time.NewTicker(90 * time.Millisecond)
	defer t.Stop()
	i := 0
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			frame := spinnerFrames[i%len(spinnerFrames)]
			fmt.Fprintf(s.w, "\r%s %s%s%s %s", clearEOL, colCyan, frame, colReset, msg)
			i++
		}
	}
}
