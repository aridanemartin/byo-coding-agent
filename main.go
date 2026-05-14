// Package main wires the harness together. The interesting code lives in
// the internal/ packages — this file constructs the root Agent, registers
// any subagents and their delegate tools, and runs the REPL.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/betta-tech/byo-coding-agent/internal/agent"
	"github.com/betta-tech/byo-coding-agent/internal/compact"
	"github.com/betta-tech/byo-coding-agent/internal/provider"
	"github.com/betta-tech/byo-coding-agent/internal/subagent"
	"github.com/betta-tech/byo-coding-agent/internal/tool"
	"github.com/betta-tech/byo-coding-agent/internal/ui"
)

const systemPrompt = `You are a coding assistant running in a terminal harness. You have file and shell tools, plus a research subagent you can delegate read-only investigations to.

For acting on the filesystem (creating, editing, running things), use bash, read_file, and write_file directly.

For READ-ONLY INVESTIGATION you SHOULD call delegate_research rather than reading files yourself. This includes questions like:
- "where is X defined?"
- "what fields does Y have?"
- "look at the structure of Z"
- "find references to A in the code"
- "summarize how B works"

The subagent has its own context window, so it can do many reads without cluttering yours. Prefer delegating for investigation, even when you think one or two reads would do it. Only skip the subagent if the question is about a single file the user has already shown you.

After delegating, present the subagent's findings to the user directly.

Be concise.`

// rootAgent is the agent the REPL drives. Subagents have their own Agent
// instances scoped to a single delegate_* tool call.
var rootAgent *agent.Agent

func main() {
	llm := provider.NewAnthropicProvider(anthropic.ModelClaudeOpus4_7, 8192, systemPrompt)

	// Subagents need to be configured (provider, tool subset) before the
	// REPL starts. Each one gets registered in subagent.Default and
	// surfaced to the model via a DelegateTool.
	registerSubagents(llm)

	rootAgent = agent.New(llm, systemPrompt, tool.Default)
	rootAgent.Compactor = compact.NoCompaction{}
	rootAgent.Confirm = ui.Confirm // the root agent asks the user before each tool call
	rootAgent.MaxTurns = 50

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

		if _, err := rootAgent.Send(ctx, userInput); err != nil {
			fmt.Printf("api error: %v\n", err)
		}
	}
}

// registerSubagents wires up every subagent the harness exposes. Each one
// is constructed with the resources it needs, registered in subagent.Default
// (so /subagents can list them), and wrapped in a DelegateTool that's
// registered in tool.Default (so the model can call it).
func registerSubagents(llm provider.Provider) {
	// Research subagent — read-only file investigation.
	subagent.Default.Register(subagent.Research{
		Provider: llm,
		Tools:    tool.Default.Subset("read_file"),
	})

	// Expose each registered subagent to the model via a DelegateTool.
	// (DelegateTool lives in package main to avoid an import cycle —
	// see delegate.go for the why.)
	for _, sa := range subagent.Default.All() {
		tool.Default.Register(&DelegateTool{Subagent: sa})
	}
}
