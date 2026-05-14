package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/memory"
)

// RecallTool surfaces past memory entries (typically prior session
// summaries) that match a query. Match semantics depend on the Store —
// SessionFiles (the default) does case-insensitive substring against tags
// and summaries. The tool returns metadata only; the agent can read_file
// on the returned paths if it wants full transcripts.
type RecallTool struct{}

func init() { Default.Register(&RecallTool{}) }

func (RecallTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "recall",
		Description: "Search the agent's persistent memory of past sessions. Returns dates, tags, summaries, and file paths for matching sessions — read_file on a returned path to see the full transcript. Recent sessions are already in your context as part of the system prompt; check there first.",
		InputSchema: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Substring to match against session tags and summaries (case-insensitive).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of session records to return. Defaults to 5.",
			},
		},
		Required: []string{"query"},
	}
}

func (RecallTool) Execute(ctx context.Context, rawInput string) (string, bool) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	if in.Limit <= 0 {
		in.Limit = 5
	}
	entries, err := memory.Default.Recall(ctx, in.Query, in.Limit)
	if err != nil {
		return err.Error(), true
	}
	if len(entries) == 0 {
		return "no matches.", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d match(es) for %q:\n", len(entries), in.Query)
	for _, e := range entries {
		fmt.Fprintf(&b, "- %s — %s", e.Time.Format("2006-01-02"), e.Content)
		if len(e.Tags) > 0 {
			fmt.Fprintf(&b, " [%s]", strings.Join(e.Tags, ", "))
		}
		b.WriteString("\n")
	}
	return b.String(), false
}
