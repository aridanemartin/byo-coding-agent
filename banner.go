package main

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

const (
	ansiBoldCyan = "\033[1;36m"
	ansiDim      = "\033[2m"
	ansiReset    = "\033[0m"
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

func printBanner() {
	if termWidth() >= bigBannerMinWidth {
		fmt.Print(ansiBoldCyan, bigBanner, ansiReset)
		fmt.Println(ansiDim + "              experimental harness · powered by claude" + ansiReset)
		fmt.Println(ansiDim + "              type a message · /help for commands · ctrl-d to exit" + ansiReset)
	} else {
		fmt.Println()
		fmt.Println(ansiBoldCyan + "  BETTATECH" + ansiReset + ansiDim + "  ·  experimental harness" + ansiReset)
		fmt.Println(ansiDim + "  type a message or /help" + ansiReset)
	}
	fmt.Println()
}

// termWidth returns stdout's terminal width. Falls back to 0 when stdout
// isn't a TTY (piped, redirected) — caller treats that as "narrow".
func termWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return w
}
