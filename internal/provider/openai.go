package provider

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
	"github.com/openai/openai-go/shared"

	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/debug"
)

// OpenAIProvider implements Provider on OpenAI's Chat Completions API.
// Works for any chat-capable model: gpt-4o, gpt-5, gpt-5-codex, o3-mini, etc.
// This file is the only place in the harness that imports openai-go.
type OpenAIProvider struct {
	client    openai.Client
	model     string
	system    string
	maxTokens int64

	mu    sync.Mutex
	total api.Usage
}

// NewOpenAIProvider constructs the provider. The SDK reads OPENAI_API_KEY
// from the environment by default; pass option.WithAPIKey if you need to
// override.
func NewOpenAIProvider(model, system string, maxTokens int64) *OpenAIProvider {
	return &OpenAIProvider{
		client:    openai.NewClient(),
		model:     model,
		system:    system,
		maxTokens: maxTokens,
	}
}

func (p *OpenAIProvider) Model() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.model
}

func (p *OpenAIProvider) SetModel(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.model = name
}

func (p *OpenAIProvider) TotalUsage() api.Usage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.total
}

func (p *OpenAIProvider) EstimatedCostUSD() float64 {
	p.mu.Lock()
	u := p.total
	model := p.model
	p.mu.Unlock()
	rates, ok := openaiPricing[model]
	if !ok {
		return -1
	}
	return float64(u.InputTokens)*rates.InputPerMillion/1_000_000 +
		float64(u.OutputTokens)*rates.OutputPerMillion/1_000_000 +
		float64(u.CacheReadTokens)*rates.CacheReadPerMillion/1_000_000
}

func (p *OpenAIProvider) Send(ctx context.Context, messages []api.Message, tools []api.ToolDef) (api.Response, error) {
	p.mu.Lock()
	model := p.model
	p.mu.Unlock()

	reqSummary := fmt.Sprintf("→ openai.Send model=%s msgs=%d tools=%d", model, len(messages), len(tools))
	reqID := debug.Recordp("provider", debug.LevelInfo, reqSummary, marshalRequestPayload(model, p.system, p.maxTokens, messages, tools))
	start := time.Now()
	resp, err := p.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:               shared.ChatModel(model),
		Messages:            p.toMessages(messages),
		Tools:               p.toTools(tools),
		MaxCompletionTokens: param.NewOpt(p.maxTokens),
	})
	elapsed := time.Since(start).Round(time.Millisecond)
	if err != nil {
		debug.Recordfc(reqID, "provider", debug.LevelError, "← openai.Send error (%v): %v", elapsed, err)
		return api.Response{}, err
	}
	if len(resp.Choices) == 0 {
		return api.Response{StopReason: api.StopOther}, nil
	}
	choice := resp.Choices[0]

	out := api.Response{StopReason: fromFinishReason(choice.FinishReason)}
	if choice.Message.Content != "" {
		out.Content = append(out.Content, api.Block{Type: api.BlockText, Text: choice.Message.Content})
	}
	for _, tc := range choice.Message.ToolCalls {
		out.Content = append(out.Content, api.Block{
			Type:      api.BlockToolUse,
			ToolUseID: tc.ID,
			ToolName:  tc.Function.Name,
			ToolInput: tc.Function.Arguments,
		})
	}

	out.Usage = api.Usage{
		InputTokens:     int(resp.Usage.PromptTokens),
		OutputTokens:    int(resp.Usage.CompletionTokens),
		CacheReadTokens: int(resp.Usage.PromptTokensDetails.CachedTokens),
		// OpenAI doesn't expose cache-creation separately; cached_tokens is
		// the only cache figure they return, and it maps to cache reads.
	}
	p.mu.Lock()
	p.total = p.total.Add(out.Usage)
	p.mu.Unlock()
	respSummary := fmt.Sprintf("← openai.Send (%v in=%d out=%d cache=%d stop=%s)",
		elapsed, out.Usage.InputTokens, out.Usage.OutputTokens, out.Usage.CacheReadTokens, out.StopReason)
	debug.Recordpc(reqID, "provider", debug.LevelInfo, respSummary, marshalResponsePayload(out, elapsed))
	return out, nil
}

