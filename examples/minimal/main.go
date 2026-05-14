// Package main is the bare-bones coding agent from follow_along chapter 01
// — the inner loop, the outer REPL, three tools dispatched by a switch,
// and nothing else. About 130 lines in one file.
//
// Read it alongside follow_along/en/01-the-agent-loop.md (or .es.md) to
// see the essence of the harness before any of the layers — TUI, provider
// abstraction, compaction, MCP, subagents, debug — get added on top.
//
// Run:
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./examples/minimal
//
// Then type things at the `>` prompt. The agent will call tools without
// asking for confirmation — that's the permission gate from chapter 02.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

const systemPrompt = `You are a coding assistant running in a terminal. You have three tools:
bash, read_file, write_file. Be concise.`

// tools is the JSON-schema definition of what the model can call. Sent on
// every request alongside the messages slice. Chapter 09 turns this into a
// proper Registry; here it's just a slice.
var tools = []anthropic.ToolUnionParam{
	{OfTool: &anthropic.ToolParam{
		Name:        "bash",
		Description: anthropic.String("Run a shell command and return its combined stdout/stderr."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"command": map[string]any{"type": "string", "description": "The command to run."},
			},
			Required: []string{"command"},
		},
	}},
	{OfTool: &anthropic.ToolParam{
		Name:        "read_file",
		Description: anthropic.String("Read the contents of a file at the given path."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"path": map[string]any{"type": "string", "description": "Filesystem path to read."},
			},
			Required: []string{"path"},
		},
	}},
	{OfTool: &anthropic.ToolParam{
		Name:        "write_file",
		Description: anthropic.String("Write content to a file (creating or overwriting it)."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"path":    map[string]any{"type": "string", "description": "Filesystem path to write."},
				"content": map[string]any{"type": "string", "description": "The bytes to write."},
			},
			Required: []string{"path", "content"},
		},
	}},
}

var client = anthropic.NewClient()

func main() {
	ctx := context.Background()
	var messages []anthropic.MessageParam
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			return // EOF (ctrl-D)
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		messages = append(messages, anthropic.NewUserMessage(anthropic.NewTextBlock(input)))
		messages = agentLoop(ctx, messages)
	}
}

// agentLoop is the inner loop: call the model, dispatch any tool calls,
// loop. Two exits — the model returns plain text, or it returns a non-tool
// stop reason. Either way we return the updated messages slice so the
// outer REPL keeps the conversation going.
func agentLoop(ctx context.Context, messages []anthropic.MessageParam) []anthropic.MessageParam {
	for {
		resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeOpus4_7,
			MaxTokens: 8192,
			System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
			Messages:  messages,
			Tools:     tools,
		})
		if err != nil {
			fmt.Printf("api error: %v\n", err)
			return messages
		}
		messages = append(messages, resp.ToParam())

		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range resp.Content {
			switch v := block.AsAny().(type) {
			case anthropic.TextBlock:
				fmt.Println(v.Text)
			case anthropic.ToolUseBlock:
				result, isErr := executeTool(v.Name, v.JSON.Input.Raw())
				toolResults = append(toolResults, anthropic.NewToolResultBlock(v.ID, result, isErr))
			}
		}
		if resp.StopReason != anthropic.StopReasonToolUse {
			return messages
		}
		messages = append(messages, anthropic.NewUserMessage(toolResults...))
	}
}

// executeTool is the switch that runs whatever the model asked for. The
// (string, bool) return — content plus is-error flag — is the "errors as
// tool results" contract from chapter 02: failures travel back to the
// model so it can recover, instead of crashing the loop.
func executeTool(name, rawInput string) (string, bool) {
	fmt.Printf("[tool] %s %s\n", name, rawInput)
	switch name {
	case "bash":
		var in struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
			return err.Error(), true
		}
		out, err := exec.Command("sh", "-c", in.Command).CombinedOutput()
		if err != nil {
			return fmt.Sprintf("%s\n[exit error: %v]", out, err), true
		}
		return string(out), false

	case "read_file":
		var in struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
			return err.Error(), true
		}
		data, err := os.ReadFile(in.Path)
		if err != nil {
			return err.Error(), true
		}
		return string(data), false

	case "write_file":
		var in struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
			return err.Error(), true
		}
		if err := os.WriteFile(in.Path, []byte(in.Content), 0644); err != nil {
			return err.Error(), true
		}
		return "wrote " + in.Path, false

	default:
		return fmt.Sprintf("unknown tool: %s", name), true
	}
}
