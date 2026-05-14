package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type WriteFileTool struct{}

func init() { registry.Register(&WriteFileTool{}) }

func (WriteFileTool) Definition() ToolDef {
	return ToolDef{
		Name:        "write_file",
		Description: "Write content to a file at the given path. Creates or overwrites.",
		InputSchema: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to write.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write.",
			},
		},
		Required: []string{"path", "content"},
	}
}

func (WriteFileTool) Execute(rawInput string) (string, bool) {
	var in struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	if err := os.WriteFile(in.Path, []byte(in.Content), 0644); err != nil {
		return err.Error(), true
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(in.Content), in.Path), false
}
