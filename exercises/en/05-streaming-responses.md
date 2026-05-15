# Exercise 5 — Streaming responses

**Goal:** render the assistant's reply token by token as it streams from the model, instead of waiting for the full response.

**Difficulty:** hard. **Time:** 3–5 hours. **Touches:** `internal/provider/`, `internal/agent/`, `internal/ui/`.

## What's already in place

The `Provider` interface in `internal/provider/provider.go` is three methods:

```go
type Provider interface {
    Send(ctx context.Context, messages []api.Message, tools []api.ToolDef) (api.Response, error)
    Model() string
    SetModel(name string)
}
```

`Send` is synchronous — it returns the full response only when the model finishes. The Bubble Tea UI in `internal/ui/program.go` reflects that: the assistant message appears in one shot, not character by character.

The Anthropic SDK supports streaming via `client.Messages.NewStreaming`. The OpenAI SDK does too. The repo just doesn't use either of those entry points yet.

This is the most invasive exercise in the set — it touches the provider interface, the agent loop, and the TUI. It also exposes a real design tension: tool calls *can't* be streamed (they have to be fully parsed before dispatch), so a streaming path needs to handle text deltas and tool blocks differently.

## What to build

A streaming variant of `Send` and a UI that consumes it.

## Suggested steps

1. **Design the chunk type.** Something like:

   ```go
   type Chunk struct {
       Kind  ChunkKind // TextDelta, ToolStart, ToolDelta, ToolEnd, Stop, Usage
       Text  string    // for TextDelta
       Block api.Block // for ToolEnd
       Usage *api.Usage
   }
   ```

2. **Extend the Provider interface — carefully.** Two options:

   a. **Add a method:** `SendStream(ctx, msgs, tools) (<-chan Chunk, error)`. Pros: clean separation; old `Send` still works. Cons: every implementation now has two methods doing nearly the same thing.

   b. **Make streaming opt-in via a sibling interface:**

      ```go
      type Streamer interface {
          SendStream(ctx, msgs, tools) (<-chan Chunk, error)
      }
      ```

      Type-assert in the agent loop. Pros: providers that can't stream don't have to. Cons: two code paths.

   Pick (b). It's the same pattern used by `TotalUsage()` and `EstimatedCostUSD()` on Provider (see how `/tokens` does an assertion in `commands.go`).

3. **Implement for Anthropic.** Use `client.Messages.NewStreaming(ctx, params)` in `internal/provider/anthropic.go`. Translate SDK events into your `Chunk` type. Buffer `content_block_delta` events for tool_use blocks until the corresponding `content_block_stop` arrives, then emit a single `ToolEnd` chunk with the assembled `api.Block`.

4. **Wire it into the agent loop.** In `internal/agent/agent.go`, when the provider implements `Streamer`, drain the chunk channel and:
   - For `TextDelta`, forward the text to a UI callback (introduce `Agent.OnTextDelta func(string)`).
   - For `ToolEnd`, accumulate blocks in `assistantBlocks`.
   - For `Stop`, finalise the message and dispatch tools as today.

5. **Wire it into the UI.** The Bubble Tea program currently appends the full assistant message in one `tea.Msg`. Add a new message type `streamDeltaMsg{text string}` that gets dispatched as deltas arrive, and append it to the visible buffer.

6. **Handle cancellation.** Ctrl-C during a stream must close the context, drain the channel, and not deadlock. Test this explicitly.

## Acceptance

- Text appears character by character (or chunk by chunk) in the TUI.
- Tool calls still dispatch correctly after the stream ends.
- Ctrl-C cleanly aborts an in-flight stream and returns control to the REPL.
- `/tokens` still reports correct counts after a streamed message.
- The OpenAI provider (which you haven't extended) still works through the non-streaming path.

## Stretch

- Implement `Streamer` for OpenAI too (`client.Chat.Completions.NewStreaming`).
- Add a `--no-stream` flag that forces the non-streaming path even when available, for debugging.
- Track time-to-first-token in the `/debug` panel.
- Stream subagent output too — they currently run silently with `Quiet: true`.
