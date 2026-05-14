# 17 · Prompt caching

The harness sends every byte of every conversation back to the API on every turn. Chapter 06 made that explicit — the API is stateless, the client carries the history. Chapter 16's token viewer made the cost visible. This chapter is about getting some of that money back.

## What it is

Prompt caching is a server-side optimization: you ask the provider to remember the *processed* state of a stable prefix of your request, so subsequent requests with the same prefix skip the work of re-reading it.

Mechanically, you place a "breakpoint" somewhere in your request. The provider hashes everything up to that point and, on a cache hit, reuses the cached attention state. Everything *after* the breakpoint still gets processed fresh.

It's the same pattern as HTTP caching, applied at the LLM layer.

## The economics

Three categories of input tokens once caching is on:

| Category | Anthropic rate (Opus 4.7) | What it means |
|---|---|---|
| Fresh input | $15.00 / M | Processed from scratch |
| Cache write | $18.75 / M | Fresh input + 25% to save the state |
| Cache read | $1.50 / M | 10% of fresh — reuse hit |

Break-even is two reads — if a prefix gets reused at least twice within the TTL, you've already saved money. For an agent, where one user message often spawns five-to-ten model calls (each tool result loops back), a stable prefix gets reused dozens of times. The savings can be 70–80% off your input bill.

OpenAI does this automatically with no breakpoints — common prefixes are detected and cached for ~5–10 minutes by default. The flexibility is lower; the ergonomics are higher.

## Why agents benefit more than chat

Three forces stack up for agents:

1. **Tool definitions are huge.** A handful of MCP servers + local tools is easily 2–4 KB of JSON schema per call. That schema doesn't change turn-to-turn — perfect cache target.
2. **The agent loop multiplies calls.** A single user request might trigger 10 model calls (read three files, run two bash commands, summarize). Each loop sends the *same* system prompt + tools, plus a slightly longer message history. The first ~80% of every call is identical to the previous one's first 80%.
3. **The system prompt grows.** When `AGENTS.md` is loaded (chapter 15), the system prompt balloons. Without caching, you pay for that 5 KB of project context on *every* call.

## What invalidates the cache

The cache is keyed by an exact byte match of everything up to the breakpoint. Any of the following silently wipes it:

- Editing the system prompt.
- Editing `AGENTS.md` between sessions.
- Adding or removing a tool (an MCP server connects, registers something new).
- Changing model id mid-session (`/model` or `/provider` swap).
- **Compaction** rewriting the early conversation history (chapter 07).
- Any timestamp or random nonce you accidentally include in the prefix.

The compaction interaction is the nasty one for this harness: every time `SlidingWindow` or `Summarize` runs, the front of the messages slice changes, the breakpoint moves over different bytes, and you pay the cache-write premium all over again.

## Implementing it in this harness

Three layers, in order of impact:

### 1. The provider

In `internal/provider/anthropic.go`, mark the last system block as a cache breakpoint:

```go
System: []anthropic.TextBlockParam{{
    Text:         p.system,
    CacheControl: anthropic.NewCacheControlEphemeralParam(),
}},
```

This caches everything up to the end of the system prompt, including the tool definitions which are sent alongside it. With this one change, every turn after the first reads the system prompt + tools from cache instead of re-processing them.

For deeper caching — caching parts of the message history too — Anthropic supports up to four breakpoints, one on each progressively-later block. Each breakpoint defines a "layer" that can be cached independently:

```
Layer 1: system prompt + tools          (caches once per session)
Layer 2: first N user messages          (caches per "stable opener")
Layer 3: most-recent assistant turn     (caches per turn)
```

The harness currently doesn't expose multi-layer breakpoints; for the one-line win, the system-level breakpoint is enough.

### 2. The usage tracking

Nothing to do. `api.Usage.CacheCreationTokens` and `CacheReadTokens` already exist (chapter 16), `AnthropicProvider.Send` already populates them from `resp.Usage`, and `/tokens` already prints them when non-zero. The price table in `anthropic.go` already has the right rates. The pipes are connected — flipping caching on lights up that column of the token viewer.

### 3. The compaction interaction

This is the trap. By default the agent loop calls `Compactor.Compact` every turn. Even with `NoCompaction` set, a single `/compact` invocation invalidates the cache for the rest of the session.

Three reasonable mitigations:

- **Don't compact under threshold.** Most strategies already do this — `SlidingWindow{KeepLast: 10}` is a no-op for the first 10 turns. The cache survives until compaction actually fires.
- **Cache only the prefix that compaction won't touch.** Put the breakpoint at the *end of the system prompt*, not inside the message history. System + tools never change; everything after that is fair game for compaction.
- **Accept the trade-off.** When compaction fires, the next turn pays the cache-write premium. The turn after that, you're back to cheap reads.

The default position in this harness is option 2 — put the breakpoint at the system level and let compaction run freely.

## OpenAI's variant

The OpenAI provider gets automatic caching with no API change. Chat Completions caches implicitly: a prefix that's been seen recently is reused, and the response's `usage.prompt_tokens_details.cached_tokens` field reports how many tokens were served from cache.

The `OpenAIProvider` in this harness already reads that field and maps it into `api.Usage.CacheReadTokens`. So switching `/provider openai` and watching `/tokens` will already show non-zero cache reads on long conversations — no further work needed.

The OpenAI design philosophy: less manual control, less you can break.

## Pitfalls

**Forgetting that tools are part of the prefix.** Tool definitions are sent on every call as part of the request, and they're cached alongside the system prompt. Changing tool descriptions or order between calls invalidates the cache. Our registry sorts tool definitions by name (chapter 09) to keep the byte serialization stable — that wasn't an accident.

**TTL surprises.** Anthropic's ephemeral cache lives ~5 minutes. If you leave the harness open for ten minutes and come back, the first turn pays the write premium again. For an interactive coding session that's fine; for a long-running batch job it might matter.

**Cache write on the first turn looks expensive.** A user opening the harness, sending one message, and quitting will pay *more* than no-caching at all (the 25% write premium with no subsequent reads to amortize it). The break-even is two cached reads. For interactive sessions this is essentially always met; for one-shot CLI invocations it isn't.

**Stacking breakpoints.** Each breakpoint is a separate cache entry. With four breakpoints stacked, you potentially pay four write premiums on the first turn. Only add breakpoints at boundaries that are *also* stable for many calls.

## Now try

1. Add the one-line `CacheControl: anthropic.NewCacheControlEphemeralParam()` to the system block in `anthropic.go`. Run the harness, ask a multi-tool question, then `/tokens`. The first turn shows cache-creation tokens; subsequent turns show cache-read tokens.
2. Make a tiny edit to `AGENTS.md` and restart. Type any prompt. Observe that the cache write fires again — the system prompt is no longer identical to whatever was cached previously.
3. Run a `/compact summarize` mid-session and watch the next turn pay the write premium again. Confirm that the turn after that goes back to reads.

← [back to TOC](README.md)
