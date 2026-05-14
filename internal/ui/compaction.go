package ui

import (
	"fmt"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

// PrintCompaction writes a before/after diff to stdout. Used by verbose mode
// when a compaction strategy shortens the history.
func PrintCompaction(before, after []api.Message) {
	fmt.Println(Dimmed("─── compaction ───"))
	fmt.Printf("BEFORE (%d messages):\n%s", len(before), api.RenderTranscript(before))
	fmt.Println(Dimmed("─── after ───"))
	fmt.Printf("AFTER (%d messages):\n%s", len(after), api.RenderTranscript(after))
	fmt.Println(Dimmed("──────────────"))
}
