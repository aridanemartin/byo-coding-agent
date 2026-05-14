# 03 · The provider interface

So far the agent loop calls `client.Messages.New(...)` directly. The Anthropic SDK types are sprinkled through every file — `anthropic.MessageParam`, `anthropic.ToolUnionParam`, `anthropic.StopReason`. The harness is married to one backend.

This is the first abstraction earn-its-keep moment. We're going to make the LLM backend swappable.

## What we want

```go
// Swap this line to swap providers — that's the whole point.
var llm Provider = NewAnthropicProvider(...)
```

Adding OpenAI, Bedrock, a local Ollama, or a mock should be one new file plus one line in `main.go`. Anything more and we've designed the abstraction wrong.

## Designing the interface

The minimum surface the agent loop needs:

```go
type Provider interface {
    Send(ctx context.Context, messages []Message, tools []ToolDef) (Response, error)
    Model() string
    SetModel(name string)
}
```

`Send` does the round trip. `Model` / `SetModel` exist for the `/model` command (chapter 05) — it's a small concession but it generalizes (every LLM provider has a notion of model).

`Message`, `ToolDef`, `Response` have to be **provider-agnostic types we define ourselves.** This is the load-bearing decision: we can't expose `anthropic.MessageParam` in the interface — that would lock callers to Anthropic's shape and defeat the abstraction.

So we mint our own:

```go
type Role string
const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
)

type BlockType string
const (
    BlockText       BlockType = "text"
    BlockToolUse    BlockType = "tool_use"
    BlockToolResult BlockType = "tool_result"
)

type Block struct {
    Type       BlockType
    Text       string  // BlockText
    ToolUseID  string  // BlockToolUse, BlockToolResult
    ToolName   string  // BlockToolUse
    ToolInput  string  // BlockToolUse — raw JSON
    ToolResult string  // BlockToolResult
    IsError    bool    // BlockToolResult
}

type Message struct {
    Role    Role
    Content []Block
}

type ToolDef struct {
    Name        string
    Description string
    InputSchema map[string]any
    Required    []string
}

type Response struct {
    Content    []Block
    StopReason StopReason
}
```

These types are deliberately the **intersection** of what any major LLM API would need. Block types map cleanly to Anthropic's native shape. For OpenAI, tool_use → `tool_calls` and tool_result → a separate `role: "tool"` message. The translation lives in the adapter.

## The Anthropic adapter

One struct, one big-ish file. The interesting work is in two private methods, `toMessages` and `toTools`, that translate our generic types into the SDK's shape:

```go
type AnthropicProvider struct {
    client    anthropic.Client
    model     anthropic.Model
    maxTokens int64
    system    string
}

func (p *AnthropicProvider) Send(ctx context.Context, messages []Message, tools []ToolDef) (Response, error) {
    resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     p.model,
        MaxTokens: p.maxTokens,
        System:    []anthropic.TextBlockParam{{Text: p.system}},
        Messages:  p.toMessages(messages),
        Tools:     p.toTools(tools),
        Thinking:  anthropic.ThinkingConfigParamUnion{
            OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
        },
    })
    if err != nil { return Response{}, err }

    out := Response{StopReason: fromStopReason(resp.StopReason)}
    for _, block := range resp.Content {
        switch v := block.AsAny().(type) {
        case anthropic.TextBlock:
            out.Content = append(out.Content, Block{Type: BlockText, Text: v.Text})
        case anthropic.ToolUseBlock:
            out.Content = append(out.Content, Block{
                Type:      BlockToolUse,
                ToolUseID: v.ID,
                ToolName:  v.Name,
                ToolInput: v.JSON.Input.Raw(),
            })
        }
    }
    return out, nil
}
```

The whole file is ~120 lines. **It is the only place in the harness that imports `anthropic-sdk-go`.** That's the test for whether the abstraction is real: if SDK types leak elsewhere, you haven't abstracted anything.

## What this earns you

Three concrete wins, in order of obviousness:

1. **You can swap providers.** Write `internal/provider/openai.go` with an `OpenAIProvider` implementing `Provider`. Change one line in `main.go`. The agent loop is unchanged.

2. **You can test the agent loop without an API key.** A `MockProvider` whose `Send` returns canned responses lets you unit-test the loop, compaction, tool dispatch — everything but the model itself.

3. **You can run two models in one session.** Subagents (chapter 11) use the same provider as the root, but in principle could use a cheaper one. The interface doesn't care.

## What it costs

Translation overhead — every `Send` call walks the messages and translates blocks both ways. For a 100-message conversation that's not free, but it's negligible next to network latency. Don't optimize this until the profile says to.

Some loss of provider-specific features. Adaptive thinking lives on the Anthropic params, not in our generic shape. Anthropic-specific fields (`thinking.display`, `output_config.effort`) are configured at adapter-construction time, not exposed through the interface. That's the right tradeoff: provider-specific knobs stay in the provider's package; the agent loop never sees them.

## Pitfalls

**Map iteration order.** When converting tools, the order of fields in `InputSchema` is determined by map iteration, which is random in Go. Two requests with the "same" tools could serialize to different bytes, breaking prompt caching (chapter 06). The fix is to sort tool names before emitting them — we'll come back to this when building the registry in chapter 09.

**The variable name collision.** When the package is `provider`, you can't also have a variable named `provider` in `main`. We rename it to `llm`:

```go
var llm provider.Provider
```

Reads naturally: `llm.Send(...)`, `llm.SetModel("claude-haiku-4-5")`.

## Now try

1. Sketch — don't have to actually implement — an `OpenAIProvider`. What would its `toMessages` look like? Where does the system prompt go in OpenAI's API vs Anthropic's?
2. Read `anthropic.go` and identify every place where SDK types touch our generic types. Those are your translation seams. There should be exactly two: `Send` (response → generic) and `toMessages` / `toTools` (generic → SDK).
3. Write a `MockProvider` that returns a fixed response. Use it to test that `agentLoop` runs without panicking on an empty `messages` slice.

Next: [04 · UI polish](04-ui-polish.md).
