package ui

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// The big banner is 75 cols wide. We need ~78 cols of terminal to render it
// without wrapping (a little breathing room).
const bigBannerMinWidth = 78

const bigBanner = `
██████╗ ███████╗████████╗████████╗ █████╗ ████████╗███████╗ ██████╗██╗  ██╗
██╔══██╗██╔════╝╚══██╔══╝╚══██╔══╝██╔══██╗╚══██╔══╝██╔════╝██╔════╝██║  ██║
██████╔╝█████╗     ██║      ██║   ███████║   ██║   █████╗  ██║     ███████║
██╔══██╗██╔══╝     ██║      ██║   ██╔══██║   ██║   ██╔══╝  ██║     ██╔══██║
██████╔╝███████╗   ██║      ██║   ██║  ██║   ██║   ███████╗╚██████╗██║  ██║
╚═════╝ ╚══════╝   ╚═╝      ╚═╝   ╚═╝  ╚═╝   ╚═╝   ╚══════╝ ╚═════╝╚═╝  ╚═╝
`

// PrintBanner renders the startup banner. Falls back to a single-line
// wordmark when the terminal is narrower than the big banner needs.
func PrintBanner() { fmt.Print(BannerText(TermWidth())) }

// BannerText returns the banner as a string for callers that want to
// pre-populate a scrollback buffer (the TUI program) rather than print.
func BannerText(width int) string {
	if width == 0 {
		width = bigBannerMinWidth
	}
	if width >= bigBannerMinWidth {
		return BoldCyan + bigBanner + Reset +
			Dimmed("              build your own coding agent") + "\n" +
			Dimmed("              type a message · /help for commands · ctrl-d to exit") + "\n\n"
	}
	return "\n" + Cyan("  BETTATECH") + Dimmed("  ·  build your own coding agent") + "\n" +
		Dimmed("  type a message or /help") + "\n\n"
}

// TermWidth returns stdout's terminal width, or 0 when stdout isn't a TTY.
func TermWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return w
}
