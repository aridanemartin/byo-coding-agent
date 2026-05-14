# How to add a new provider

Goal: run the harness against a different LLM backend — OpenAI, Bedrock, a local Ollama, or a mock for tests.

The `Provider` interface ([`internal/provider/provider.go`](../../internal/provider/provider.go)) is three methods. Implementing them in a new file is the entire change at the abstraction layer; the agent loop, tools, compaction, and TUI don't know which backend they're talking to.

## Steps

### 1. Create `internal/provider/your_provider.go`

```go
package provider

import (
	"context"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

type YourProvider struct {
	client    *yourSDK.Client
	model     string
	system    string
	maxTokens int
}

func NewYourProvider(model, system string, maxTokens int) *YourProvider {
	return &YourProvider{
		client:    yourSDK.NewClient(),
		model:     model,
		system:    system,
		maxTokens: maxTokens,
	}
}

func (p *YourProvider) Model() string        { return p.model }
func (p *YourProvider) SetModel(name string) { p.model = name }

func (p *YourProvider) Send(ctx context.Context, messages []api.Message, tools []api.ToolDef) (api.Response, error) {
	req := p.toRequest(messages, tools)        // ↓ adapter
	sdkResp, err := p.client.Chat(ctx, req)
	if err != nil {
		return api.Response{}, err
	}
	return p.fromResponse(sdkResp), nil        // ↓ adapter
}
```

The two private methods (`toRequest`, `fromResponse`) are the only places SDK types are allowed to appear. If a `yourSDK.Foo` shows up anywhere else in the codebase, the abstraction has leaked.

### 2. Implement the two translation methods

`toRequest` maps `[]api.Message` → the SDK's native message shape, and `[]api.ToolDef` → the SDK's tool shape. Look at [`internal/provider/anthropic.go`](../../internal/provider/anthropic.go)'s `toMessages` and `toTools` for the reference pattern.

For OpenAI specifically:

| api type | OpenAI mapping |
|---|---|
| `Message{Role: User}` | `{role: "user", content: ...}` |
| `Message{Role: Assistant}` with `BlockToolUse` | `{role: "assistant", tool_calls: [...]}` |
| `BlockToolResult` | A separate `{role: "tool", tool_call_id: ..., content: ...}` message |
| `system` (top-level field) | First message: `{role: "system", content: ...}` |

The `tool_result` shape is OpenAI's biggest divergence: results are their own messages with `role: "tool"`, not blocks inside a user message. Your `toRequest` has to flatten one `api.Message` containing multiple `BlockToolResult` blocks into multiple OpenAI messages.

`fromResponse` does the reverse: SDK content/tool_calls/finish_reason → `api.Response{Content, StopReason, Usage}`.

### 3. Wire it into `main.go`

Change one line:

```go
// Before
llm := provider.NewAnthropicProvider(anthropic.ModelClaudeOpus4_7, 8192, sysPrompt)

// After
llm := provider.NewYourProvider("gpt-4o", sysPrompt, 8192)
```

Build, run. The rest of the harness is unchanged.

## Conventions

- **Only the new file imports the SDK.** This is the test for whether the abstraction is real. If you find SDK types leaking into `internal/agent/`, `internal/tool/`, or `main.go`, fix it now — it'll be much harder later.
- **`Send` must populate `Usage`** if you want `/tokens` to work. Map the provider's per-call token counts into `api.Usage`. Cache fields can be zero if the provider doesn't expose them.
- **`StopReason` is a three-way enum** — `StopEndTurn` (model is done), `StopToolUse` (model wants tools), `StopOther` (everything else: max_tokens, refusal, etc.). The agent loop only branches on `StopToolUse`.
- **Sort tool definitions before sending.** Map iteration in Go is random; consecutive requests with the "same" tools can serialize to different bytes, which breaks prompt caching. The Anthropic adapter handles this in the registry layer — copy the pattern.

## Mock provider for tests

A minimal stub that returns canned responses, useful for testing the agent loop without an API key:

```go
type MockProvider struct {
	Responses []api.Response
	calls     int
}

func (p *MockProvider) Send(ctx context.Context, _ []api.Message, _ []api.ToolDef) (api.Response, error) {
	if p.calls >= len(p.Responses) {
		return api.Response{StopReason: api.StopEndTurn}, nil
	}
	r := p.Responses[p.calls]
	p.calls++
	return r, nil
}

func (p *MockProvider) Model() string        { return "mock" }
func (p *MockProvider) SetModel(name string) {}
```

Inject canned text responses, canned `tool_use` blocks, etc. — useful for unit-testing compaction, the agent loop, slash commands.

## Token tracking (optional)

If your provider can report token usage, add a non-interface `TotalUsage()` method that returns cumulative session counts. The `/tokens` slash command type-asserts on this method (see [`follow_along/en/16-token-viewer.md`](../../follow_along/en/16-token-viewer.md)) — implement it and the token viewer just works.

## See also

- [`follow_along/en/03-the-provider-interface.md`](../../follow_along/en/03-the-provider-interface.md) for why the interface looks the way it does.
- [`internal/provider/anthropic.go`](../../internal/provider/anthropic.go) for the reference implementation.
