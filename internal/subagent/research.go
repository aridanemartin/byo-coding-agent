package subagent

import (
	"context"

	"github.com/betta-tech/byo-coding-agent/internal/agent"
	"github.com/betta-tech/byo-coding-agent/internal/provider"
	"github.com/betta-tech/byo-coding-agent/internal/tool"
)

// Research is a focused subagent for read-only investigation. It gets the
// file-reading tools but not bash, and runs with a tight system prompt so
// it stays on task. Each Run is a fresh agent — no state between calls.
type Research struct {
	Provider provider.Provider
	Tools    *tool.Registry
}

const researchSystem = `You are a research subagent. Your job is to investigate the
task you're given and return a concise, factual answer.

Rules:
- Use the tools available to look up information. Prefer fewer, more targeted
  reads over scanning everything.
- Return a short answer with the specific facts requested. No preamble.
- If the answer requires a path or identifier, include it verbatim.
- You have a limited number of tool calls; do not waste them.`

func (Research) Name() string { return "research" }

func (Research) Description() string {
	return "Investigate the codebase or filesystem and return a focused answer. " +
		"Prefer this over reading files yourself when the user asks ANY question " +
		"about the code — 'where is X', 'how does Y work', 'what does Z look like'. " +
		"The subagent has read_file access and its own context window, so it can " +
		"explore freely without polluting your conversation. Always pass a concrete " +
		"task description, not just the user's literal question."
}

func (r Research) Run(ctx context.Context, task string) (string, error) {
	defer Begin(r.Name())()

	a := agent.New(r.Provider, researchSystem, r.Tools)
	a.Name = r.Name()
	a.LogPrefix = "  ↳ "
	a.Quiet = true // suppress the subagent's running commentary; we return the final text
	a.MaxTurns = 10
	return a.Send(ctx, task)
}
