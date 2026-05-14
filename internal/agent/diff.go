package agent

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pmezard/go-difflib/difflib"
)

// buildWriteDiff returns a unified-diff string describing what a
// write_file tool call would change on disk, suitable for display in the
// approval modal. Returns "" when the input is malformed (caller falls
// back to the plain "approve?" prompt).
//
// When the target file doesn't exist yet, the result still uses the
// unified-diff prelude (--- /dev/null / +++ new file) so the diff lexer
// in chroma colors the body green, matching the natural "this is brand
// new" semantic.
func buildWriteDiff(rawInput string) string {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return ""
	}

	existing, err := os.ReadFile(in.Path)
	if err != nil {
		// New file. Format as a unified diff against /dev/null so the
		// "+" lines drive consistent highlighting downstream.
		return synthesizeNewFileDiff(in.Path, in.Content)
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(existing)),
		B:        difflib.SplitLines(in.Content),
		FromFile: in.Path + " (current)",
		ToFile:   in.Path + " (proposed)",
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return ""
	}
	if text == "" {
		// File exists with identical content — nothing to approve. Still
		// return a non-empty marker so the modal opens with a clear
		// message instead of falling back to the plain prompt.
		return "(no changes: proposed content is identical to current file)\n"
	}
	return text
}

func synthesizeNewFileDiff(path, content string) string {
	lines := difflib.SplitLines(content)
	var out string
	out += "--- /dev/null\n"
	out += "+++ " + path + " (new file)\n"
	out += fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(lines))
	for _, l := range lines {
		out += "+" + l
		// SplitLines preserves trailing newlines; if the last line lacked
		// one, the next iteration would concatenate without a break.
	}
	if len(lines) > 0 && lines[len(lines)-1] != "" && lines[len(lines)-1][len(lines[len(lines)-1])-1] != '\n' {
		out += "\n"
	}
	return out
}
