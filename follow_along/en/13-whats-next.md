# 13 · What's next

You have a working coding agent. It calls Claude, runs tools, asks before destructive ops, delegates investigation to subagents, compacts long conversations, and has a TUI that doesn't make you cringe.

This chapter is about what we deliberately skipped — the layers that would turn this from a learning project into something you'd actually use day-to-day — plus exercises that fit naturally into the existing architecture.

## What's missing

**Streaming.** The model returns a full response before we render anything. Real coding agents stream tokens as they arrive — text appears character by character, and `tool_use` blocks render as they're being constructed. The Anthropic SDK supports streaming via `client.Messages.NewStreaming(...)`; you'd change `Provider.Send` to return a channel of partial events, and `agent.loop` would forward them as `AppendMsg`s into the TUI. Hardest part: making the TUI append-in-place (within a line) rather than always appending whole lines. The `bubbles/textarea` is a starting point for this.

**Tests.** Nothing's automated. The `Provider` interface was specifically designed to be mockable; the agent loop should be testable end-to-end with a `MockProvider` that records calls and returns canned responses. Compaction strategies are easy to unit-test on synthetic message slices. Slash commands have no tests at all. This is the biggest gap relative to a "real" project.

**Prompt caching.** Every turn re-sends the full history at full price. Anthropic's prompt cache would cut that to ~10% for the cached prefix. The mechanics are well-documented; the harness change is small (add `cache_control: {type: "ephemeral"}` to the last system block on every request). What it costs: cache invalidation. Any change to the system prompt, the tool list, or any earlier message invalidates the cache for that prefix. Compaction in particular destroys it. The book-keeping isn't free.

**Multi-line input.** The Bubble Tea input is single-line. `bubbles/textarea` would unlock Shift-Enter for newlines, but on some terminals you can't distinguish Shift-Enter from Enter, which makes "Enter submits, Shift-Enter newlines" unreliable. Most production tools use Alt-Enter or Ctrl-J as the newline binding. Pick a convention and document it.

**Permission policies more interesting than "ask every time."** Right now every tool call gets prompted. A `PermissionPolicy` interface — `AlwaysAllow`, `AlwaysAsk`, `AllowList{names}`, `AskOnce` — would slot between the agent loop and the tool registry. Sketch is in chapter 02. This is the most pedagogically clean follow-up; it teaches the same plug-and-play pattern one more time, on the most "real" concern of all (safety).

**MCP support.** The Model Context Protocol is the standard for "agents talking to external tool servers." Adding MCP support means turning `Tool` into something that can be backed by a remote server, not just a local Go struct. The Anthropic SDK has MCP helpers; the architectural shape is that tools become a tuple of `(local Go funcs, remote MCP servers)` and the registry composes them.

**Persistence.** The conversation lives in memory; quitting loses it. A `/save` command writing `messages` as JSON, plus a `/load` that reads it back, is ~30 lines. A more sophisticated version stores per-project state (file: `.bettatech_harness_session.json` in the cwd).

**Token counting.** No way to know how big the conversation is in tokens. `client.Messages.CountTokens(...)` exists; surfacing it as a `/tokens` command or in the status bar is a few lines and useful when tuning compaction thresholds.

## Exercises that fit the existing architecture

These are roughly ordered from easiest to most involved.

### 1. Add a `web_fetch` tool

Take a URL, return the response body. Use `net/http` with a sensible timeout. Drop a file in `internal/tool/`, `init()`-register, done. ~30 lines.

### 2. Add an alias mechanism to slash commands

Make `/quit` an alias for `/exit`. Decide whether to add a new entry, or first-class aliases via an `aliases []string` field on the `command` struct. The decision is itself the exercise.

### 3. Write a `MockProvider`

Implements `Provider`. Stores a list of canned responses and returns them in order. Used to test the agent loop without an API key. Bonus: track what `Send` was called with so you can assert on the messages and tools.

### 4. Add a `TokenBudget` compaction strategy

Drop oldest messages until estimated token count is under a configurable threshold. Start with byte-count as a token proxy (cheap, no API call). Later, swap in `client.Messages.CountTokens(...)` for accuracy.

