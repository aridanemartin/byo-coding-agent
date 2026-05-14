// Package main wires the harness together. The interesting code lives in
// the internal/ packages — this file is just the REPL, the agent loop, and
// the bits of glue that depend on more than one extension point.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/compact"
	"github.com/betta-tech/byo-coding-agent/internal/provider"
	"github.com/betta-tech/byo-coding-agent/internal/tool"
	"github.com/betta-tech/byo-coding-agent/internal/ui"
)

const systemPrompt = `You are a coding assistant running in a terminal harness. You have three tools:

- bash: execute shell commands in the user's working directory
- read_file: read the contents of a file
- write_file: write contents to a file (creates or overwrites)

Use the tools to accomplish what the user asks. Be concise.`

// Package-level state so /commands can read and mutate it. The whole REPL
// runs on one goroutine, so no locking is needed. Named `llm` rather than
// `provider` to avoid shadowing the package name.
var (
	llm       provider.Provider
	compactor compact.CompactionStrategy = compact.NoCompaction{}
	messages  []api.Message
	registry                             = tool.Default
)

func main() {
	// Swap this line to swap providers — that's the whole point of the abstraction.
	llm = provider.NewAnthropicProvider(anthropic.ModelClaudeOpus4_7, 8192, systemPrompt)

	// Swap this line to swap compaction strategies. NoCompaction by default;
	// try &compact.SlidingWindow{KeepLast: 10} or &compact.Summarize{Provider: llm, ...}.
	// Wrap any strategy with compact.WithLogging(inner, "path.log") to dump
	// before/after transcripts on each compaction event.
	compactor = compact.NoCompaction{}

	ctx := context.Background()

	ui.PrintBanner()
	for {
		line, ok := ui.ReadChatInput()
		if !ok {
			fmt.Println()
			return
		}
		userInput := strings.TrimSpace(line)
		if userInput == "" {
			continue
		}

		if runCommand(userInput) {
			continue
		}

		messages = append(messages, api.Message{
			Role:    api.RoleUser,
			Content: []api.Block{{Type: api.BlockText, Text: userInput}},
		})
		agentLoop(ctx)
	}
}

// agentLoop calls the provider and executes tool calls until it stops
// requesting tools. Reads and mutates the package-level `messages`.
func agentLoop(ctx context.Context) {
	for {
		if compacted, err := compactor.Compact(ctx, messages); err != nil {
			fmt.Printf("compaction error: %v (continuing without)\n", err)
		} else {
			if verbose && len(compacted) != len(messages) {
				ui.PrintCompaction(messages, compacted)
			}
			messages = compacted
		}

		sp := ui.StartSpinner("thinking...")
		resp, err := llm.Send(ctx, messages, registry.Definitions())
		sp.Stop()
		if err != nil {
			fmt.Printf("api error: %v\n", err)
			return
		}

		messages = append(messages, api.Message{Role: api.RoleAssistant, Content: resp.Content})

		var toolResults []api.Block
		for _, b := range resp.Content {
			switch b.Type {
			case api.BlockText:
				if b.Text != "" {
					fmt.Println(b.Text)
				}
			case api.BlockToolUse:
				result, isErr := executeTool(b.ToolName, b.ToolInput)
				toolResults = append(toolResults, api.Block{
					Type:       api.BlockToolResult,
					ToolUseID:  b.ToolUseID,
					ToolResult: result,
					IsError:    isErr,
				})
			}
		}

		if resp.StopReason != api.StopToolUse {
			return
		}

		messages = append(messages, api.Message{Role: api.RoleUser, Content: toolResults})
	}
}

// executeTool is the harness-side wrapper around a tool call: logging,
// approval gate, then dispatch to the registry. The tool's actual behavior
// lives in its own file under internal/tool/.
func executeTool(name, rawInput string) (string, bool) {
	fmt.Printf("[tool] %s %s\n", name, rawInput)
	if !ui.Confirm("approve?") {
		return "user denied this tool call", true
	}
	return registry.Execute(name, rawInput)
}
