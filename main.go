// Package main wires the harness together. The interesting code lives in
// the internal/ packages — this file constructs the root Agent, registers
// any subagents and their delegate tools, and launches the Bubble Tea TUI.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/betta-tech/byo-coding-agent/internal/agent"
	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/compact"
	"github.com/betta-tech/byo-coding-agent/internal/debug"
	"github.com/betta-tech/byo-coding-agent/internal/mcp"
	"github.com/betta-tech/byo-coding-agent/internal/provider"
	"github.com/betta-tech/byo-coding-agent/internal/subagent"
	"github.com/betta-tech/byo-coding-agent/internal/tool"
	"github.com/betta-tech/byo-coding-agent/internal/ui"
)

const systemPrompt = `You are running inside a small, hackable coding-agent harness written in Go — about 1,000 lines under internal/, with chapter-length notes in follow_along/ explaining why each layer exists. The repo is a learning project. The user is here to understand how harnesses work and to play with this one: read its code, swap pieces, break things, fix them. Treat that curiosity as the default mode of the session, not an exception.

When something you're doing intersects with a harness feature, mention it briefly so the user can poke at it. Don't push — just one short pointer so they know the seam exists. Examples:

- "I'll delegate this read-only investigation — chapter 11 has the design."
- "If you want to see the actual JSON I sent to the API, /debug on."
- "After this turn /tokens will show what it cost."
- "This write goes through the diff approval modal (chapter 18) — you'll see the proposed change before it lands."
- "/compact summarize and /verbose on let you watch the conversation get rewritten."

If the user is asking a how-does-this-work question and a follow_along chapter covers it, pointing them there ("chapter 03 walks through the provider seam") is usually more useful than re-explaining from scratch. The chapters are the canonical answer; you're the shortcut to them.

You can read the harness's own source — main.go, commands.go, anything under internal/, the follow_along/ markdowns — exactly like any other file. The harness is a known territory, not a black box.

Tool guidance:

For acting on the filesystem (creating, editing, running things), use bash, read_file, and write_file directly. Writes are mediated by a diff approval modal, so propose changes naturally — the user reviews each one.

For READ-ONLY INVESTIGATION you SHOULD call delegate_research rather than reading files yourself. This includes questions like:
- "where is X defined?"
- "what fields does Y have?"
- "look at the structure of Z"
- "find references to A in the code"
- "summarize how B works"

The subagent has its own context window, so it can do many reads without cluttering yours. Prefer delegating even when you think one or two reads would do it. Only skip the subagent if the question is about a single file the user has already shown you. After delegating, present the subagent's findings directly.

Be concise. Be honest when you don't know — guessing is worse than saying "I'd need to read X to answer that." Match the user's language: if they write in Spanish, answer in Spanish.`

// rootAgent is the agent the REPL drives. Subagents have their own Agent
// instances scoped to a single delegate_* tool call.
var rootAgent *agent.Agent

// activeSysPrompt is the fully-assembled system prompt (base + AGENTS.md),
// stashed at package scope so /provider can construct replacement providers
// with the same prompt mid-session.
var activeSysPrompt string

// activeProgram is the Bubble Tea program, exposed at package scope so
// slash commands can send messages into the UI (e.g. DebugToggleMsg).
var activeProgram *tea.Program

// notifyDebugUI sends a DebugToggleMsg into the running program so it
// recomputes layout. Called from /debug after flipping state.
func notifyDebugUI() {
	if activeProgram != nil {
		activeProgram.Send(ui.DebugToggleMsg{})
	}
}

