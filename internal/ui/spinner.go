package ui

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

type Spinner struct {
	stop chan struct{}
	done chan struct{}
}

// StartSpinner begins a braille spinner on stdout with the given label.
// No-op when stdout isn't a TTY (piped, redirected). Call Stop() to clear.
func StartSpinner(label string) *Spinner {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return &Spinner{}
	}
	s := &Spinner{
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Print("\r\033[K") // clear the current line
				return
			case <-ticker.C:
				fmt.Printf("\r%s%c%s %s%s%s",
					BoldCyan, spinnerFrames[i], Reset,
					Dim, label, Reset)
				i = (i + 1) % len(spinnerFrames)
			}
		}
	}()
	return s
}

// Stop must be called exactly once per started spinner.
func (s *Spinner) Stop() {
	if s.stop == nil {
		return // no-op spinner (non-TTY)
	}
	close(s.stop)
	<-s.done
}
