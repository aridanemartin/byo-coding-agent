package ui

import (
	"bytes"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
	"github.com/charmbracelet/lipgloss"
)

// HighlightPayload returns content with ANSI 256-color syntax highlighting
// applied if it looks like a recognized language (JSON or a unified diff),
// otherwise returns it unchanged. If width is greater than zero, the
// (possibly highlighted) output is also wrapped to that width — the wrap
// is ANSI-aware, so color codes survive intact.
//
// Detection is intentionally cheap: the first non-whitespace byte (or a
// "--- " prefix) decides the lexer. Anything else passes through with
// only the wrap applied.
func HighlightPayload(content string, width int) string {
	if lang := detectLanguage(content); lang != "" {
		var buf bytes.Buffer
		if err := quick.Highlight(&buf, content, lang, "terminal256", highlightStyle); err == nil {
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

// detectLanguage picks a Chroma lexer name from a leading-character peek.
// Returns "" when no lexer applies — caller skips highlighting in that case.
func detectLanguage(s string) string {
	t := strings.TrimSpace(s)
	if len(t) == 0 {
		return ""
	}
	if t[0] == '{' || t[0] == '[' {
		return "json"
	}
	if strings.HasPrefix(t, "--- ") {
		return "diff"
	}
	return ""
}
