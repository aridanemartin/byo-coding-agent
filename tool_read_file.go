package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type ReadFileTool struct{}

func init() { registry.Register(&ReadFileTool{}) }

func (ReadFileTool) Definition() ToolDef {
	return ToolDef{
		Name:        "read_file",
		Description: "Read the contents of a file at the given path.",
		InputSchema: map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to read.",
			},
		},
		Required: []string{"path"},
	}
}

func (ReadFileTool) Execute(rawInput string) (string, bool) {
	var in struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return fmt.Sprintf("invalid tool input: %v", err), true
	}
	data, err := os.ReadFile(in.Path)
	if err != nil {
		return err.Error(), true
	}
	return string(data), false
}
