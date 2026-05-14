package main

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
)

type AnthropicProvider struct {
	client    anthropic.Client
	model     anthropic.Model
	maxTokens int64
	system    string
}

func NewAnthropicProvider(model anthropic.Model, maxTokens int64, system string) *AnthropicProvider {
	return &AnthropicProvider{
		client:    anthropic.NewClient(),
		model:     model,
		maxTokens: maxTokens,
		system:    system,
	}
}

func (p *AnthropicProvider) Send(ctx context.Context, messages []Message, tools []ToolDef) (Response, error) {
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
	if err != nil {
		return Response{}, err
	}

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

func (p *AnthropicProvider) toMessages(messages []Message) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, 0, len(messages))
	for _, m := range messages {
		blocks := make([]anthropic.ContentBlockParamUnion, 0, len(m.Content))
		for _, b := range m.Content {
			switch b.Type {
			case BlockText:
				blocks = append(blocks, anthropic.NewTextBlock(b.Text))
			case BlockToolUse:
				blocks = append(blocks, anthropic.ContentBlockParamUnion{
					OfToolUse: &anthropic.ToolUseBlockParam{
						ID:    b.ToolUseID,
						Name:  b.ToolName,
						Input: json.RawMessage(b.ToolInput),
					},
				})
			case BlockToolResult:
				blocks = append(blocks, anthropic.NewToolResultBlock(b.ToolUseID, b.ToolResult, b.IsError))
			}
		}
		switch m.Role {
		case RoleUser:
			out = append(out, anthropic.NewUserMessage(blocks...))
		case RoleAssistant:
			out = append(out, anthropic.NewAssistantMessage(blocks...))
		}
	}
	return out
}

func (p *AnthropicProvider) toTools(tools []ToolDef) []anthropic.ToolUnionParam {
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

func (p *AnthropicProvider) Model() string         { return string(p.model) }
func (p *AnthropicProvider) SetModel(name string)  { p.model = anthropic.Model(name) }

func fromStopReason(s anthropic.StopReason) StopReason {
	switch s {
	case anthropic.StopReasonEndTurn:
		return StopEndTurn
	case anthropic.StopReasonToolUse:
		return StopToolUse
	default:
		return StopOther
	}
}
