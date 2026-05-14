package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type BashTool struct{}

func init() { registry.Register(&BashTool{}) }

func (BashTool) Definition() ToolDef {
	return ToolDef{
		Name:        "bash",
		Description: "Run a shell command. Returns combined stdout and stderr.",
		InputSchema: map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute.",
			},
		},
		Required: []string{"command"},
	}
}

func (BashTool) Execute(rawInput string) (string, bool) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	cmd := exec.Command("sh", "-c", in.Command)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("%s\n[exit error: %v]", out, err), true
	}
	return string(out), false
}
