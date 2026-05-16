# 11 · Subagents

By chapter 10 the harness has clean seams everywhere: provider (chapter 03), compaction (chapter 07), tools (chapter 09), and `internal/` packages giving each layer a home. What it still can't do is *delegate*. Every tool call lands back in the main conversation. Reading twenty files to answer one question pollutes the context with twenty tool-result blocks; the main loop never recovers focus. This chapter introduces the abstraction that fixes it.

A subagent is a separate agent loop, spawned by the main agent, with its own context window and tool subset, that returns its final answer as a single tool result.

Why bother:

- **Research without polluting context.** Reading 20 files to find one answer uses 20 tool-result blocks in the main conversation. A research subagent reads those 20 files, returns one synthesized answer, and the main agent sees that one line.
- **Specialization.** A code-review subagent has different tools and prompts than a refactoring subagent. The main agent picks the right specialist.
- **Cost control.** Subagents can use cheaper models. (Our implementation doesn't, but the architecture supports it trivially.)

This chapter is two refactors and one new feature, in order.

## Refactor 1: extract `Agent` into a struct

Right now `agentLoop` is a function that reads package-level globals (`provider`, `messages`, `compactor`, `tools`). To have *two* agents (root + subagent), each needs its own state.

```go
// internal/agent/agent.go
type Agent struct {
    Name      string
    Provider  provider.Provider
    Tools     *tool.Registry
    Compactor compact.CompactionStrategy
    System    string
    MaxTurns  int
    Verbose   bool
    LogPrefix string
    Quiet     bool
    Confirm   func(prompt string) bool

    messages []api.Message
}

func New(p provider.Provider, system string, tools *tool.Registry) *Agent { /* … */ }
func (a *Agent) Send(ctx context.Context, prompt string) (string, error) { /* … */ }
func (a *Agent) Messages() []api.Message { return a.messages }
func (a *Agent) ClearMessages()            { a.messages = a.messages[:0] }
func (a *Agent) SetMessages(m []api.Message) { a.messages = m }
```

`Send` appends the prompt you pass in as a user-role message and then runs the loop. The loop logic is what `agentLoop` used to be — moved verbatim, with `provider` → `a.Provider`, `messages` → `a.messages`, etc.

The `LogPrefix` and `Quiet` fields are how we distinguish root from subagent visually:

| | Root agent | Subagent |
|---|---|---|
| `LogPrefix` | `""` | `"  ↳ "` |
| `Quiet` | `false` | `true` |
| `Confirm` | `ui.Confirm` (asks you) | `nil` (auto-approve) |
| `Name` | `""` | `"research"` |

So tool calls indent (`  ↳ [tool] read_file ...`), assistant text is silent (the subagent's working commentary doesn't echo back; we just return the final answer), and approval is implicit — you approving the parent delegate call counts as approval for everything inside.

## Refactor 2: the `Subagent` abstraction

Same pattern as Provider, Tool, CompactionStrategy:

```go
// internal/subagent/registry.go
type Subagent interface {
    Name() string
    Description() string
    Run(ctx context.Context, task string) (string, error)
}

type Registry struct { /* … */ }
var Default = NewRegistry()
```

Plus a tracker — `Begin(name)` / `Active()` — for the UI to show what's running:

```go
func Begin(name string) func() {
    trk.mu.Lock()
    trk.active[name]++
    trk.mu.Unlock()
    return func() {
        trk.mu.Lock()
        trk.active[name]--
        // …
        trk.mu.Unlock()
    }
}
```

Subagents call `defer Begin(name)()` at the top of `Run`. The TUI status indicator reads `Active()` to display in-flight subagents.

One concrete subagent, `Research`, lives in `internal/subagent/research.go`. It constructs an `Agent` with read-only tools, runs once, returns the result:

```go
type Research struct {
    Provider provider.Provider
    Tools    *tool.Registry  // curated subset
}

func (Research) Name() string        { return "research" }
func (Research) Description() string { /* … */ }

func (r Research) Run(ctx context.Context, task string) (string, error) {
    defer Begin(r.Name())()
    a := agent.New(r.Provider, researchSystem, r.Tools)
    a.Name = r.Name()
    a.LogPrefix = "  ↳ "
    a.Quiet = true
    a.MaxTurns = 10
    return a.Send(ctx, task)
}
```

## The delegate tool

How does the main agent *invoke* a subagent? Through a tool, like everything else:

```go
type DelegateTool struct {
    Subagent subagent.Subagent
}

func (d *DelegateTool) Definition() api.ToolDef {
    return api.ToolDef{
        Name:        "delegate_" + d.Subagent.Name(),
        Description: d.Subagent.Description(),
        InputSchema: map[string]any{
            "task": map[string]any{"type": "string", "description": "…"},
        },
        Required: []string{"task"},
    }
}

func (d *DelegateTool) Execute(ctx context.Context, rawInput string) (string, bool) {
    var in struct{ Task string `json:"task"` }
    json.Unmarshal([]byte(rawInput), &in)
    fmt.Println(ui.Dimmed("↳ delegating to " + d.Subagent.Name() + " subagent"))
    result, err := d.Subagent.Run(ctx, in.Task)
    fmt.Println(ui.Dimmed("← " + d.Subagent.Name() + " subagent done"))
    if err != nil { return err.Error(), true }
    return result, false
}
```

`main` constructs a `DelegateTool` per subagent and registers it in `tool.Default`. The model sees `delegate_research` in its tool list and can choose to call it.

## Where DelegateTool lives (and why)

There's a subtle issue: `DelegateTool` needs `subagent.Subagent`. If we put it in `internal/tool/`, that creates `tool → subagent`. But `subagent` imports `agent`, and `agent` imports `tool`. We get a cycle:

```
tool → subagent → agent → tool
```

The fix: put `DelegateTool` in `main`. Main imports everything, no cycle. The `Tool` interface lives in `internal/tool/`, but anything can implement it.

This is a recurring pattern — the cleanest place for "glue" between two abstractions is often the top, not inside either abstraction.

## Why subagents don't auto-register

Tools self-register via `init()`. Why don't subagents?

Subagents need configuration: a `Provider`, sometimes a tool subset, sometimes a model. None of that exists at `init()` time — those are runtime values constructed in `main`.

So registration is explicit:

```go
// main.go
func registerSubagents(llm provider.Provider) {
    subagent.Default.Register(subagent.Research{
        Provider: llm,
        Tools:    tool.Default.Subset("read_file"),
    })
    for _, sa := range subagent.Default.All() {
        tool.Default.Register(&DelegateTool{Subagent: sa})
    }
}
```

Two lines per subagent — one to construct + register the subagent, one (implicit in the loop) to register its delegate tool. Less magic than the tool pattern, more honest about the dependency.

## Getting the model to actually delegate

The single biggest surprise in this chapter: **Opus 4.7 is biased against using subagents by default.** Documented behavior. A soft system prompt that says "use the subagent when many file reads are needed" gets interpreted as "just do it myself, it's fine."

The fix is in the prompt. Make it explicit:

> For READ-ONLY INVESTIGATION you SHOULD call delegate_research rather than reading files yourself. This includes questions like:
>
> - "where is X defined?"
> - "what fields does Y have?"
> - …
>
> Prefer delegating for investigation, even when you think one or two reads would do it.

And make the tool description imperative, not descriptive:

> Investigate the codebase or filesystem and return a focused answer. Prefer this over reading files yourself when the user asks ANY question about the code.

This is harness engineering in its most direct form: the model's apparent personality is yours to shape. If subagents aren't getting used, the problem is almost always your prompt, not the architecture.

## The visible result

When you ask "where is the agent loop defined?" — and the prompt is right — the conversation looks like:

```
[tool] delegate_research {"task":"locate the agent loop"}
approve? [y/n] y
↳ delegating to research subagent
  ↳ [tool] read_file {"path":"main.go"}
  ↳ [tool] read_file {"path":"internal/agent/agent.go"}
← research subagent done (2.1s)
The agent loop is in internal/agent/agent.go, line 88...
```

Three visible cues: the header, the indented tool calls, the footer with elapsed time.

## Pitfalls

**Synchronous-only subagents.** Our subagents run inline — the parent agent's tool call blocks until the subagent finishes. That means you can't fan out two subagents in parallel. Parallelism is doable but requires the parent to issue both tool calls in one turn, and the subagent registry to support concurrent `Run` calls (the tracker does; the rest is plumbing).

**Visibility while running.** Because the REPL is blocked while the agent runs, you can't type `/subagents` to see what's in flight. The `subagent.Active()` tracker is set up for it, but only the chapter-12 TUI actually displays the data in real time.

**Tool subsets.** `tool.Default.Subset("read_file")` constructs a new registry containing *only* `read_file`. The subagent only sees that subset, so it can't run shell commands. This is curation, not restriction — the registry is a regular value, not a permissions construct. If you need real restrictions (sandbox), they have to live in the tool's `Execute` itself.

> **In the current repo.** The pieces from this chapter:
>
> - The `Agent` struct: [`internal/agent/agent.go`](../internal/agent/agent.go). Read the field list at the top — `Name`, `LogPrefix`, `Quiet`, `Confirm` are the fields that differ between root and subagent.
> - The `Subagent` interface and the active-tracker: [`internal/subagent/registry.go`](../internal/subagent/registry.go).
> - The research subagent: [`internal/subagent/research.go`](../internal/subagent/research.go). Note the system prompt and how it's constructed with `LogPrefix: "  ↳ "`, `Quiet: true`, and `MaxTurns: 10`.
> - The delegate tool: [`delegate.go`](../delegate.go) (at repo root, not in `internal/tool/`, to avoid the import cycle described above).
> - Subagent registration: see `registerSubagents()` in [`main.go`](../main.go).

## Now try

1. Add a second subagent: `CodeReview`. Tools: `read_file`, `bash` (so it can run linters). System prompt: "You are a code reviewer. …". Register it. Confirm `delegate_codereview` appears in `/tools`.
2. Have a long conversation. Right after a delegate call, type `/subagents`. (You won't catch one in flight unless you're fast — but the registered subagents always show.)
3. Modify `Research.Run` so subagents respect a Confirm function passed from the parent. That's the path to *recursive* approval — every subagent tool call also asks you. Heavier UX, more visibility.

Next: [12 · The full TUI](12-full-tui.md).
