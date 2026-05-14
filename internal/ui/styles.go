// Package ui holds everything the user sees: the banner, the loading
// spinner, the chat-input TUI, the confirmation prompt, and the styling
// helpers used by the rest of the harness when printing to stdout.
package ui

// ANSI escape sequences for terminal styling. These are exported so the
// command handlers (in main) can apply the same styling vocabulary.
const (
	BoldCyan = "\033[1;36m"
	Dim      = "\033[2m"
	Yellow   = "\033[33m"
	Reset    = "\033[0m"
)

// Dimmed wraps s with the dim ANSI escape and reset.
func Dimmed(s string) string { return Dim + s + Reset }

// Cyan wraps s with the bold-cyan ANSI escape and reset.
func Cyan(s string) string { return BoldCyan + s + Reset }
