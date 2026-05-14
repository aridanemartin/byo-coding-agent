# 06 · Conversation state

A short, important chapter. We're going to make explicit something that's been quietly true all along: **the model has no memory.** Every API call sends the entire conversation from scratch.

## The invariant

```
POST /v1/messages → response
POST /v1/messages → response
POST /v1/messages → response
```

These three calls are independent. There's no session ID, no server-side conversation, no thread the API remembers between them. To maintain a conversation, the client re-sends everything every turn.

That puts the burden of "memory" on us — the harness. We carry the conversation as a slice in memory, and on every turn we hand the whole slice back to the model.

## Where the slice lives and what it contains

```go
var messages []api.Message
```

That's it. The source of truth for "what has been said." Every turn writes to it; every API call reads it.

What gets appended, in order:

| Step | What gets added | Why |
|---|---|---|
| You submit a line | `{Role: User, Content: [{Type: Text, Text: ...}]}` | Your message |
| Model responds | `{Role: Assistant, Content: resp.Content}` | The model's full response, including any `tool_use` blocks |
| Tools execute | `{Role: User, Content: [{Type: ToolResult, ToolUseID: ..., ToolResult: ...}]}` | One user-role message containing all tool results from this turn |
| Loop calls Send again | nothing new — Send re-reads the slice | The "loop" in agentLoop is over `messages`, not over fresh input |

After a single turn that uses one tool, the slice has four entries (user-role text → assistant with tool_use → user-role tool_result → assistant with final text). The next message you send appends entry five.

## The wiring between tool_use and tool_result

Each `tool_use` block has a unique `ToolUseID` (e.g. `toolu_01abc...`). Each `tool_result` block must reference that same ID via its `ToolUseID` field. Lose the link and the API returns 400 — it sees an orphaned tool_result and refuses to process the turn.

```go
// In agent.loop, after the model emits a tool_use block with v.ID:
result, isErr := executeTool(v.ToolName, v.ToolInput)
toolResults = append(toolResults, Block{
    Type:       BlockToolResult,
    ToolUseID:  v.ToolUseID,   // ← the link
    ToolResult: result,
    IsError:    isErr,
})
```

The IDs are opaque tokens — the model assigns them, we echo them back. This pairing is invisible most of the time but very visible when chapter 07's compaction logic accidentally splits the conversation between a `tool_use` and its `tool_result`.

## System prompt is separate

A subtle but important point: the system prompt is NOT in `messages`. It's a top-level field on the Anthropic request:

```go
client.Messages.New(ctx, anthropic.MessageNewParams{
    System:   []anthropic.TextBlockParam{{Text: p.system}},  // ← top-level
    Messages: ...,                                            // ← user/assistant only
    ...
})
```

Stored on the provider, sent every request. The model doesn't see the system prompt as a "first user message"; it sees it as its own internal instructions. (Other providers — OpenAI in particular — bundle the system prompt into the messages array as `role: "system"`. The provider abstraction from chapter 03 hides this difference.)

## Two consequences

### `/clear` is one line

`messages = messages[:0]` empties the slice. The next API call sends only the system prompt — no prior turns. The model has no memory of anything that came before. We saw this in chapter 05 and now we know why it works.

### Cost grows linearly with conversation length

Every turn re-sends every previous turn through tokenization. By turn 10 you're paying to re-process the same 9 turns of context. By turn 50, you're paying *a lot*.

There are two ways to handle this:

1. **Prompt caching.** Tell the API "this prefix is stable, cache it." Subsequent requests pay ~10% for the cached portion. We don't use this here, but it's the production answer.
2. **Compaction.** When the conversation gets long, summarize the old part and replace it with the summary. Chapter 07 is dedicated to this.

Both are *client-side* responses to a *server-side* reality. The model still has no memory; we just send less context.

## Why server-side compaction isn't the answer here

Anthropic does offer a beta `compact-2026-01-12` header — the API summarizes earlier turns automatically when the context gets near its limit. It's nice. We don't use it.

Reason: this is a learning harness whose provider abstraction is meant to generalize. Server-side compaction is an Anthropic-only feature. If we depended on it, swapping in OpenAI or a mock provider would silently lose compaction. Client-side compaction works against any backend with no changes.

This is a recurring pattern in harness engineering: when there's a feature available "for free" at the provider, the question is whether using it leaks provider-specific assumptions into the harness. Sometimes yes (web search), sometimes no (this one).

## Pitfalls

**The "second-system effect" on context.** Once you understand statelessness, it's tempting to over-design — build a fancy context manager, vector store, retrieval system. Resist. The whole conversation slice is fine for most coding-agent use cases. Compaction (chapter 07) handles the long-tail case.

**Modifying `messages` while iterating.** Don't. The agent loop reads and appends; that's safe in single-goroutine code. If you ever spawn goroutines that touch `messages`, you need a mutex or a channel. We don't yet; chapter 11 has us close to needing one.

**Forgetting to append the assistant turn.** A common bug: handle the tool_use blocks but forget to also append the model's response to `messages`. The next API call would have no record of what the model said, and the API would 400 on the orphaned tool_results.

> **In the current repo.** By chapter 11 the `messages` slice moved from a package-level global to a field on the `Agent` struct in [`internal/agent/agent.go`](../internal/agent/agent.go) — see the `messages []api.Message` field and the `Send`/`Messages`/`SetMessages`/`ClearMessages` methods around it. The statelessness story doesn't change; it's just owned by a struct instead of by `main`. That's what lets us have multiple agents (root + subagents) at the same time.

## Now try

1. After a few turns, dump `messages` to JSON (`json.MarshalIndent(messages, "", "  ")` to stdout) and read the structure. Confirm the alternating user/assistant pattern and the tool_use/tool_result pairings.
2. Time how long the API call takes at turn 1 vs turn 20. Most of the extra time is processing the longer prefix — feel the linear cost.
3. Manually edit `messages` mid-conversation to delete an old assistant response. Send a new message. What happens? (It might 400 if you broke a tool_use/result pair.)

Next: [07 · Compaction strategies](07-compaction-strategies.md).