### 5. Add a `PermissionPolicy` abstraction

Interface with `AlwaysAllow`, `AlwaysAsk`, `AllowList{names}`, `AskOnce`. Slot it between the agent loop and the registry (replace the inline `confirm` call). One line in `main.go` to pick a policy. Mirror the Provider / Compactor pattern exactly.

### 6. Add a second subagent

A `CodeReview` subagent with tools `read_file` + `bash` (for running linters), with a review-focused system prompt. Register it in `main.registerSubagents`. Confirm the model picks the right one based on description.

### 7. Streaming

The big one. Change `Provider.Send` to return a channel of partial responses (or use a callback). Update `agent.loop` to forward partial events as text. Update the TUI to append within the current paragraph rather than always appending whole lines.

### 8. Wire input history into the new TUI

The old `internal/ui/input.go` still has `loadHistory` / `appendHistory`. The Bubble Tea program in chapter 12 doesn't use them. Add a `[]string` field to the harness model, handle Up/Down keys for navigation, persist to `~/.bettatech_harness_history` on submit. The chapter-08 input had all this — port it.

## Going further: what becomes Claude Code

A few features that distinguish a polished product from this harness:

- **Persistent context across sessions.** Each project gets a `.claude` directory; messages and learnings persist.
- **Skill packs.** Files in a known location that the agent can load context from on demand. (`SKILL.md` files; loaded only when relevant to the task.)
- **Slash commands you can define.** A `.claude/commands/` directory; each file is a prompt template; `/<filename>` invokes it.
- **Hooks.** Pre-tool and post-tool callbacks you configure; lets you lint, audit, or block tool calls without modifying the harness code.
- **Inline diffs.** When the agent edits a file, show a diff in the UI; let you approve/reject changes hunk-by-hunk.
- **Sandboxing.** `bash` runs in a container or restricted shell, not on the host directly. (For agents that operate on shared infrastructure.)
- **Cost tracking.** The agent's token usage and dollar cost are always visible.

Each of those is a chapter unto itself. None are in this book. If you implement one and want to send a pull request, please do.

## Three layers, the same discipline

The chapters above built a harness from scratch. That's one layer. Two more sit above it — the same discipline applied at higher altitude:

| Layer | What you touch | Where the leverage is |
|---|---|---|
| **Building** | The Go code: agent loop, provider, tool registry, compaction | Forms the mental model. ~1% of time. |
| **Extending** | New code plugged into existing abstractions — MCP wrappers (ch 14), token reporters (ch 16), a new subagent | The bridge. ~10% of time. |
| **Configuring** | Files the harness reads and prompts you write — `AGENTS.md` (ch 15), `mcp.json`, an SDD workflow, slash-command palettes | Where most real work happens. ~89% of time. |

A poorly-configured Claude Code with a 50 KB self-contradicting `AGENTS.md` feels exactly as broken as a poorly-built harness. Getting either right is the same skill: a small number of orthogonal decisions, made deliberately, with awareness of what they cost. The reason this book emphasizes building is that the mental model only forms when you can see every seam. Once you can, every config file reads differently.

If you came in thinking "configuration isn't real engineering," chapter 14 onward should have settled it: `mcp.json` and `AGENTS.md` reshape the agent's behavior as much as anything in `internal/`. The difference between layers is your blast radius, not the kind of thinking involved.

## A closing note on harness engineering

You've just built a harness — albeit a small one — for an LLM. The architecture choices weren't accidents: every interface, every layer, every decorator was a deliberate response to a real problem. When you read other harnesses (Claude Code's source, OpenCode, Aider, Continue), you'll see the same shapes. Different colors, different idioms, but the same skeleton: an agent loop, a tool surface, a permission gate, a context manager, a UI.

The next time you sit down to use an AI coding tool, you'll be able to read its quirks as harness decisions: "ah, they ask per-session not per-call," "they keep tool results out of the visible UI," "they use a sliding window not summarization." You'll also know what's missing or what you'd do differently.

That's the win. The model isn't the product. The harness is.

← [back to TOC](README.md)
