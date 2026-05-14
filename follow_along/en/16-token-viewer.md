# 16 · The token viewer

We are going to implement this:

<img width="298" height="129" alt="bettatech-tui-cost" src="https://github.com/user-attachments/assets/b5b81e52-8d34-4375-ba67-0394c33b74e8" />

Every API call returns a `usage` object: how many input tokens it cost, how many output tokens it produced, how many were served from the prompt cache. The harness has been throwing that information away since chapter 01. This chapter wires it through.

The result: a `/tokens` command that prints a cumulative breakdown, and a live status line at the bottom of the TUI that updates after every turn. Roughly 80 lines of code across four files.

## Why this matters

Two reasons, neither vanity:

1. **Cost intuition.** Until you watch the number tick up, "1,000 tokens" is abstract. Once you see a single conversation hit 200 K tokens after twelve turns and twenty file reads, you start writing tighter prompts and configuring compaction earlier.
2. **Debugging.** When the agent feels slow or expensive, the token panel tells you where the budget went — cache misses, runaway tool outputs, an `AGENTS.md` you forgot to trim.

The feature is also a useful tour of how non-interface, provider-specific data flows up through the harness's layers. Worth doing once.

## What the API gives us

The Anthropic Messages API returns this on every response:

```json
{
  "usage": {
    "input_tokens": 1234,
    "output_tokens": 567,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 0
  }
}
```

Cache fields are zero unless prompt caching is enabled. Pricing differs by category — cache reads are ~10× cheaper than fresh input tokens — so we track all four separately.

## Plumbing it through

Three steps, top to bottom:

### 1. `api.Usage` and `Response.Usage`

A new struct in `internal/api/types.go`:

```go
type Usage struct {
    InputTokens         int
    OutputTokens        int
    CacheCreationTokens int
    CacheReadTokens     int
}

func (u Usage) Add(other Usage) Usage { /* per-field sum */ }
```

And one field on `Response`:

```go
type Response struct {
    Content    []Block
    StopReason StopReason
    Usage      Usage          // ← new
}
```

`Usage` lives in `api` because it's the provider-agnostic shape — any backend that has a "tokens" concept can fill it in. OpenAI's `usage.prompt_tokens` / `completion_tokens` map cleanly; a hypothetical local-model provider would just leave it zero.

### 2. The provider populates and accumulates

The Anthropic adapter copies usage out of the SDK's response and folds it into a session-wide running total:

```go
type AnthropicProvider struct {
    // … existing fields …
    mu    sync.Mutex
    total api.Usage          // cumulative across every Send call
}

// In Send, after the API call:
out.Usage = api.Usage{
    InputTokens:         int(resp.Usage.InputTokens),
    OutputTokens:        int(resp.Usage.OutputTokens),
    CacheCreationTokens: int(resp.Usage.CacheCreationInputTokens),
    CacheReadTokens:     int(resp.Usage.CacheReadInputTokens),
}
p.mu.Lock()
p.total = p.total.Add(out.Usage)
p.mu.Unlock()
```

The mutex matters because subagents (chapter 11) run on goroutines that may share the provider with the root agent. Two simultaneous `Send` calls would race on the unprotected total. A plain `sync.Mutex` is enough — we lock for nanoseconds, no contention worth measuring.

Two new methods on the provider expose totals and dollar cost:

```go
func (p *AnthropicProvider) TotalUsage() api.Usage { /* mutex-guarded read */ }
func (p *AnthropicProvider) EstimatedCostUSD() float64 { /* total × pricing table */ }
```

### 3. Pricing table

Cost is per-model. We hardcode the rates the harness knows about:

```go
type pricing struct {
    InputPerMillion         float64
    OutputPerMillion        float64
    CacheCreationPerMillion float64
    CacheReadPerMillion     float64
}

var modelPricing = map[string]pricing{
    "claude-opus-4-7":   {15.00, 75.00, 18.75, 1.50},
    "claude-sonnet-4-6": {3.00,  15.00, 3.75,  0.30},
    "claude-haiku-4-5":  {1.00,  5.00,  1.25,  0.10},
}
```

Unknown models return `-1` from `EstimatedCostUSD()`. The slash command and the status line render that as `(unknown model)` rather than printing $0.0000 — silent zeros are worse than a visible blank.

## Why these methods stay off the `Provider` interface

The `Provider` interface in chapter 03 was deliberately minimal:

```go
type Provider interface {
    Send(...) (Response, error)
    Model() string
    SetModel(name string)
}
```

`TotalUsage()` and `EstimatedCostUSD()` are not on it. Two reasons:

1. **Not every backend has a token concept** in the same shape. A streaming-only provider might report tokens differently; a local-model provider might not report them at all. Forcing the interface to have these methods means every implementation has to implement them, even as no-ops.
2. **The harness calls them in exactly one place** — `main.go`'s `usageFunc`. We know we have an `AnthropicProvider` there because that's what we constructed. No assertion needed.

