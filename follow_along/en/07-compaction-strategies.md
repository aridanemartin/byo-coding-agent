# 07 · Compaction strategies

Chapter 06 made the API's statelessness explicit: every call ships the entire conversation. That's our problem now.

Conversations grow. Each turn re-sends the whole history. By turn 30 you're paying real money to re-tokenize hours of past chat. By turn 100 you start running into the context window.

This chapter is the first time we have to *throw information away* — every prior chapter only added. Like providers (chapter 03), compaction is something you'd want to swap and experiment with, so it gets the same treatment: an interface, multiple implementations, one line in `main.go` to switch.

## The interface

```go
type CompactionStrategy interface {
    Compact(ctx context.Context, messages []api.Message) ([]api.Message, error)
}
```

Strategies are called at the top of every agent loop turn. Most of the time they return the input unchanged (the threshold isn't reached). When they do act, they return a shortened slice.

## Three strategies

| Strategy | What it does | When to use it |
|---|---|---|
| `NoCompaction` | Returns input unchanged | Default; you control the budget elsewhere |
| `SlidingWindow{KeepLast: N}` | Drops everything but the last N messages | Cheap, no API call, loses old context |
| `Summarize{Threshold, KeepRecent, …}` | Asks the model to summarize old turns, replaces them with the summary | Preserves earlier context as a synthetic message; costs one extra API call when it fires |

Plus a **decorator**:

| Wrapper | What it does |
|---|---|
| `WithLogging(inner, path)` | Wraps any strategy; logs before/after diffs to a file. Useful for comparing strategies. |

```go
// In main.go — swap the line to swap behavior
compactor = compact.NoCompaction{}
compactor = &compact.SlidingWindow{KeepLast: 10}
compactor = &compact.Summarize{Provider: llm, Threshold: 20, KeepRecent: 6}
compactor = compact.WithLogging(&compact.SlidingWindow{KeepLast: 10}, "compactions.log")
```

## The "safe split" trick

This is the one piece of compaction that's genuinely subtle.

A naive truncation can leave a `tool_use` block in the dropped portion and its matching `tool_result` in the kept portion. The next API call sees a `tool_result` with no preceding `tool_use` — and returns 400. The conversation is unrecoverable without manual surgery.

The fix: walk backwards from your desired split point until you find a "clean" boundary — a user message that contains text, not a tool_result reply. That marks the start of a fresh turn, which is always safe to split on.

```go
// SafeSplitPoint walks backward from `desired` to find an index where the
// conversation is in a "clean" state — no tool_use without its tool_result
// on either side of the split.
func SafeSplitPoint(messages []api.Message, desired int) int {
    if desired <= 0 { return 0 }
    if desired >= len(messages) { return len(messages) }
    for i := desired; i > 0; i-- {
        if messages[i].Role == api.RoleUser && !messages[i].HasToolResult() {
            return i
        }
    }
    return 0
}
```

Every strategy routes through this. If we can't find a safe boundary near where we wanted, we do nothing — better than a broken conversation.

## Summarization

The interesting strategy. When the conversation hits `Threshold`, we:

1. Find a safe split point near `len(messages) - KeepRecent`.
2. Take the old half (everything before the split).
3. Ask the model to summarize it. Use the *same* provider — recursive but bounded; the summarization call is one-shot, no tools, returns a single text response.
4. Replace the old half with a synthetic user message: `"[earlier conversation summary] ..."`.
5. Keep the recent half as-is.

```go
func (s *Summarize) Compact(ctx context.Context, messages []api.Message) ([]api.Message, error) {
    if len(messages) < s.Threshold { return messages, nil }
    split := SafeSplitPoint(messages, len(messages)-s.KeepRecent)
    if split == 0 { return messages, nil }
    old, recent := messages[:split], messages[split:]

    resp, err := s.Provider.Send(ctx, []api.Message{{
        Role:    api.RoleUser,
        Content: []api.Block{{Type: api.BlockText, Text: instructions + "\n\n" + api.RenderTranscript(old)}},
    }}, nil)
    if err != nil { return messages, fmt.Errorf("summarize: %w", err) }

    // … extract summary text, prepend as a synthetic message …
    return append([]api.Message{{Role: api.RoleUser, ...}}, recent...), nil
}
```

The model's summarization is bias-shaped by its system prompt — when summarizing in the context of "you are a coding assistant," it tends to preserve file paths, function names, and decisions, and drop chit-chat. That's a happy accident. A more rigorous design would let you override the summarization prompt explicitly.

## The decorator pattern

The cleanest piece of this design is `LoggingStrategy`. It implements `CompactionStrategy` *and* it wraps one:

```go
type LoggingStrategy struct {
    Inner    CompactionStrategy
    FilePath string
}

func (l *LoggingStrategy) Compact(ctx context.Context, messages []api.Message) ([]api.Message, error) {
    before := messages
    after, err := l.Inner.Compact(ctx, messages)
    if err != nil { return after, err }
    if len(after) != len(before) {
        l.writeEvent(before, after)
    }
    return after, nil
}
```

This is the **decorator pattern** in one struct. Logging is orthogonal to the strategy choice; it shouldn't require a `LogTo string` field on every strategy. Instead, *anything* that implements the interface can be wrapped. Future strategies need no logging-specific code; they get logging for free.

The same pattern shows up everywhere in well-designed systems: HTTP middleware, observability wrappers, retry decorators. Worth recognizing once so you can use it always.

## The slash command for testing

`/compact [strategy]` and `/verbose` exist mostly so you can experiment without restarting the harness:

| Command | Effect |
|---|---|
| `/compact` | Run the configured strategy now |
| `/compact sliding` | Run an ad-hoc `SlidingWindow{KeepLast: 6}` |
| `/compact summarize` | Run an ad-hoc `Summarize{Threshold: 0, …}` (Threshold: 0 forces it to fire) |
| `/compact none` | Run `NoCompaction` (no-op; useful for "what's my baseline?") |
| `/verbose [on\|off]` | Toggle live before/after printing on compaction |

This is BYO-shaped: you can interactively probe how each strategy affects the same conversation. The diffs are illuminating. Sliding window drops decisions; summarization mangles syntax; neither is perfect.

## Pitfalls

**Recursion in summarization.** The `Summarize` strategy calls `Provider.Send`. If your provider somehow ran `Compactor.Compact` inside `Send`, you'd recurse forever. Don't do that — compaction sits in `agent.loop`, around `Provider.Send`, never inside it.

**The system-prompt bias.** Because summarization shares the provider, it inherits the system prompt. If your system prompt is very specific ("you are a code reviewer"), the summary may be filtered through that lens. Accept it or override `Instructions` on `Summarize`.

**Compaction breaking the prompt cache.** If you turn on prompt caching (we don't, but you might), every compaction event invalidates the cache for that prefix — the prefix bytes just changed. Sliding window and summarization both rewrite the prefix. Cache lifetime gets capped at "time between compactions."

> **In the current repo.** Everything compaction-related lives in [`internal/compact/`](../internal/compact/):
>
> - [`strategy.go`](../internal/compact/strategy.go) — the `CompactionStrategy` interface + `SafeSplitPoint`
> - [`nocompaction.go`](../internal/compact/nocompaction.go) — the no-op default
> - [`slidingwindow.go`](../internal/compact/slidingwindow.go) — drops oldest
> - [`summarize.go`](../internal/compact/summarize.go) — model-driven summarization
> - [`logging.go`](../internal/compact/logging.go) — the `WithLogging(inner, path)` decorator
>
> One file per strategy, one interface they all implement. Adding a new one (e.g. the `TokenBudget` exercise below) is one new file + one line in `main.go` to opt in.

## Now try

1. Run the harness, have a 15+ turn conversation, then `/compact sliding` and `/compact summarize` back-to-back. Compare the resulting `messages` (use `/verbose on` first).
2. Wrap a strategy with `WithLogging` and compare two strategies on the same conversation by running each with logging to a different file. Diff the files.
3. Write a `TokenBudget` strategy that drops oldest messages until estimated token count is under a configurable threshold. Start with byte-count as a token proxy; later swap in a real `count_tokens` call.

Next: [08 · Better input](08-better-input.md).
