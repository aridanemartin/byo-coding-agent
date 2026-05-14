package ui

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// The big banner is 75 cols wide. We need ~78 cols of terminal to render it
// without wrapping (a little breathing room).
const (
	bigBannerMinWidth = 78
	BigBannerWidth    = 75 // visual width of the wordmark, used by the shine animation
)

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

// AnimatedBanner returns the banner with a "shine" highlight peaked at
// column shineCol. Pass shineCol == -1 (or any out-of-range value) to get
// the static, settled look — useful as the first frame and as the final
// frame after the animation completes.
//
// Two effects beyond a flat sweep make this feel like light passing over
// polished metal:
//   - Diagonal angle. Each row's peak is offset by the row index, so the
//     shine cuts across the wordmark as a slash rather than a vertical bar.
//   - Comet trail. Cells behind the peak fade more slowly than cells ahead,
//     so the shine leaves a visible streak in its wake.
//
// Non-wide terminals fall back to the plain single-line wordmark with no
// animation.
func AnimatedBanner(width, shineCol int) string {
	if width == 0 {
		width = bigBannerMinWidth
	}
	if width < bigBannerMinWidth {
		// Narrow terminal: no animation, just the static fallback.
		return BannerText(width)
	}

	var sb strings.Builder
	sb.WriteString("\n")
	for row, line := range strings.Split(strings.Trim(bigBanner, "\n"), "\n") {
		col := 0
		for _, r := range line {
			if r == ' ' {
				sb.WriteRune(r)
				col++
				continue
			}
			fmt.Fprintf(&sb, "\x1b[1;38;5;%dm%s\x1b[0m", shineColor256(col, row, shineCol), string(r))
			col++
		}
		sb.WriteString("\n")
	}
	sb.WriteString(Dimmed("              build your own coding agent") + "\n")
	sb.WriteString(Dimmed("              type a message · /help for commands · ctrl-d to exit") + "\n\n")
	return sb.String()
}

// shineBase is the banner's resting color. It's used in two places, and
// the two must agree to avoid a visible "snap" when the animation ends:
//
//   - The tail of shineGradient, so cells far from the active shine peak
//     already sit at this color while the animation is running.
//   - The static return value from shineColor256 once the animation is
//     done (shineCol < 0).
//
// Keeping both in sync means the last animated frame already looks
// identical to the settled frame — the transition is invisible.
const shineBase = 39

// shineGradient is the 256-color palette indexed by distance from the
// shine peak: bright white at the core, ramping down to shineBase. The
// curve concentrates the dramatic brightness changes near the peak and
// settles smoothly into the base color so cells outside the streak look
// the same regardless of whether the animation is running.
var shineGradient = [...]int{
	231,        // d=0  pure white core
	195,        // d=1
	159,        // d=2
	123,        // d=3
	87,         // d=4
	51,         // d=5
	45,         // d=6
	shineBase,  // d=7
	shineBase,  // d=8
	shineBase,  // d=9
	shineBase,  // d≥10 settled base
}

// shineColor256 returns the 256-color foreground for a banner cell. Two
// inputs shape the gradient beyond a simple horizontal sweep:
//
//   - row creates a diagonal: each row peak is offset by `row` columns,
//     so a single shineCol draws a `\`-shaped streak across the wordmark.
//   - the asymmetry between leading and trailing edges gives a comet trail:
//     cells behind the peak fade at half the rate of cells ahead, so the
//     shine looks like it's leaving a glow in its wake.
func shineColor256(col, row, shineCol int) int {
	if shineCol < 0 {
		return shineBase // settled — must match shineGradient's tail
	}
	effCol := col + row // diagonal slope of 1 col per row
	diff := effCol - shineCol
	var d int
	if diff >= 0 {
		d = diff // ahead of the peak — short fade
	} else {
		d = (-diff + 1) / 2 // behind the peak — half-rate fade for the trail
	}
	if d >= len(shineGradient) {
		return shineGradient[len(shineGradient)-1]
	}
	return shineGradient[d]
}

// TermWidth returns stdout's terminal width, or 0 when stdout isn't a TTY.
func TermWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return w
}