func main() {
	// AGENTS.md, if present in the working directory, is appended to the
	// system prompt as project-specific context. Opt-in, missing file is
	// silent — same shape as the MCP config.
	activeSysPrompt = systemPrompt + loadAgentsContext()
	llm := newProvider(activeSysPrompt)

	// MCP servers are opt-in via mcp.json. If the file is absent, no servers
	// are launched; if it's present, each entry registers its tools into
	// tool.Default. We do this in a background goroutine started further
	// below — connecting servers (especially over HTTP) can take seconds,
	// and we'd rather show the TUI immediately and stream a loading line
	// than block on startup.
	mcpCtx, cancelMCP := context.WithCancel(context.Background())
	var (
		mcpClientsMu sync.Mutex
		mcpClients   []*mcp.Client
		mcpWG        sync.WaitGroup
	)
	defer func() {
		// Signal the goroutine to stop dialing, wait for it to finish,
		// then close whichever clients it managed to connect.
		cancelMCP()
		mcpWG.Wait()
		mcpClientsMu.Lock()
		for _, c := range mcpClients {
			_ = c.Close()
		}
		mcpClientsMu.Unlock()
	}()

	registerSubagents(llm)

	rootAgent = agent.New(llm, activeSysPrompt, tool.Default)
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
	// Read from rootAgent.Provider (not the local llm) so /provider swaps
	// propagate to the status line without rebuilding the program.
	usageFunc := func() (api.Usage, float64) {
		if r, ok := rootAgent.Provider.(interface {
			TotalUsage() api.Usage
			EstimatedCostUSD() float64
		}); ok {
			return r.TotalUsage(), r.EstimatedCostUSD()
		}
		return api.Usage{}, -1
	}
	program := ui.NewProgram(runner, usageFunc)
	activeProgram = program

	// debug events from anywhere in the harness push a refresh into the TUI
	// so the panel updates in near-real-time.
	debug.SetSink(func(_ debug.Event) {
		program.Send(ui.DebugRefreshMsg{})
	})

	// Now that we have a program to send progress events into, kick off
	// MCP server connection in the background. The TUI shows a loading
	// line while this runs; the user can already type and the agent can
	// already work (with whatever tools are registered so far) before
	// MCP servers finish connecting.
	mcpWG.Add(1)
	go func() {
		defer mcpWG.Done()
		clients := setupMCP(mcpCtx, func(server string, status mcp.ProgressStatus, total int) {
			program.Send(ui.MCPStatusMsg{Server: server, Status: status, Total: total})
		})
		mcpClientsMu.Lock()
		mcpClients = clients
		mcpClientsMu.Unlock()
	}()

	// Confirm: a goroutine-safe function the agent calls when it wants y/n.
	// It posts an ApprovalRequest to the program and waits on the reply
	// channel. The program's Update flips into stateAwaitingApproval, the
	// user picks, and we resume. detail is optional long-form content
	// (e.g. a diff for write_file calls) shown in a modal.
	rootAgent.Confirm = func(prompt, detail string) bool {
		reply := make(chan bool, 1)
		program.Send(ui.ApprovalRequest{Prompt: prompt, Detail: detail, Reply: reply})
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

// KnownProviders is the ordered list of names accepted by /provider and by
// $LLM_PROVIDER. Update both this list and newProviderByName when adding a
// backend.
var KnownProviders = []string{"anthropic", "openai"}

// newProvider picks the initial provider at startup, honoring $LLM_PROVIDER
// and $LLM_MODEL as optional defaults. Mid-session swaps go through the
// /provider slash command instead.
func newProvider(sysPrompt string) provider.Provider {
	name := os.Getenv("LLM_PROVIDER")
	if name == "" {
		name = "anthropic"
	}
	p, err := newProviderByName(name, os.Getenv("LLM_MODEL"), sysPrompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provider: %v (falling back to anthropic)\n", err)
		p, _ = newProviderByName("anthropic", "", sysPrompt)
	}
	return p
}

// newProviderByName constructs a provider by short name. An empty model
// string falls back to that provider's default. Unknown names error.
func newProviderByName(name, model, sysPrompt string) (provider.Provider, error) {
	switch name {
	case "anthropic":
		m := anthropic.Model(model)
		if m == "" {
			m = anthropic.ModelClaudeOpus4_7
		}
		return provider.NewAnthropicProvider(m, 8192, sysPrompt), nil
	case "openai":
		if model == "" {
			model = "gpt-5-codex"
		}
		return provider.NewOpenAIProvider(model, sysPrompt, 8192), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (try one of %s)", name, strings.Join(KnownProviders, ", "))
	}
}

// switchProvider replaces the root agent's provider mid-session and
// re-registers subagents so they use the new one. Conversation history is
// kept — it's translated by the new provider on the next Send. Token totals
// reset because each provider keeps its own accumulator.
func switchProvider(name, model string) error {
	p, err := newProviderByName(name, model, activeSysPrompt)
	if err != nil {
		return err
	}
	rootAgent.Provider = p
	registerSubagents(p)
	return nil
}

// ProviderKind returns the short name of a provider value. Used by /model
// and /provider to display the active backend.
func ProviderKind(p provider.Provider) string {
	switch p.(type) {
	case *provider.AnthropicProvider:
		return "anthropic"
	case *provider.OpenAIProvider:
		return "openai"
	default:
		return "unknown"
	}
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
// progress receives per-server lifecycle events the caller can pipe into
// the TUI loading indicator. Errors connecting individual servers are
// logged but never fatal.
func setupMCP(ctx context.Context, progress mcp.ProgressFunc) []*mcp.Client {
	cfg, err := mcp.LoadConfig("mcp.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mcp: config error: %v\n", err)
		return nil
	}
	return mcp.Register(ctx, cfg, tool.Default, progress)
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
