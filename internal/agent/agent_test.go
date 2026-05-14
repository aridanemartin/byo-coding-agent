package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/compact"
	"github.com/betta-tech/byo-coding-agent/internal/provider"
	"github.com/betta-tech/byo-coding-agent/internal/tool"
)

// The agent loop prints `[tool] …` lines and assistant text to stdout by
// design (chapter 01). Tests don't silence them — running `go test -v`
// will interleave that output with the test runner's. Run without -v for
// a quiet pass/fail summary.

// echoTool is the minimal tool.Tool used by tests that need a registered
// tool but don't care about its behavior. It returns its raw input as the
// "tool result" so test code can confirm the agent's dispatch is wired up.
type echoTool struct{}

func (echoTool) Definition() api.ToolDef {
	return api.ToolDef{Name: "echo", Description: "echoes its input"}
}

func (echoTool) Execute(_ context.Context, in string) (string, bool) { return in, false }

// newTestAgent wires an Agent against the mock provider with the smallest
// useful surface area: an empty system prompt, an empty tool registry,
// no-op compaction. Tests opt into more by mutating the returned Agent.
func newTestAgent(p provider.Provider) *Agent {
	a := New(p, "", tool.NewRegistry())
	a.Compactor = compact.NoCompaction{}
	return a
}

// TestSendReturnsTextOnEndTurn exercises the simplest path: the model
// replies with a text block and a non-tool stop reason, and Send returns
// that text trimmed.
func TestSendReturnsTextOnEndTurn(t *testing.T) {
	p := provider.NewMockProvider(api.Response{
		StopReason: api.StopEndTurn,
		Content:    []api.Block{{Type: api.BlockText, Text: "hello there"}},
	})
	a := newTestAgent(p)

	got, err := a.Send(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got != "hello there" {
		t.Errorf("got %q, want %q", got, "hello there")
	}
	if p.Calls() != 1 {
		t.Errorf("provider calls = %d, want 1", p.Calls())
	}
}

// TestSendLoopsOnToolUse covers the tool-use round-trip: the model asks
// for `echo`, the agent dispatches it, the agent loops back to the model
// with the tool result appended, the model returns text, Send returns it.
// We also verify the second request to the provider carries the tool
// result block — that's the "errors as tool results" contract from
// chapter 02 in action.
func TestSendLoopsOnToolUse(t *testing.T) {
	p := provider.NewMockProvider(
		api.Response{
			StopReason: api.StopToolUse,
			Content: []api.Block{{
				Type:      api.BlockToolUse,
				ToolUseID: "t1",
				ToolName:  "echo",
				ToolInput: `{"x":1}`,
			}},
		},
		api.Response{
			StopReason: api.StopEndTurn,
			Content:    []api.Block{{Type: api.BlockText, Text: "done"}},
		},
	)
	reg := tool.NewRegistry()
	reg.Register(echoTool{})
	a := newTestAgent(p)
	a.Tools = reg

	got, err := a.Send(context.Background(), "do it")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got != "done" {
		t.Errorf("got %q, want %q", got, "done")
	}
	if p.Calls() != 2 {
		t.Errorf("provider calls = %d, want 2", p.Calls())
	}
	last := p.LastSent()
	if !hasToolResult(last, "t1") {
		t.Errorf("second request to provider missing tool_result for t1; got %d messages", len(last))
	}
}

// TestSendStopsAtMaxTurns confirms the safety clamp: a provider that
// keeps requesting tools indefinitely is bounded by Agent.MaxTurns. The
// loop terminates with a max-turns error rather than spinning forever.
func TestSendStopsAtMaxTurns(t *testing.T) {
	p := &provider.MockProvider{
		Responses: []api.Response{{
			StopReason: api.StopToolUse,
			Content: []api.Block{{
				Type:      api.BlockToolUse,
				ToolUseID: "t",
				ToolName:  "echo",
				ToolInput: `{}`,
			}},
		}},
		RepeatLast: true, // make the mock loop forever
	}
	reg := tool.NewRegistry()
	reg.Register(echoTool{})
	a := newTestAgent(p)
	a.Tools = reg
	a.MaxTurns = 3

	_, err := a.Send(context.Background(), "loop forever")
	if err == nil {
		t.Fatal("expected max-turns error, got nil")
	}
	if !strings.Contains(err.Error(), "max turns") {
		t.Errorf("err = %v, want one mentioning max turns", err)
	}
	if p.Calls() != 3 {
		t.Errorf("provider calls = %d, want 3 (MaxTurns)", p.Calls())
	}
}

// TestSendPropagatesProviderError makes sure a provider failure bubbles
// out of Send instead of being swallowed. The error itself should be the
// exact sentinel — no wrapping at this layer.
func TestSendPropagatesProviderError(t *testing.T) {
	sentinel := errors.New("provider exploded")
	p := &provider.MockProvider{Err: sentinel}
	a := newTestAgent(p)

	_, err := a.Send(context.Background(), "hi")
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v", err, sentinel)
	}
}

// hasToolResult scans messages for a tool_result block matching id.
func hasToolResult(messages []api.Message, id string) bool {
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Type == api.BlockToolResult && b.ToolUseID == id {
				return true
			}
		}
	}
	return false
}
