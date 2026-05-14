// Package api defines the provider-agnostic message types used everywhere
// else in the harness. Providers translate to/from their SDK's native shape;
// tools, compaction strategies, and the agent loop all speak in these types.
package api

import (
	"fmt"
	"strings"
)

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

// HasToolResult reports whether the message contains any tool_result blocks.
// Used by compaction to find safe split points (a clean conversation
// boundary is just before a user message that is NOT tool results).
func (m Message) HasToolResult() bool {
	for _, b := range m.Content {
		if b.Type == BlockToolResult {
			return true
		}
	}
	return false
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

// RenderTranscript serializes messages to a human-readable transcript.
// Used by summarization prompts and compaction logs.
func RenderTranscript(msgs []Message) string {
	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(string(m.Role))
		sb.WriteString(": ")
		for _, b := range m.Content {
			switch b.Type {
			case BlockText:
				sb.WriteString(b.Text)
			case BlockToolUse:
				fmt.Fprintf(&sb, "[called %s with %s]", b.ToolName, b.ToolInput)
			case BlockToolResult:
				fmt.Fprintf(&sb, "[tool result: %s]", b.ToolResult)
			}
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