This is the **structural typing** idiom you can lean on in Go: optional capabilities go on the concrete type; callers who need them either know the concrete type or do a type assertion. The interface stays narrow.

## The slash command

`/tokens` calls the methods directly (since `rootAgent.Provider` is typed as the interface, we type-assert through a small contract):

```go
type tokenReporter interface {
    TotalUsage() api.Usage
    EstimatedCostUSD() float64
}

func cmdTokens(_ string) {
    stats, ok := rootAgent.Provider.(tokenReporter)
    if !ok {
        fmt.Println(ui.Dimmed("this provider doesn't report token usage"))
        return
    }
    // pretty-print stats.TotalUsage() and stats.EstimatedCostUSD()
}
```

The `tokenReporter` interface exists only inside `commands.go`. Defining it inline is the Go idiom for "I want to ask if this value can do X" without coupling the producer and consumer. Same pattern as `io.Reader` vs `bytes.Buffer` — buffers don't import `io` to satisfy `Reader`; the interface is the consumer's contract.

Output:

```
session usage:
  input          12,034
  output         3,891
  est. cost      $0.4729
```

Cache lines only print when cache is non-zero — visual noise costs more than the missing detail.

## The live status line

Chapter 12 reserved one line above the input box for the spinner. When the agent is idle, that line was blank. We reuse it for usage.

```go
switch m.state {
case stateRunning:
    statusLine = "  " + m.spinner.View() + " " + Dimmed("thinking..."+activeSubagentSummary())
case stateIdle:
    statusLine = "  " + Dimmed(m.usageStatus())
}
```

`usageStatus()` formats the same numbers as the slash command, in one line:

```
  12,034 in · 3,891 out · ~$0.4729
```

With cache activity, it adds a `cache 0/1,200` term in the middle.

### Getting the numbers into the model

The TUI doesn't know about providers. It receives a `UsageFunc` at construction:

```go
type UsageFunc func() (api.Usage, float64)

func NewProgram(runner AgentRunner, usageFunc UsageFunc) *tea.Program { /* … */ }
```

`main.go` provides the closure:

```go
usageFunc := func() (api.Usage, float64) {
    return llm.TotalUsage(), llm.EstimatedCostUSD()
}
program := ui.NewProgram(runner, usageFunc)
```

The closure is called from `View()` every time the TUI re-renders the input area. That happens on every keystroke, every appended message, every tick — but the cost is one mutex lock and four int reads. Cheap enough to not bother caching.

### Why a closure and not a Bubble Tea message

We could have sent a `UsageMsg` after every turn, updating a field on the model. That'd be more Bubble-Tea-idiomatic. The closure is simpler and the data is read-only — the TUI never mutates it — so there's no benefit to the message pattern.

The general rule: **`tea.Msg` is for state changes you want the Update loop to react to. A closure that returns a snapshot is fine when the only consumer is `View`.**

## Pitfalls

**Currency drift.** Rates change. Hardcoded prices in `modelPricing` will go stale, sometimes by a lot. Mark them with a date comment, prefix displayed costs with `~`, and put the source URL in the comment so reviewers know where to update.

**Token estimates from the model.** Don't confuse this with `client.Messages.CountTokens(...)`, which estimates *before* a call. Our token viewer reports *actuals* the API returned. The estimate API exists if you want a "this turn would cost ~X" hint before sending; we don't use it here.

**Multi-provider sessions.** If you swap providers mid-session (`/model` swaps within Anthropic, but imagine a hypothetical `/provider openai`), the running total is provider-specific. Splitting subtotals by provider is a sensible extension; we don't do it.

**Streaming.** When you add streaming (chapter 13), the SDK delivers `usage` only on the final `message_stop` event. Move the accumulator update there. The agent loop's per-turn cost-printing doesn't change.

> **In the current repo.** The pieces of this chapter live in:
>
> - [`internal/api/types.go`](../internal/api/types.go) — `Usage` struct and `Response.Usage` field.
> - [`internal/provider/anthropic.go`](../internal/provider/anthropic.go) — accumulator, mutex, `TotalUsage()`, `EstimatedCostUSD()`, the `modelPricing` table.
> - [`commands.go`](../commands.go) — `/tokens` command and the inline `tokenReporter` interface.
> - [`internal/ui/program.go`](../internal/ui/program.go) — `UsageFunc`, the harness field, `usageStatus()`, the status-line rendering.

## Now try

1. Run the harness, ask it a small question, then `/tokens`. Compare the input/output split — system prompt + tool definitions add up.
2. Have a longer conversation (10+ turns). Watch the live counter on the status line tick up between turns. Notice how subagent calls add big input chunks (the research prompt + tool descriptions sent to the subagent).
3. Set `/model claude-haiku-4-5` and ask the same question again. The cost line should drop 15×. The token counts barely change — the difference is entirely the pricing table.

← [back to TOC](README.md)
