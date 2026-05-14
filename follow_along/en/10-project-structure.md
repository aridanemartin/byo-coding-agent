# 10 · Project structure

By chapter 09, the repo has 16 Go files at the top level. Everything is `package main`. It works, but a new reader has to map files to concepts themselves.

This chapter is about moving things into folders — specifically, into `internal/` packages. It's a contentious refactor because **flat is more idiomatic in Go** than people think; we're going to do it anyway, and pay the costs in plain sight.

## What "best practice" actually says

Go's project layout philosophy: package layout reflects domain boundaries, not file types. The standard library, `kubectl`, `chi`, and most popular Go projects keep many files in one directory and let packages emerge based on real semantic boundaries.

A 600-line REPL with one binary really does fit fine in one package. We're splitting it anyway, because **the BYO framing has a different goal**: a learner cloning the repo needs to map files to concepts visually. Folders make "this is where tools live, this is where providers live" obvious without reading file headers.

So this is a deliberate tradeoff: less idiomatic Go, more navigable repo. If you're using this codebase as a model for production Go, weigh accordingly.

## The target layout

```
.
├── main.go              wiring + REPL + agent loop + executeTool wrapper
├── commands.go          slash command registry (lives in main; touches everything)
└── internal/
    ├── api/             Message, Block, ToolDef, Response, RenderTranscript
    ├── provider/        Provider interface + AnthropicProvider
    ├── tool/            Tool interface + Registry + bash / readfile / writefile
    ├── compact/         CompactionStrategy + Sliding / Summarize / Logging
    └── ui/              banner, spinner, input (Bubble Tea), styling helpers
```

`internal/` is enforced by the Go compiler — code under it can only be imported by packages within the same module. That's the right signal for "these aren't public APIs, they're implementation."

## What had to change

Three categories of mechanical edits:

### 1. Shared types move to `internal/api/`

`Message`, `Block`, `ToolDef`, `Response`, `StopReason`, plus helpers like `RenderTranscript` and `Message.HasToolResult`. The constants get exported (`api.RoleUser`, `api.BlockText`). This is the layer everything else depends on, no dependencies of its own.

### 2. Cross-package types get capitalized

`type provider` → `type Provider`. `type compactionStrategy` → `type CompactionStrategy`. `safeSplitPoint` → `SafeSplitPoint`. Anything that gets called from outside the package has to be exported. This is the largest mechanical chunk; mostly find-and-replace.

### 3. Variable renames to avoid shadowing

The package `provider` now exports `Provider`. A variable named `provider` in `main` shadows the package import:

```go
import "github.com/betta-tech/byo-coding-agent/internal/provider"

var provider provider.Provider   // ← compile error, kind of
```

So we rename the variable. We picked `llm`:

```go
var llm provider.Provider
```

Reads naturally — `llm.Send(...)`, `llm.SetModel("...")`. Trivial change once, but you have to do it everywhere.

## What didn't change

The architecture. The whole point of the refactor is to *expose* what was already there. Three extension points (Provider, Tool, CompactionStrategy) were always conceptually separate; now they're physically separate too.

The agent loop, the executeTool wrapper, the commands — all still live in `package main`. Specifically:

- `main.go` keeps the agent loop and the wiring.
- `commands.go` keeps the slash commands because they touch every extension point — putting them elsewhere would require passing all state through, or making everything global *and* exported. Better to keep the integration layer at the top.

## Why `internal/` and not just `pkg/`

`internal/` is **compiler-enforced**: anything under it can only be imported by packages within the same module tree. If someone else `go get`s this repo as a library, they can't depend on `internal/tool/`. This is the right signal for "stable harness API: there is none."

`pkg/` is convention — older Go projects use it for "code that's meant to be imported," but Go itself doesn't enforce anything. For a binary that's not meant to be reused as a library, `internal/` is correct.

## Module path

Module path changes from `harness` to `github.com/betta-tech/byo-coding-agent` to match the GitHub URL. This isn't strictly necessary (you can have any module name), but matching the URL is the convention, and it saves future renames if/when someone forks.

```sh
go mod edit -module github.com/betta-tech/byo-coding-agent
```

Then every internal import becomes:

```go
import "github.com/betta-tech/byo-coding-agent/internal/api"
```

Long imports are the cost of fully-qualified module paths. Editors auto-complete them; humans grep for the last component.

## Pitfalls

**Import cycles.** The biggest risk. The first cycle we hit was:

```
agent → ui → subagent → agent
```

(`agent` used `ui.StartSpinner`; `ui` needed `subagent.Active()` for the status bar; `subagent.Research` constructed an `Agent`.)

The fix was to make `agent` not import `ui` — drop the spinner call from the agent loop (status bar replaces it in chapter 12), and inline a plain-text compaction diff. Cleaner dependency direction anyway: agent depends only on api, compact, provider, tool.

Rules of thumb to avoid these:

- `api` depends on nothing. It's the bottom of the dependency stack.
- Logic packages (`agent`, `compact`) depend on `api` and each other selectively. They never depend on UI.
- UI packages depend on logic packages and on `api`, never the reverse.

**Where DelegateTool lives.** We don't have it yet (chapter 11 introduces it), but spoiler: `DelegateTool` ends up in `main` rather than `internal/tool/` to avoid `tool → subagent → agent → tool`. Sometimes the right answer is "don't put it in the obvious package."

**Tests are now per-package.** With a flat layout, `internal_test.go` could touch anything. With `internal/` packages, you write tests per package, and exported APIs are the only thing reachable from other packages' tests. That's good discipline but a change.

## What we kept simple

We did **not** create subpackages within `internal/provider/anthropic/` or `internal/tool/bash/`. There's just `internal/provider/` (with both the interface and the Anthropic impl) and `internal/tool/` (with the interface and every tool as a file).

The deeper nesting would have been more "idiomatic" in some senses but would have killed the self-registration trick (every subpackage would need its own import in `main`). The flatter layout preserves "drop a file in, it appears."

> **In the current repo.** The layout in this chapter is exactly what's at HEAD. Walk it from the top:
>
> - [`main.go`](../main.go) + [`commands.go`](../commands.go) + [`delegate.go`](../delegate.go) — the wiring layer
> - [`internal/api/`](../internal/api/) — shared types, no internal dependencies
> - [`internal/provider/`](../internal/provider/) — Provider interface + Anthropic impl
> - [`internal/tool/`](../internal/tool/) — Tool interface + registry + each tool
> - [`internal/compact/`](../internal/compact/) — strategies + decorator
> - [`internal/agent/`](../internal/agent/) — the agent struct (added in chapter 11)
> - [`internal/subagent/`](../internal/subagent/) — subagent abstraction (chapter 11)
> - [`internal/ui/`](../internal/ui/) — banner, spinner, Bubble Tea program (chapter 12)

## Now try

1. Pretend you're a new reader. Without looking at `main.go`, navigate the repo and try to write down: where does the agent loop live? Where do tools live? Where does the API translation happen? If the structure is clear, you should answer in under a minute.
2. Run `go mod why github.com/anthropics/anthropic-sdk-go` and trace the import path. Only one package should depend on it.
3. Try to add a new package `internal/cache/` that depends on `internal/agent`. Does anything break? (It shouldn't — agent doesn't depend on cache, so no cycle.) Now reverse it. (Now you have agent → cache, which is fine if cache doesn't depend on agent.)

Next: [11 · Subagents](11-subagents.md).
