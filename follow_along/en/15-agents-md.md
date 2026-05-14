# 15 · Project context with AGENTS.md

The system prompt we wrote in chapter 01 tells the agent *how to behave* — be concise, prefer the research subagent, errors are tool results. It says nothing about *this codebase*. The agent doesn't know what `internal/tool/` is for, doesn't know that `delegate.go` lives outside `internal/` on purpose, doesn't know your conventions.

You could paste that context into every conversation. The convention people have converged on instead is **`AGENTS.md`** — a markdown file at the project root that AI tooling reads automatically and feeds to the model as project context. Claude Code calls its variant `CLAUDE.md`; the cross-tool version is `AGENTS.md`.

This chapter wires that into the harness.

## What AGENTS.md is for

Project-specific things the agent should always know but you don't want to re-state every turn:

- Repo layout and what each package is responsible for
- Build / test / lint commands
- Conventions ("errors as tool results, not Go errors")
- Known gotchas ("the delegate tool lives in main to avoid an import cycle")
- What this project *isn't* (so the agent doesn't propose features that don't fit)

Anything generic — "be concise," "prefer X over Y" — belongs in the system prompt the harness ships with. Anything project-shaped belongs in `AGENTS.md`. The split tracks the same line as `harness vs. model`: the harness owns reusable behavior, the project owns its own context.

## Where it slots in

The harness already has a system prompt. There are two reasonable ways to add `AGENTS.md` content:

| Approach | How | Tradeoff |
|---|---|---|
| **Append to the system prompt** | Read `AGENTS.md`, concatenate to `systemPrompt` before `provider.NewAnthropicProvider(...)` | Treated as instructions, sent every turn, plays well with prompt caching, survives `/clear`. ✓ |
| **Inject as the first user message** | Push `{Role: User, Content: "Project context:\n\n" + contents}` into `rootAgent.SetMessages(...)` before the REPL starts | Shows up in scrollback, the agent can quote it, but gets dropped by compaction and re-injected only if you do it again. |

We use the system-prompt approach. Same choice every other tool (Claude Code, Cursor, Aider) makes, for the same reasons: the content is *instructions about the codebase*, not part of the conversation.

## The implementation

About six lines in `main.go`:

```go
func loadAgentsContext() string {
    data, err := os.ReadFile("AGENTS.md")
    if err != nil {
        return ""
    }
    return "\n\n# Project context (from AGENTS.md)\n\n" + string(data)
}
```

And in `main()`, before constructing the provider:

```go
sysPrompt := systemPrompt + loadAgentsContext()
llm := provider.NewAnthropicProvider(anthropic.ModelClaudeOpus4_7, 8192, sysPrompt)
```

That's it. The Anthropic provider stores `sysPrompt` and sends it as the top-level `system` field on every request (chapter 06). The model sees one fused prompt; it doesn't know the prompt is two files glued together.

The wrapper text (`# Project context (from AGENTS.md)`) is a soft signal to the model that the second half is context, not instructions. Without it, the model treats every line equally, which is fine but slightly worse — it sometimes echoes "this codebase says…" instead of just acting on it.

## When the file gets read

`loadAgentsContext()` runs **once, at startup**, before `provider.NewAnthropicProvider`. Two consequences:

1. **Edit `AGENTS.md`, restart the harness.** No hot-reload, same as `mcp.json` (chapter 14). Adding a `/reload-context` slash command is a natural extension — call `provider.SetSystemPrompt(...)` after re-reading the file. We left the setter out for simplicity.
2. **The file is read from the working directory** — the project you're running the harness against, not the harness repo. Same `os.ReadFile("AGENTS.md")` pattern as `mcp.json`. If you want Claude Code's "walk up the directory tree" behavior, it's ~10 lines: start at CWD, walk parents until you find one or hit `/`.

## Two file-location interpretations

The same filename plays two roles depending on which project you're in:

| Role | Lives at | Who reads it |
|---|---|---|
| **Consumer side** | `your-project/AGENTS.md` (in whatever you're working on with the harness) | The harness's `loadAgentsContext`. |
| **Author side** | `byo-coding-agent/AGENTS.md` (root of the harness repo itself) | Any external agent (Claude Code, Cursor, this harness) that someone runs on the harness. |

These don't conflict — they're the same convention applied to different repos. The harness ships its own `AGENTS.md` at the repo root, so anyone cloning the repo to work on the harness itself gets project context for free, regardless of which agent they use.

## Why this is more powerful than it looks

Three things become possible once `AGENTS.md` is in the system prompt:

- **Project conventions become enforceable.** "Errors are returned as tool results, not Go errors" written in `AGENTS.md` means the model will catch itself when it drafts a `return err`. You don't have to remind it.
- **Onboarding new agents is one file.** Switching from this harness to Claude Code on the same repo? The agent gets the same context. No model retraining, no per-tool configuration.
- **Project memory.** Decisions you make during a refactor ("we moved X to Y because Z") can be appended to `AGENTS.md` so the next session inherits them. This is the same idea as Claude Code's "memory files" but file-based and reviewable in git.

## Pitfalls

**Size.** Every byte of `AGENTS.md` is paid for in input tokens on every API call. A 50 KB `AGENTS.md` will dominate your tokens and slow caching. Keep it under a few thousand tokens (maybe 5–10 KB). If you need more, link out to docs the agent can `read_file` on demand.

**Secrets.** Resist the temptation to put credentials, API keys, or environment-specific paths in `AGENTS.md`. The file is committed and ends up in every request. Use the existing env-var pattern from chapter 14 instead.

**Drift.** `AGENTS.md` rots like any other documentation. Treat it as code: review changes, prune outdated sections, delete instructions that no longer hold. An out-of-date `AGENTS.md` is worse than no `AGENTS.md` — the model will follow stale rules confidently.

**Cache invalidation.** If you turn on prompt caching (chapter 13's deferred topic), editing `AGENTS.md` invalidates the cached prefix on every request that follows the restart. That's fine — the rewrite is rare. But don't append a timestamp; you'd lose caching entirely.

> **In the current repo.** The loader is the `loadAgentsContext` function in [`main.go`](../main.go), called inline before [`provider.NewAnthropicProvider`](../internal/provider/anthropic.go). The repo's own [`AGENTS.md`](../AGENTS.md) sits at the project root.

## Now try

1. Drop a one-line `AGENTS.md` into the repo (`This project uses tabs, not spaces.`) and start the harness. Ask "write me a hello-world Go file." Watch the agent comply with the rule it wasn't told in the conversation.
2. Delete `AGENTS.md` and restart. Notice the difference in how the agent talks about the codebase — same questions, blanker answers.
3. Add a `/context` slash command that prints whatever `loadAgentsContext()` returned, so you can sanity-check what the model is actually seeing.

← [back to TOC](README.md)
