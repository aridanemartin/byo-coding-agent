package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

const systemPrompt = `You are a coding assistant running in a terminal harness. You have three tools:

- bash: execute shell commands in the user's working directory
- read_file: read the contents of a file
- write_file: write contents to a file (creates or overwrites)

Use the tools to accomplish what the user asks. Be concise.`

// Package-level state so /commands can read and mutate it. The whole REPL
// runs on one goroutine, so no locking is needed.
var (
	provider  Provider
	compactor CompactionStrategy = NoCompaction{}
	messages  []Message
)

func main() {
	// Swap this line to swap providers — that's the whole point of the abstraction.
	provider = NewAnthropicProvider(anthropic.ModelClaudeOpus4_7, 8192, systemPrompt)

	// Swap this line to swap compaction strategies. NoCompaction by default;
	// try &SlidingWindow{KeepLast: 10} or &Summarize{Provider: provider, ...}.
	// Wrap any strategy with WithLogging(inner, "path.log") to dump before/after
	// transcripts on each compaction event.
	compactor = NoCompaction{}

	ctx := context.Background()

	printBanner()
	for {
		line, ok := readChatInput()
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

		messages = append(messages, Message{
			Role:    RoleUser,
			Content: []Block{{Type: BlockText, Text: userInput}},
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
				printCompaction(messages, compacted)
			}
			messages = compacted
		}

		sp := startSpinner("thinking...")
		resp, err := provider.Send(ctx, messages, registry.Definitions())
		sp.Stop()
		if err != nil {
			fmt.Printf("api error: %v\n", err)
			return
		}

		// Record the assistant turn verbatim — tool_use blocks and all.
		messages = append(messages, Message{Role: RoleAssistant, Content: resp.Content})

		var toolResults []Block
		for _, b := range resp.Content {
			switch b.Type {
			case BlockText:
				if b.Text != "" {
					fmt.Println(b.Text)
				}
			case BlockToolUse:
				result, isErr := executeTool(b.ToolName, b.ToolInput)
				toolResults = append(toolResults, Block{
					Type:       BlockToolResult,
					ToolUseID:  b.ToolUseID,
					ToolResult: result,
					IsError:    isErr,
				})
			}
		}

		if resp.StopReason != StopToolUse {
			return
		}

		messages = append(messages, Message{Role: RoleUser, Content: toolResults})
	}
}

// executeTool is the harness-side wrapper around a tool call: logging,
// approval gate, then dispatch to the registry. The tool's actual behavior
// lives in its own file (tool_bash.go, tool_read_file.go, etc.).
func executeTool(name string, rawInput string) (string, bool) {
	fmt.Printf("[tool] %s %s\n", name, rawInput)
	if !confirm("approve?") {
		return "user denied this tool call", true
	}
	return registry.Execute(name, rawInput)
}
