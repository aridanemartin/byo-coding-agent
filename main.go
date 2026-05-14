// Package main wires the harness together. The interesting code lives in
// the internal/ packages — this file constructs the root Agent, registers
// any subagents and their delegate tools, and launches the Bubble Tea TUI.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/betta-tech/byo-coding-agent/internal/agent"
	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/compact"
	"github.com/betta-tech/byo-coding-agent/internal/mcp"
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
	// AGENTS.md, if present in the working directory, is appended to the
	// system prompt as project-specific context. Opt-in, missing file is
	// silent — same shape as the MCP config.
	sysPrompt := systemPrompt + loadAgentsContext()
	llm := provider.NewAnthropicProvider(anthropic.ModelClaudeOpus4_7, 8192, sysPrompt)

	// MCP servers are opt-in via mcp.json. If the file is absent, no servers
	// are launched; if it's present, each entry registers its tools into
	// tool.Default before subagents claim subsets.
	mcpCtx, cancelMCP := context.WithCancel(context.Background())
	defer cancelMCP()
	mcpClients := setupMCP(mcpCtx)
	defer func() {
		for _, c := range mcpClients {
			_ = c.Close()
		}
	}()

	registerSubagents(llm)

	rootAgent = agent.New(llm, sysPrompt, tool.Default)
	rootAgent.Compactor = compact.NoCompaction{}
	rootAgent.MaxTurns = 50

	// The runner glues the input box to either commands or the agent loop.
	// Slash commands are handled inline; everything else goes to the agent.
	runner := func(ctx context.Context, input string) error {
		if runCommand(input) {
			return nil
		}
		_, err := rootAgent.Send(ctx, input)
		return err
	}

	// Build the Bubble Tea program first — we need its Send() to wire up
	// the approval flow before the agent ever runs. The UsageFunc reports
	// the provider's session totals so the TUI can render them on the
	// status line.
	usageFunc := func() (api.Usage, float64) {
		return llm.TotalUsage(), llm.EstimatedCostUSD()
	}
	program := ui.NewProgram(runner, usageFunc)

	// Confirm: a goroutine-safe function the agent calls when it wants y/n.
	// It posts an ApprovalRequest to the program and waits on the reply
	// channel. The program's Update flips into stateAwaitingApproval, the
	// user picks, and we resume.
	rootAgent.Confirm = func(prompt string) bool {
		reply := make(chan bool, 1)
		program.Send(ui.ApprovalRequest{Prompt: prompt, Reply: reply})
		return <-reply
	}

	// Redirect stdout into the program. Every existing fmt.Println in the
	// agent, tools, and commands flows through the pipe → forwarder
	// goroutine → ui.AppendMsg → viewport. No refactor of print sites.
	originalStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(originalStdout, "pipe setup failed: %v\n", err)
		return
	}
	os.Stdout = w
	ui.SuppressSpinner = true // status bar replaces the legacy spinner

	go func() {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			program.Send(ui.AppendMsg(scanner.Text() + "\n"))
		}
	}()

	if _, err := program.Run(); err != nil {
		os.Stdout = originalStdout
		fmt.Fprintf(originalStdout, "program error: %v\n", err)
		return
	}
	os.Stdout = originalStdout
}

// loadAgentsContext reads ./AGENTS.md and returns it wrapped in a header for
// inclusion in the system prompt. The file is the AGENTS.md convention
// popularized by Claude Code's CLAUDE.md — a place to leave project-specific
// guidance the agent should always have in context. Missing file is silent;
// AGENTS.md is opt-in.
func loadAgentsContext() string {
	data, err := os.ReadFile("AGENTS.md")
	if err != nil {
		return ""
	}
	return "\n\n# Project context (from AGENTS.md)\n\n" + string(data)
}

// setupMCP loads mcp.json (if present) and connects each configured server.
// Errors connecting individual servers are logged but never fatal.
func setupMCP(ctx context.Context) []*mcp.Client {
	cfg, err := mcp.LoadConfig("mcp.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp: config error: %v\n", err)
		return nil
	}
	return mcp.Register(ctx, cfg, tool.Default)
}

// registerSubagents wires up every subagent the harness exposes.
func registerSubagents(llm provider.Provider) {
	subagent.Default.Register(subagent.Research{
		Provider: llm,
		Tools:    tool.Default.Subset("read_file"),
	})
	for _, sa := range subagent.Default.All() {
		tool.Default.Register(&DelegateTool{Subagent: sa})
	}
}
