package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/subagent"
	"github.com/betta-tech/byo-coding-agent/internal/ui"
)

// DelegateTool exposes one Subagent to the model as a callable tool. The
// model sees "delegate_<subagent name>" with the subagent's description and
// a single `task` parameter. When called, the subagent runs in its own
// agent loop and returns its final answer as the tool result.
//
// Lives in package main rather than internal/tool/ to avoid an import
// cycle (tool → subagent → agent → tool). The Tool interface itself is
// defined in internal/tool/, so we still implement it cleanly.
type DelegateTool struct {
	Subagent subagent.Subagent
}

func (d *DelegateTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "delegate_" + d.Subagent.Name(),
		Description: d.Subagent.Description(),
		InputSchema: map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "Concrete description of what the subagent should do.",
			},
		},
		Required: []string{"task"},
	}
}

func (d *DelegateTool) Execute(ctx context.Context, rawInput string) (string, bool) {
	var in struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}

	name := d.Subagent.Name()
	fmt.Println(ui.Dimmed(fmt.Sprintf("↳ delegating to %s subagent", name)))

	start := time.Now()
	result, err := d.Subagent.Run(ctx, in.Task)
	elapsed := time.Since(start).Round(time.Millisecond)

	fmt.Println(ui.Dimmed(fmt.Sprintf("← %s subagent done (%s)", name, elapsed)))

	if err != nil {
		return fmt.Sprintf("subagent error: %v", err), true
	}
	return result, false
}
