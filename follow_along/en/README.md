# Follow-Along

A chapter-by-chapter walk through how this harness came together. The intent is **the story of why**, not a line-by-line recap — the repo at HEAD is the source of truth for what the code looks like; these chapters explain how each layer was decided on and what it costs to skip.

Read in order. Each chapter is short (5–10 min) and focuses on one design decision.

## Chapters

| # | Title | What it covers |
|---|---|---|
| [00](00-introduction.md) | Introduction | What "harness engineering" is and why a learning project belongs in this space |
| [01](01-the-agent-loop.md) | The agent loop | Bare REPL + Anthropic SDK + three tools + the `executeTool` switch |
| [02](02-the-permission-gate.md) | The permission gate | Why approval lives at the harness layer, the "error as tool result" contract |
| [03](03-the-provider-interface.md) | The provider interface | Extracting an LLM abstraction so the harness isn't married to one SDK |
| [04](04-ui-polish.md) | UI polish | ASCII banner, terminal width detection, loading spinner |
| [05](05-slash-commands.md) | Slash commands | A small command palette: `/help`, `/model`, `/clear`, `/tools`, `/exit` |
| [06](06-conversation-state.md) | Conversation state | The API is stateless; the client carries everything |
| [07](07-compaction-strategies.md) | Compaction strategies | An interface for handling long conversations; sliding window vs summarization; the logging decorator |
| [08](08-better-input.md) | Better input | From `bufio.Scanner` → readline → a Bubble Tea bordered input box |
| [09](09-plug-and-play-tools.md) | Plug-and-play tools | A `Tool` interface, a `Registry`, and `init()` self-registration |
| [10](10-project-structure.md) | Project structure | Why we moved to `internal/` packages — and what it cost |
| [11](11-subagents.md) | Subagents | Extracting `Agent` as a struct, the `Subagent` abstraction, the delegate tool |
| [12](12-full-tui.md) | The full TUI | A proper Bubble Tea program with viewport, scrollback, approval flow |
| [13](13-whats-next.md) | What's next | Exercises, what we deliberately skipped, and where to take it |

## Extras

Standalone chapters that extend the harness with concrete integrations. They build on the architecture from chapters 01–13 but aren't part of the core arc — read them when you want the feature, skip them when you don't.

| # | Title | What it covers |
|---|---|---|
| [14](14-mcp-support.md) | Adding MCP support | Wrapping remote Model Context Protocol servers behind the `Tool` interface |
| [15](15-agents-md.md) | Project context with AGENTS.md | Loading a project-specific markdown file into the system prompt at startup |
| [16](16-token-viewer.md) | The token viewer | Tracking session usage and cost, `/tokens` command and a live status line |
| [17](17-prompt-caching.md) | Prompt caching | What it is, what invalidates it, and the one-line change to turn it on |
| [18](18-diff-approval.md) | Diff approval for writes | Showing a unified diff in a modal before `write_file` touches disk |
| [19](19-agent-memory.md) | Agent memory | A `Store` interface and a session-files implementation so the agent remembers across runs |

## How to read this

If you want the **fastest tour**: read chapter 00, then skim chapters 01, 09, 11 (the three core abstractions: agent loop, tools, subagents). Everything else is layered polish.

If you want to **build it yourself**: read in order, write the code for each chapter, then diff your work against the repo at HEAD before moving on.

## A note on snippets

Code blocks in these chapters are pedagogical — they show the *shape* of the code at the relevant stage. The repo at HEAD has additional layers (the `Tool` interface, the `internal/` packages, the full TUI) that get introduced gradually. Don't be surprised if a chapter 02 snippet doesn't match `main.go` line-for-line — by chapter 12 we'd refactored most of it.
