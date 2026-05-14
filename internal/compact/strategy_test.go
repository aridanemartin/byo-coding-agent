package compact

import (
	"context"
	"testing"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

// Test helpers that build common message shapes — text turn, tool-use
// turn, tool-result turn. Keeps the test cases readable.

func textMsg(role api.Role, text string) api.Message {
	return api.Message{Role: role, Content: []api.Block{{Type: api.BlockText, Text: text}}}
}

func toolUseMsg(id, name string) api.Message {
	return api.Message{Role: api.RoleAssistant, Content: []api.Block{{
		Type:      api.BlockToolUse,
		ToolUseID: id,
		ToolName:  name,
		ToolInput: "{}",
	}}}
}

func toolResultMsg(id, result string) api.Message {
	return api.Message{Role: api.RoleUser, Content: []api.Block{{
		Type:       api.BlockToolResult,
		ToolUseID:  id,
		ToolResult: result,
	}}}
}

// When the desired split already lands on a clean user text message,
// SafeSplitPoint returns it as-is.
func TestSafeSplitPointAtCleanBoundary(t *testing.T) {
	msgs := []api.Message{
		textMsg(api.RoleUser, "hi"),
		textMsg(api.RoleAssistant, "hello"),
		textMsg(api.RoleUser, "how are you"),
	}
	if got := SafeSplitPoint(msgs, 2); got != 2 {
		t.Errorf("got %d, want 2 (already a clean user message)", got)
	}
}

// When desired falls inside a tool_use/tool_result pair, the function
// walks back to the previous clean user-text boundary.
func TestSafeSplitPointWalksBackOverToolPair(t *testing.T) {
	msgs := []api.Message{
		textMsg(api.RoleUser, "q1"),       // 0
		textMsg(api.RoleAssistant, "a1"),  // 1
		textMsg(api.RoleUser, "q2"),       // 2  <- last clean boundary
		toolUseMsg("t1", "echo"),          // 3
		toolResultMsg("t1", "ok"),         // 4  <- splitting here orphans tool_use
		textMsg(api.RoleAssistant, "done"), // 5
	}
	if got := SafeSplitPoint(msgs, 4); got != 2 {
		t.Errorf("got %d, want 2 (walks back from inside the tool pair)", got)
	}
}

// If no clean boundary exists in the prefix, returns 0 — "do nothing"
// is safer than risking an orphaned tool_use.
func TestSafeSplitPointGivesUpAtZero(t *testing.T) {
	msgs := []api.Message{
		toolResultMsg("t1", "ok"),
		textMsg(api.RoleAssistant, "...."),
	}
	if got := SafeSplitPoint(msgs, 1); got != 0 {
		t.Errorf("got %d, want 0 (no safe boundary)", got)
	}
}

// Edge values bounce out without walking the slice.
func TestSafeSplitPointEdges(t *testing.T) {
	msgs := []api.Message{textMsg(api.RoleUser, "x")}
	if got := SafeSplitPoint(msgs, 0); got != 0 {
		t.Errorf("desired=0: got %d, want 0", got)
	}
	if got := SafeSplitPoint(msgs, 5); got != 1 {
		t.Errorf("desired beyond len: got %d, want len(msgs)", got)
	}
}

func TestNoCompactionIsPassthrough(t *testing.T) {
	msgs := []api.Message{
		textMsg(api.RoleUser, "a"),
		textMsg(api.RoleAssistant, "b"),
	}
	out, err := NoCompaction{}.Compact(context.Background(), msgs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(msgs) {
		t.Errorf("NoCompaction changed length: %d → %d", len(msgs), len(out))
	}
}

// Below the keep-window: no-op.
func TestSlidingWindowBelowThresholdIsNoop(t *testing.T) {
	msgs := []api.Message{
		textMsg(api.RoleUser, "a"),
		textMsg(api.RoleAssistant, "b"),
	}
	w := &SlidingWindow{KeepLast: 10}
	out, _ := w.Compact(context.Background(), msgs)
	if len(out) != len(msgs) {
		t.Errorf("below threshold should be a no-op, len changed %d → %d", len(msgs), len(out))
	}
}

// Above the keep-window: drops a prefix, snapping to a safe boundary.
func TestSlidingWindowAboveThresholdDropsPrefix(t *testing.T) {
	msgs := []api.Message{
		textMsg(api.RoleUser, "1"),
		textMsg(api.RoleAssistant, "2"),
		textMsg(api.RoleUser, "3"),
		textMsg(api.RoleAssistant, "4"),
		textMsg(api.RoleUser, "5"),
	}
	w := &SlidingWindow{KeepLast: 2}
	out, _ := w.Compact(context.Background(), msgs)
	if len(out) >= len(msgs) {
		t.Errorf("above threshold should shrink: %d → %d", len(msgs), len(out))
	}
	// First message must be a clean user-text message — never a tool_result.
	if len(out) > 0 && out[0].HasToolResult() {
		t.Error("output starts with a tool_result (split orphaned a tool_use)")
	}
}

// The whole point of SafeSplitPoint is that compaction never separates a
// tool_use from its tool_result. This is a regression test for that
// invariant on the SlidingWindow path.
func TestSlidingWindowNeverSplitsToolPair(t *testing.T) {
	msgs := []api.Message{
		textMsg(api.RoleUser, "q1"),
		textMsg(api.RoleAssistant, "a1"),
		textMsg(api.RoleUser, "q2"),
		toolUseMsg("t1", "echo"),
		toolResultMsg("t1", "ok"),
		textMsg(api.RoleAssistant, "done"),
		textMsg(api.RoleUser, "q3"),
	}
	// KeepLast = 3 would naively split at index 4 — right between tool_use
	// and tool_result. SafeSplitPoint should walk back instead.
	w := &SlidingWindow{KeepLast: 3}
	out, _ := w.Compact(context.Background(), msgs)
	for _, m := range out {
		if m.HasToolResult() {
			for _, b := range m.Content {
				if b.Type == api.BlockToolResult {
					// Make sure the matching tool_use is also in out.
					if !precedingHasToolUse(out, b.ToolUseID) {
						t.Errorf("tool_result %s in output without its tool_use", b.ToolUseID)
					}
				}
			}
		}
	}
}

// precedingHasToolUse reports whether any message in slice contains a
// tool_use block with the given id.
func precedingHasToolUse(messages []api.Message, id string) bool {
	for _, m := range messages {
		for _, b := range m.Content {
			if b.Type == api.BlockToolUse && b.ToolUseID == id {
				return true
			}
		}
	}
	return false
}
