package ui

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/lipgloss"
)

// HighlightPayload returns content with ANSI 256-color syntax highlighting
// applied if it looks like JSON, otherwise returns it unchanged. If width
// is greater than zero, the (possibly highlighted) output is also wrapped
// to that width — the wrap is ANSI-aware, so color codes survive intact.
//
// The detection is intentionally cheap: a leading `{` or `[` (after
// whitespace) is enough. Non-JSON payloads (plain text, transcripts) pass
// through with only the wrap applied.
func HighlightPayload(content string, width int) string {
	if looksLikeJSON(content) {
		var buf bytes.Buffer
		if err := quick.Highlight(&buf, content, "json", "terminal256", highlightStyle); err == nil {
			content = buf.String()
		}
	}
	if width > 0 {
		content = lipgloss.NewStyle().Width(width).Render(content)
	}
	return content
}

// highlightStyle is the Chroma style name used for the debug modal. Monokai
// reads well on dark terminals; "vs" or "github" are reasonable choices for
// light terminals if someone ever wants to make this configurable.
const highlightStyle = "monokai"

func looksLikeJSON(s string) bool {
	t := strings.TrimSpace(s)
	if len(t) == 0 {
		return false
	}
	switch t[0] {
	case '{', '[':
		return true
	}
	return false
}
