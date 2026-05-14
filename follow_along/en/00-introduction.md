# 00 · Introduction

## What we're building

A coding agent in a terminal. You type a question or a task; the agent calls tools (`bash`, `read_file`, `write_file`) to act on your filesystem; it can delegate read-only investigation to a subagent; long conversations get compacted automatically. About 1,000 lines of Go.

The build target looks like this when you run it:

<img width="885" height="332" alt="bettatech-tui" src="https://github.com/user-attachments/assets/c726f9c6-466b-4193-8f24-5a4bb9e96994" />

There's nothing exotic in here. It's a model, a loop that calls it, some tools the model can use, and a UI that lets a human steer it. The interesting thing is the *seams* — where one piece ends and the next begins.

## What "harness engineering" is

The model is the engine. The **harness** is everything else: the loop that calls the model, the tools it can use, how the conversation is shaped over time, what it's allowed to do, how you talk to it.

The discipline matters because the same model behind two different harnesses behaves like two different products. Claude Code, OpenCode, Aider, and Cursor all use roughly the same family of models. Their personalities — fast or careful, transparent or opaque, capable or cautious — live in their harnesses. Get the harness right and a mid-tier model feels great; get it wrong and a frontier model feels broken.

### Three layers

Harness engineering happens at three layers, all using the same discipline:

| Layer | What you touch |
|---|---|
| **Building** | The code: agent loop, provider, tool registry, compaction |
| **Extending** | New code that plugs into existing abstractions — a new tool, a new subagent, an MCP integration |
| **Configuring** | Files the harness reads and prompts you write — `AGENTS.md`, slash-command palettes, permission policies, SDD workflows |

Building lets you change anything; configuring composes faster. Most practitioners spend ~1% of their time building, ~10% extending, and the rest configuring — that's where the leverage lives. A poorly-configured Claude Code with a 50 KB self-contradicting `AGENTS.md` feels exactly as broken as a poorly-built harness; getting either right is the same skill.

This book emphasizes building because that's where the mental model is forged. Once you understand *why* an `executeTool` wrapper sits between the agent and the registry, you read every config file with new eyes.

This project is a stripped-down, readable version of that kind of harness, designed to be poked at.

## Why a "build your own" book

The big lessons in harness engineering aren't visible in finished products. By the time you see a polished tool, you can't tell *why* its tool surface looks the way it does — why three dedicated tools instead of one `bash`, why approval is per-call instead of per-session, why compaction is client-side instead of server-side. These are decisions, not facts. The way you internalize a decision is to make it yourself.

So each chapter introduces one piece of the harness, explains the alternatives we considered, picks one, and tells you what it costs.

## Prerequisites

- **Go 1.21+.** We use generics, the `max` builtin, and `golang.org/x/term`.
- **An Anthropic API key.** Get one at [console.anthropic.com](https://console.anthropic.com). Free tier is enough.
- **Comfort with reading Go.** You don't have to write it, but if you're going to learn the most, you'll write each chapter's code before peeking at HEAD.
- **A real terminal.** Some chapters render ASCII art and TUIs; the experience inside an IDE's "terminal panel" is sometimes laggy.

## How the book is structured

Three rough arcs:

| Chapters | Arc | What you build |
|---|---|---|
| 01–02 | The bare minimum | A REPL that calls Claude, runs tools, asks before destructive ones |
| 03–08 | Abstractions earn their keep | `Provider`, slash commands, compaction, a better input |
| 09–12 | The architecture pays off | Plug-and-play tools, `internal/` packages, subagents, a full TUI |

By the end of chapter 02 you have something that works. By the end of chapter 12 you have something that resembles a small Claude Code.

## A note on the model

We use `claude-opus-4-7` throughout. Opus 4.7 is documented to spawn fewer subagents and to be more literal about following system prompts than Opus 4.6 — these behaviors come up in chapters 07 and 11. If you're following along with a different model the prompts may behave differently; mostly that's fine.

## What this book doesn't cover

- Building a model. We use the Anthropic SDK; we treat the model as a black box.
- Production deployment, multi-user systems, persistence. The harness is local-only.
- Streaming token-by-token output. The harness waits for the full response before rendering.
- Permission policies more complex than "ask every time."

Chapter 13 talks about adding these.

Next: [01 · The agent loop](01-the-agent-loop.md).
