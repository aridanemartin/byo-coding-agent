# Build Your Own Coding Agent

[![ci](https://github.com/betta-tech/byo-coding-agent/actions/workflows/ci.yml/badge.svg)](https://github.com/betta-tech/byo-coding-agent/actions/workflows/ci.yml)

🌐 **Languages:** **English** · [Español](README.es.md)

A hands-on introduction to **harness engineering** — the discipline of building the scaffolding around an LLM that turns it into a useful agent. You'll build a working AI coding agent in Go, then experiment with the parts that matter: providers, tools, compaction strategies, and permissions.

## What is harness engineering?

The model is the engine. The harness is everything else: the loop that calls it, the tools it can use, how its conversation is shaped over time, what it's allowed to do, how the user talks to it.

Get the harness right and a mid-tier model feels great. Get it wrong and a frontier model feels broken. Most of the interesting decisions in tools like Claude Code, OpenCode, and Aider live in their harnesses, not their models.

Harness engineering happens at three layers — **building** the loop and abstractions, **extending** them with new tools or integrations, and **configuring** the behavior via files like `AGENTS.md` and `mcp.json`. Most practitioners spend the bulk of their time in the top layer; this book focuses on building because that's where the mental model is formed. Once you've built one harness, you read every config file with new eyes.

This project is a stripped-down, readable version of those tools, designed to be poked at.

## What you'll build

A terminal-based coding agent (~600 lines of Go) that:

- Talks to Claude (or any LLM you plug in)
- Calls tools — `bash`, `read_file`, `write_file` — to act on your filesystem
- Asks for approval before each tool call
- Compacts long conversations using pluggable strategies
- Supports slash commands (`/help`, `/model`, `/compact`, `/verbose`, …)
- Has a TUI input with history, line editing, and a styled prompt

## Prerequisites

- Go 1.21+ (the project uses generics and the `max` builtin)
- An Anthropic API key — [console.anthropic.com](https://console.anthropic.com) → Settings → API Keys

## Quick start

```sh
git clone git@github.com:betta-tech/byo-coding-agent.git
cd byo-coding-agent
export ANTHROPIC_API_KEY=sk-ant-...
go run .
```

To run against OpenAI as well, export the key once:

```sh
export OPENAI_API_KEY=sk-...
go run .
```

Then switch providers mid-session with the slash command — no restart needed:

```
/provider              # show current provider + the available choices
/provider openai       # swap to OpenAI (defaults to gpt-5-codex)
/provider openai gpt-4o-mini
/provider anthropic
/model gpt-5           # change just the model on the current provider
```

Both providers implement the same [`Provider`](internal/provider/provider.go) interface; the rest of the harness is unchanged when you swap. Setting `LLM_PROVIDER`/`LLM_MODEL` env vars still works as a startup default if you'd rather not type the command every session.

Type `/help` to see commands. Try:

- `list the files here`
- `write a hello.txt with a haiku in it`
- `read main.go and tell me how the agent loop works`

## Architecture in 60 seconds

The harness is built around three orthogonal extension points. Each lives in its own `internal/` package, with a small interface and an in-tree default implementation. Swapping is one line.

```
┌─────────────────────────────────────────────────────┐
│  main.go        wiring · REPL loop · agent loop     │
│  commands.go    /help · /model · /compact · …       │
└─────────────────────────────────────────────────────┘
        │
        ├── internal/api/             shared types (Message, Block, ToolDef, …)
        │
        ├── internal/provider/        Provider interface + Anthropic impl
        │     Send messages → get response. Swap to add OpenAI etc.
        │
        ├── internal/tool/            Tool interface + Registry + one file per tool
        │     Self-registers via init() — drop a file in, it appears.
        │
        ├── internal/compact/         CompactionStrategy interface + strategies
        │     SlidingWindow, Summarize, NoCompaction, WithLogging decorator.
        │
        └── internal/ui/              Banner, spinner, Bubble Tea input, styles
              TUI affordances. Readable but less interesting.
```

## Project layout

```
.
├── main.go              wiring + REPL + agent loop + executeTool wrapper
├── commands.go          slash command registry
└── internal/
    ├── api/             Message, Block, ToolDef, Response, RenderTranscript
    ├── provider/        Provider interface + AnthropicProvider
    ├── tool/            Tool interface + Registry + bash / readfile / writefile
    ├── compact/         CompactionStrategy + SlidingWindow / Summarize / Logging
    └── ui/              banner, spinner, input (Bubble Tea), styling helpers
```

`internal/` is enforced by the Go compiler — packages in there can only be imported by code in this same module, which is the right signal for "these aren't meant to be reused as a library."

## The three extension points

### 1. Providers

Want to use OpenAI, Bedrock, or a local model? Implement `Provider`:

```go
type Provider interface {
    Send(ctx context.Context, messages []Message, tools []ToolDef) (Response, error)
    Model() string
    SetModel(name string)
}
```

Then change one line in `main.go`:

```go
llm = provider.NewOpenAIProvider(...)  // instead of provider.NewAnthropicProvider
```

The adapter is the only place that knows the SDK's wire format. The rest of the harness deals in generic `Message` / `Block` / `ToolDef` types. See `internal/provider/anthropic.go` for the reference.

### 2. Tools

Want to add `git_diff`, `web_search`, `kubectl`? Create **one file** under `internal/tool/`:

```go
// internal/tool/gitdiff.go
package tool

import (
    "os/exec"

    "github.com/betta-tech/byo-coding-agent/internal/api"
)

type GitDiffTool struct{}

func init() { Default.Register(&GitDiffTool{}) }

func (GitDiffTool) Definition() api.ToolDef {
    return api.ToolDef{
        Name:        "git_diff",
        Description: "Show uncommitted changes in the current repo.",
        InputSchema: map[string]any{},
        Required:    []string{},
    }
}

func (GitDiffTool) Execute(_ string) (string, bool) {
    out, err := exec.Command("git", "diff").CombinedOutput()
    if err != nil { return string(out), true }
    return string(out), false
}
```

Drop the file in. Run `go run .`. Type `/tools` — `git_diff` is in the list, the model can call it. **No edits to `main.go`** — `main` already imports `internal/tool`, so the new file's `init()` runs when the package loads.

### 3. Compaction strategies

Want to test different ways to handle long conversations? Implement `CompactionStrategy`:

```go
type CompactionStrategy interface {
    Compact(ctx context.Context, messages []Message) ([]Message, error)
}
```

Three are included:

| Strategy | What it does |
|---|---|
| `compact.NoCompaction{}` | Default — never modifies messages |
| `&compact.SlidingWindow{KeepLast: 10}` | Keeps the last N messages, drops older |
| `&compact.Summarize{Provider: llm, Threshold: 20, KeepRecent: 6}` | Asks the model to summarize old turns once history hits `Threshold` |

Wrap any of them with `compact.WithLogging(inner, "compactions.log")` to record before/after diffs to a file — useful for comparing strategies.

Swap by changing one line in `main.go`:

```go
compactor = &compact.Summarize{Provider: llm, Threshold: 20, KeepRecent: 6}
```

There's a subtle bit: a naive truncation can leave a `tool_use` block without its matching `tool_result`, and the API will 400. The `SafeSplitPoint` helper in `internal/compact/strategy.go` walks back until it finds a "clean" boundary. All strategies route through it.

## Commands

| Command | Effect |
|---|---|
| `/help` | List all commands |
| `/provider [anthropic\|openai] [model]` | Show or swap the LLM provider |
| `/model [name]` | Show current model or change it (provider-aware suggestions) |
| `/tokens` | Show cumulative input/output tokens and estimated cost |
| `/debug [on\|off\|clear]` | Toggle the live debug panel (provider calls, tool dispatch, compaction) |
| `/clear` | Wipe conversation history |
| `/tools` | List registered tools |
| `/subagents` | List registered and in-flight subagents |
| `/compact [sliding\|summarize\|none]` | Run compaction (configured strategy, or ad-hoc one) |
| `/verbose [on\|off]` | Toggle before/after printing on compaction |
| `/exit` | Quit |

## Now try

In rough order of difficulty:

1. **Add a `git_diff` tool.** Read `tool_bash.go` for the pattern, write a new `tool_git_diff.go`. Verify `/tools` lists it after.
2. **Add a `TokenBudget` compaction strategy.** Drop oldest messages until estimated token count is under a configurable threshold. Start with a byte-count approximation; later swap in a real `count_tokens` call.
3. **Add a `PermissionPolicy` abstraction.** Currently every tool call goes through `confirm`. Refactor so a policy decides — `AlwaysAllow`, `AlwaysAsk`, `AllowList{names}`. The policy slots into `main.go` like the other extension points.
4. **Add a second provider.** OpenAI, a local Ollama, or a `MockProvider` that records calls (most useful for the next exercise).
5. **Add tests.** With `MockProvider` you can test the agent loop end-to-end without an API call. Compaction strategies are easy to test on synthetic message histories.

## What's not in here yet

- **Streaming.** The model returns a full response before we render anything. Real coding agents stream tokens as they arrive.
- **Tests.** Nothing's automated yet — see exercise 5.
- **Prompt caching.** Every turn re-sends the full history at full price.
- **Multi-line input.** Bubble Tea's `textarea` would unlock Shift-Enter for newlines.
- **Permission policies.** Approval is hardcoded as "ask every time" — see exercise 3.
- **MCP support.** No external tool servers.

Each one is a worthwhile next chapter.

## Documentation

Two flavors of docs, for the two reasons people read this repo:

- [`follow_along/`](follow_along/en/README.md) — chapter-length narrative on the *why* of every layer in the harness, in the order it was built. Read in order; about an hour total. Available in [English](follow_along/en/README.md) and [Spanish](follow_along/es/README.md).
- [`how-to/`](how-to/en/README.md) — short recipe-style references for the extension tasks people actually do: [add a tool](how-to/en/add-a-tool.md), [add a provider](how-to/en/add-a-provider.md), [add a permission policy](how-to/en/add-a-permission-policy.md). Also bilingual.

If you're learning, start with `follow_along/`. If you've already built it and want to extend it, jump to `how-to/`.

## Acknowledgments

The structure draws on architectural decisions visible in Claude Code, OpenCode, and Aider. The "build your own X" framing comes from *Build Your Own Redis*, *Crafting Interpreters*, and Thorsten Ball's *Writing An Interpreter In Go*.
