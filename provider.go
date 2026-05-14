package main

import "context"

// Provider is the LLM backend. Swap implementations to swap models / SDKs.
// The harness only talks to providers through this interface.
type Provider interface {
	Send(ctx context.Context, messages []Message, tools []ToolDef) (Response, error)
	Model() string
	SetModel(name string)
}

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

// Block is one piece of message content. Fields are interpreted based on Type.
type Block struct {
	Type BlockType

	Text string // BlockText

	ToolUseID string // BlockToolUse, BlockToolResult
	ToolName  string // BlockToolUse
	ToolInput string // BlockToolUse — raw JSON, pass-through to provider

	ToolResult string // BlockToolResult
	IsError    bool   // BlockToolResult
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

type StopReason string

const (
	StopEndTurn StopReason = "end_turn"
	StopToolUse StopReason = "tool_use"
	StopOther   StopReason = "other"
)

type Response struct {
	Content    []Block
	StopReason StopReason
}