// toMessages translates our generic Message slice into the OpenAI chat-message
// shape. Two notable splits:
//   - tool results: Anthropic carries them as blocks inside a user-role
//     message; OpenAI wants each one as its own message with role:"tool".
//   - assistant turns with both text and tool_use: one OpenAI assistant
//     message with .Content AND .ToolCalls populated.
func (p *OpenAIProvider) toMessages(messages []api.Message) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages)+1)
	if p.system != "" {
		out = append(out, openai.SystemMessage(p.system))
	}
	for _, m := range messages {
		switch m.Role {
		case api.RoleUser:
			// Split this message: gather text into one UserMessage, emit each
			// tool_result as its own ToolMessage.
			var textParts []string
			var toolResults []openai.ChatCompletionMessageParamUnion
			for _, b := range m.Content {
				switch b.Type {
				case api.BlockText:
					textParts = append(textParts, b.Text)
				case api.BlockToolResult:
					toolResults = append(toolResults, openai.ToolMessage(b.ToolResult, b.ToolUseID))
				}
			}
			if len(textParts) > 0 {
				out = append(out, openai.UserMessage(strings.Join(textParts, "\n")))
			}
			out = append(out, toolResults...)

		case api.RoleAssistant:
			var text strings.Builder
			var toolCalls []openai.ChatCompletionMessageToolCallParam
			for _, b := range m.Content {
				switch b.Type {
				case api.BlockText:
					if text.Len() > 0 {
						text.WriteString("\n")
					}
					text.WriteString(b.Text)
				case api.BlockToolUse:
					toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallParam{
						ID: b.ToolUseID,
						Function: openai.ChatCompletionMessageToolCallFunctionParam{
							Name:      b.ToolName,
							Arguments: b.ToolInput,
						},
					})
				}
			}
			msg := openai.ChatCompletionAssistantMessageParam{}
			if text.Len() > 0 {
				msg.Content.OfString = param.NewOpt(text.String())
			}
			if len(toolCalls) > 0 {
				msg.ToolCalls = toolCalls
			}
			out = append(out, openai.ChatCompletionMessageParamUnion{OfAssistant: &msg})
		}
	}
	return out
}

// toTools builds the OpenAI tool list. OpenAI expects the *full* JSON Schema
// envelope (`{"type":"object","properties":..., "required":...}`); our
// ToolDef.InputSchema only holds the inner properties map, so we wrap.
func (p *OpenAIProvider) toTools(tools []api.ToolDef) []openai.ChatCompletionToolParam {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openai.ChatCompletionToolParam, 0, len(tools))
	for _, t := range tools {
		parameters := map[string]any{
			"type":       "object",
			"properties": t.InputSchema,
		}
		if len(t.Required) > 0 {
			parameters["required"] = t.Required
		}
		out = append(out, openai.ChatCompletionToolParam{
			Function: shared.FunctionDefinitionParam{
				Name:        t.Name,
				Description: param.NewOpt(t.Description),
				Parameters:  shared.FunctionParameters(parameters),
			},
		})
	}
	return out
}

func fromFinishReason(r string) api.StopReason {
	switch r {
	case "stop":
		return api.StopEndTurn
	case "tool_calls":
		return api.StopToolUse
	default:
		return api.StopOther
	}
}

type openaiRates struct {
	InputPerMillion     float64
	OutputPerMillion    float64
	CacheReadPerMillion float64
}

// openaiPricing is per-million-token rates in USD. Verify at
// https://platform.openai.com/docs/pricing — these are approximations and
// change. Unknown models fall back to "-1" from EstimatedCostUSD().
var openaiPricing = map[string]openaiRates{
	"gpt-4o":      {InputPerMillion: 2.50, OutputPerMillion: 10.00, CacheReadPerMillion: 1.25},
	"gpt-4o-mini": {InputPerMillion: 0.15, OutputPerMillion: 0.60, CacheReadPerMillion: 0.075},
	"gpt-5":       {InputPerMillion: 1.25, OutputPerMillion: 10.00, CacheReadPerMillion: 0.125},
	"gpt-5-codex": {InputPerMillion: 1.25, OutputPerMillion: 10.00, CacheReadPerMillion: 0.125},
	"gpt-5-mini":  {InputPerMillion: 0.25, OutputPerMillion: 2.00, CacheReadPerMillion: 0.025},
	"o1":          {InputPerMillion: 15.00, OutputPerMillion: 60.00, CacheReadPerMillion: 7.50},
	"o3-mini":     {InputPerMillion: 1.10, OutputPerMillion: 4.40, CacheReadPerMillion: 0.55},
}
