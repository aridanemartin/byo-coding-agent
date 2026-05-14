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
func PrintBanner() {
	if TermWidth() >= bigBannerMinWidth {
		fmt.Print(BoldCyan, bigBanner, Reset)
		fmt.Println(Dimmed("              experimental harness · powered by claude"))
		fmt.Println(Dimmed("              type a message · /help for commands · ctrl-d to exit"))
	} else {
		fmt.Println()
		fmt.Println(Cyan("  BETTATECH") + Dimmed("  ·  experimental harness"))
		fmt.Println(Dimmed("  type a message or /help"))
	}
	fmt.Println()
}

// TermWidth returns stdout's terminal width, or 0 when stdout isn't a TTY.
func TermWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return w
}
