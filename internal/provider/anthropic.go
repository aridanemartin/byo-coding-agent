package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/debug"
)

// AnthropicProvider is the reference Provider implementation. It is the
// only file in the harness that imports the Anthropic SDK.
type AnthropicProvider struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
	system    string

	mu    sync.Mutex
	total api.Usage // cumulative across every Send call on this provider
}

func NewAnthropicProvider(model anthropic.Model, maxTokens int64, system string) *AnthropicProvider {
	return &AnthropicProvider{
		client:    anthropic.NewClient(),
		model:     model,
		maxTokens: maxTokens,
		system:    system,
	}
}

func (p *AnthropicProvider) Model() string { return string(p.model) }
func (p *AnthropicProvider) SetModel(name string) {
	p.mu.Lock()
	p.model = anthropic.Model(name)
	p.mu.Unlock()
}

// TotalUsage returns the cumulative tokens this provider has consumed since
// it was constructed. Subagents that share the provider contribute to the
// same total.
func (p *AnthropicProvider) TotalUsage() api.Usage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.total
}

// EstimatedCostUSD multiplies the cumulative usage by the current model's
// per-million-token rates. Returns -1 for an unknown model.
func (p *AnthropicProvider) EstimatedCostUSD() float64 {
	p.mu.Lock()
	u := p.total
	model := string(p.model)
	p.mu.Unlock()
	rates, ok := modelPricing[model]
	if !ok {
		return -1
	}
	return float64(u.InputTokens)*rates.InputPerMillion/1_000_000 +
		float64(u.OutputTokens)*rates.OutputPerMillion/1_000_000 +
		float64(u.CacheCreationTokens)*rates.CacheCreationPerMillion/1_000_000 +
		float64(u.CacheReadTokens)*rates.CacheReadPerMillion/1_000_000
}

// pricing is per-million-token rates in USD. Update from
// https://www.anthropic.com/pricing when rates change.
type pricing struct {
	InputPerMillion         float64
	OutputPerMillion        float64
	CacheCreationPerMillion float64
	CacheReadPerMillion     float64
}

var modelPricing = map[string]pricing{
	"claude-opus-4-7":   {InputPerMillion: 15.00, OutputPerMillion: 75.00, CacheCreationPerMillion: 18.75, CacheReadPerMillion: 1.50},
	"claude-opus-4-6":   {InputPerMillion: 15.00, OutputPerMillion: 75.00, CacheCreationPerMillion: 18.75, CacheReadPerMillion: 1.50},
	"claude-sonnet-4-6": {InputPerMillion: 3.00, OutputPerMillion: 15.00, CacheCreationPerMillion: 3.75, CacheReadPerMillion: 0.30},
	"claude-haiku-4-5":  {InputPerMillion: 1.00, OutputPerMillion: 5.00, CacheCreationPerMillion: 1.25, CacheReadPerMillion: 0.10},
}

func (p *AnthropicProvider) Send(ctx context.Context, messages []api.Message, tools []api.ToolDef) (api.Response, error) {
	reqSummary := fmt.Sprintf("→ anthropic.Send model=%s msgs=%d tools=%d", p.model, len(messages), len(tools))
	reqID := debug.Recordp("provider", debug.LevelInfo, reqSummary, marshalRequestPayload(string(p.model), p.system, p.maxTokens, messages, tools))
	start := time.Now()
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     p.model,
		MaxTokens: p.maxTokens,
		System:    []anthropic.TextBlockParam{{Text: p.system}},
		Messages:  p.toMessages(messages),
		Tools:     p.toTools(tools),
		Thinking: anthropic.ThinkingConfigParamUnion{
			OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
		},
	})
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		debug.Recordfc(reqID, "provider", debug.LevelError, "← anthropic.Send error (%v): %v", elapsed, err)
		return api.Response{}, err
	}

	out := api.Response{StopReason: fromStopReason(resp.StopReason)}
	for _, block := range resp.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			out.Content = append(out.Content, api.Block{Type: api.BlockText, Text: v.Text})
		case anthropic.ToolUseBlock:
			out.Content = append(out.Content, api.Block{
				Type:      api.BlockToolUse,
				ToolUseID: v.ID,
				ToolName:  v.Name,
				ToolInput: v.JSON.Input.Raw(),
			})
		}
	}

	out.Usage = api.Usage{
		InputTokens:         int(resp.Usage.InputTokens),
		OutputTokens:        int(resp.Usage.OutputTokens),
		CacheCreationTokens: int(resp.Usage.CacheCreationInputTokens),
		CacheReadTokens:     int(resp.Usage.CacheReadInputTokens),
	}
	p.mu.Lock()
	p.total = p.total.Add(out.Usage)
	p.mu.Unlock()
	respSummary := fmt.Sprintf("← anthropic.Send (%v in=%d out=%d cache=%d/%d stop=%s)",
		elapsed, out.Usage.InputTokens, out.Usage.OutputTokens,
		out.Usage.CacheCreationTokens, out.Usage.CacheReadTokens, out.StopReason)
	debug.Recordpc(reqID, "provider", debug.LevelInfo, respSummary, marshalResponsePayload(out, elapsed))
	return out, nil
}

func (p *AnthropicProvider) toMessages(messages []api.Message) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.Content))
		for _, b := range m.Content {
			switch b.Type {
			case api.BlockText:
				blocks = append(blocks, anthropic.NewTextBlock(b.Text))
			case api.BlockToolUse:
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    b.ToolUseID,
						Name:  b.ToolName,
						Input: json.RawMessage(b.ToolInput),
					},
				})
			case api.BlockToolResult:
				blocks = append(blocks, anthropic.NewToolResultBlock(b.ToolUseID, b.ToolResult, b.IsError))
			}
		}
		switch m.Role {
		case api.RoleUser:
			out = append(out, anthropic.NewUserMessage(blocks...))
		case api.RoleAssistant:
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		}
	}
	return out
}

func (p *AnthropicProvider) toTools(tools []api.ToolDef) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		out = append(out, anthropic.ToolUnionParam{
			OfTool: &anthropic.ToolParam{
				Name:        t.Name,
				Description: anthropic.String(t.Description),
				InputSchema: anthropic.ToolInputSchemaParam{
					Properties: t.InputSchema,
					Required:   t.Required,
				},
			},
		})
	}
	return out
}

func fromStopReason(s anthropic.StopReason) api.StopReason {
	switch s {
	case anthropic.StopReasonEndTurn:
		return api.StopEndTurn
	case anthropic.StopReasonToolUse:
		return api.StopToolUse
	default:
		return api.StopOther
	}
}
