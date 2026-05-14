package api

import (
	"strings"
	"testing"
)

func TestUsageAddSumsAllFields(t *testing.T) {
	a := Usage{InputTokens: 100, OutputTokens: 50, CacheCreationTokens: 10, CacheReadTokens: 5}
	b := Usage{InputTokens: 200, OutputTokens: 30, CacheCreationTokens: 0, CacheReadTokens: 15}
	sum := a.Add(b)

	want := Usage{InputTokens: 300, OutputTokens: 80, CacheCreationTokens: 10, CacheReadTokens: 20}
	if sum != want {
		t.Errorf("sum = %+v, want %+v", sum, want)
	}
}

func TestUsageAddIsCommutative(t *testing.T) {
	a := Usage{InputTokens: 100, OutputTokens: 50, CacheReadTokens: 7}
	b := Usage{InputTokens: 200, OutputTokens: 30, CacheCreationTokens: 3}
	if a.Add(b) != b.Add(a) {
		t.Error("Add should be commutative")
	}
}

func TestUsageAddWithZero(t *testing.T) {
	a := Usage{InputTokens: 100, OutputTokens: 50}
	if a.Add(Usage{}) != a {
		t.Error("adding zero usage should be a no-op")
	}
}

func TestMessageHasToolResult(t *testing.T) {
	cases := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "empty content",
			msg:  Message{Role: RoleUser},
			want: false,
		},
		{
			name: "text only",
			msg:  Message{Role: RoleUser, Content: []Block{{Type: BlockText, Text: "hi"}}},
			want: false,
		},
		{
			name: "single tool_result",
			msg:  Message{Role: RoleUser, Content: []Block{{Type: BlockToolResult, ToolUseID: "t1"}}},
			want: true,
		},
		{
			name: "mixed text and tool_result",
			msg: Message{Role: RoleUser, Content: []Block{
				{Type: BlockText, Text: "context"},
				{Type: BlockToolResult, ToolUseID: "t"},
			}},
			want: true,
		},
		{
			name: "tool_use is not a tool_result",
			msg: Message{Role: RoleAssistant, Content: []Block{
				{Type: BlockToolUse, ToolUseID: "t1", ToolName: "echo"},
			}},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.HasToolResult(); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRenderTranscriptIncludesAllBlockKinds(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: []Block{{Type: BlockText, Text: "hello"}}},
		{Role: RoleAssistant, Content: []Block{
			{Type: BlockText, Text: "thinking…"},
			{Type: BlockToolUse, ToolName: "bash", ToolInput: `{"cmd":"ls"}`},
		}},
		{Role: RoleUser, Content: []Block{{Type: BlockToolResult, ToolResult: "file.txt"}}},
	}
	out := RenderTranscript(msgs)
	for _, want := range []string{
		"user:",
		"assistant:",
		"hello",
		"called bash",
		`{"cmd":"ls"}`,
		"tool result",
		"file.txt",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("transcript missing %q\nfull:\n%s", want, out)
		}
	}
}
