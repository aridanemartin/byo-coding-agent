# 09 · Plug-and-play tools

We've abstracted the LLM backend (chapter 03) and the compaction strategy (chapter 07). The next obvious gap is tools.

Right now tools live as two disconnected things: a `[]ToolDef` slice (sent to the model) and a `switch name {}` in `executeTool` (what gets run). Adding a tool means editing two places.

This chapter unifies them.

## The pattern, one more time

We've done this twice already:

```
interface → swap-friendly impls → one-line replacement
```

For tools, with one twist: tools are **additive** (you can have many at once), not exclusive (you have one provider, one compactor). So instead of "one line in main.go to swap," we get a **registry**:

```go
type Tool interface {
    Definition() api.ToolDef
    Execute(ctx context.Context, input string) (result string, isError bool)
}

type Registry struct {
    tools map[string]Tool
}

func (r *Registry) Register(t Tool) { r.tools[t.Definition().Name] = t }
func (r *Registry) Definitions() []api.ToolDef { /* sorted */ }
func (r *Registry) Execute(ctx context.Context, name, input string) (string, bool) { /* dispatch */ }

var Default = NewRegistry()
```

The agent loop calls `registry.Definitions()` for the API request and `registry.Execute(ctx, name, input)` for dispatch. It doesn't know which tools exist.

## Self-registration via `init()`

The cherry on top: tools register themselves when the file is loaded. Each tool gets its own file:

```go
// internal/tool/bash.go
package tool

type BashTool struct{}

func init() { Default.Register(&BashTool{}) }

func (BashTool) Definition() api.ToolDef { /* schema */ }
func (BashTool) Execute(ctx context.Context, rawInput string) (string, bool) { /* impl */ }
```

To add a new tool:

1. Drop a file in `internal/tool/` with `package tool` at the top.
2. Implement `Tool` (two methods).
3. Add `func init() { Default.Register(&YourTool{}) }`.

That's it. **No edits to `main.go`.** When the package loads, Go runs every file's `init()`, every tool registers, the agent sees them all.

This trick — using `init()` for self-registration — is the same one `database/sql` drivers use. The "drop a file in, it appears" workflow is one of the more pleasant patterns in Go.

## Why it works (and doesn't always)

It works **because every tool file is in the same package.** When `main` imports `internal/tool`, Go compiles every file in that directory, runs every `init()`, registers every tool.

If you split tools into subpackages (`internal/tool/bash/`, `internal/tool/read_file/`), you'd lose the trick: `main` would have to do `import _ "internal/tool/bash"` for each one to trigger that package's `init()`. The "list every tool somewhere" problem comes back. We deliberately kept all tools in one folder to preserve the property.

## What you can do with this

Try it. Make a new file:

```go
// internal/tool/gitdiff.go
package tool

import (
    "context"
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

func (GitDiffTool) Execute(ctx context.Context, _ string) (string, bool) {
    out, err := exec.CommandContext(ctx, "git", "diff").CombinedOutput()
    if err != nil { return string(out), true }
    return string(out), false
}
```

Run `go run .`, type `/tools`. `git_diff` is in the list. The model can call it.

## The cleanup `executeTool` got

After this refactor, `executeTool` in `main.go` drops from a big switch to a thin wrapper:

```go
func executeTool(name, rawInput string) (string, bool) {
    fmt.Printf("[tool] %s %s\n", name, rawInput)
    if !confirm("approve?") {
        return "user denied this tool call", true
    }
    return registry.Execute(ctx, name, rawInput)
}
```

Three concerns separated:

| Concern | Owner |
|---|---|
| Logging | `main` (the `[tool] …` print) |
| Approval | `main` (calls `confirm`) |
| Dispatch | `registry.Execute` |

Each tool's behavior is in its own file. Each cross-cutting concern is in `main`. This is the shape you want: a thin top layer that knows about every extension point and gates them, plus a fat collection of small files that don't know about each other.

## Tool inputs and the `context.Context` argument

Once we get serious, every tool's `Execute` takes a `context.Context`. `bash` actually uses it (`exec.CommandContext`) so a long-running command can be cancelled when the agent is interrupted. Others (`read_file`, `write_file`) accept it and ignore.

Threading context through every layer (agent → registry → tool) is a Go idiom. It pays off when you add timeout/cancellation later. Better to add it now than retrofit.

## Pitfalls

**Map iteration is random.** `Definitions()` would return tools in a random order if you iterated `r.tools` directly. Two API calls would serialize differently, breaking prompt caching. The fix is one extra line: sort by name first.

```go
sort.Strings(names)
for _, n := range names {
    out = append(out, r.tools[n].Definition())
}
```

**`Default` is a global.** Like all globals, it's a tempting place to hang state. Resist. Tools should be small and focused. If a tool needs configuration, take it as a struct field and let `main` construct it — see the delegate tool in chapter 11 for an example.

**Auto-registration vs explicit registration.** `init()` works great for tools that are pure types (no configuration needed). For tools that need a `Provider`, a `Config`, or any other runtime dependency, you can't auto-register — you need an explicit `registry.Register(&MyTool{Provider: llm})` in `main`. We hit this with subagents in chapter 11.

> **In the current repo.** Everything tool-related lives in [`internal/tool/`](../internal/tool/):
>
> - [`registry.go`](../internal/tool/registry.go) — the `Tool` interface, `Registry` struct, and the global `Default` registry
> - [`bash.go`](../internal/tool/bash.go), [`readfile.go`](../internal/tool/readfile.go), [`writefile.go`](../internal/tool/writefile.go) — one file per tool, each with a 1-line `init()` that registers itself
>
> The agent loop in [`internal/agent/agent.go`](../internal/agent/agent.go) calls `a.Tools.Definitions()` for the API request and `a.Tools.Execute(ctx, name, input)` for dispatch. It doesn't know which tools exist — that's the whole point.

## Now try

1. Add a `web_fetch` tool that takes a URL and returns the response body. (Use `net/http`. Set a timeout.)
2. Read `internal/tool/registry.go` and find the `Subset(...names)` method. It returns a new Registry containing only the named tools. Then read `internal/subagent/research.go` to see why that matters (next chapter).
3. Try registering two tools with the same `Name()`. What happens? Where in the registry would you add a duplicate-check?

Next: [10 · Project structure](10-project-structure.md).
