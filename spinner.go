package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

type spinner struct {
	stop chan struct{}
	done chan struct{}
}

// startSpinner begins a braille spinner on stdout with the given label.
// No-op when stdout isn't a TTY (piped, redirected). Call Stop() to clear.
func startSpinner(label string) *spinner {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return &spinner{}
	}
	s := &spinner{
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
					ansiBoldCyan, spinnerFrames[i], ansiReset,
					ansiDim, label, ansiReset)
				i = (i + 1) % len(spinnerFrames)
			}
		}
	}()
	return s
}

// Stop must be called exactly once per started spinner.
func (s *spinner) Stop() {
	if s.stop == nil {
		return // no-op spinner (non-TTY)
	}
	close(s.stop)
	<-s.done
}
