package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/memory"
)

// RememberTool writes a piece of memory through the active memory.Default
// store. Persistence semantics depend on the Store implementation — see
// follow_along chapter 19. The agent should use this sparingly, for things
// the user genuinely wants to carry across sessions.
type RememberTool struct{}

func init() { Default.Register(&RememberTool{}) }

func (RememberTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name: "remember",
		Description: "Record a fact, decision, or preference into the agent's persistent memory so it carries across sessions. Use sparingly for things the user genuinely wants to persist (project conventions, preferences, important decisions). Don't dump every tool call here.",
		InputSchema: map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The text to remember.",
			},
			"kind": map[string]any{
				"type":        "string",
				"description": "Optional category: fact (default), decision, preference.",
			},
			"tags": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional short tags to make the entry findable later via the recall tool.",
			},
		},
		Required: []string{"content"},
	}
}

func (RememberTool) Execute(ctx context.Context, rawInput string) (string, bool) {
	var in struct {
		Content string   `json:"content"`
		Kind    string   `json:"kind"`
		Tags    []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	if in.Content == "" {
		return "remember: `content` is required", true
	}
	if in.Kind == "" {
		in.Kind = memory.KindFact
	}
	err := memory.Default.Save(ctx, memory.Entry{
		Time:    time.Now(),
		Kind:    in.Kind,
		Content: in.Content,
		Tags:    in.Tags,
	})
	if err != nil {
		return err.Error(), true
	}
	return "remembered.", false
}
