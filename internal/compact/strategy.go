// Package compact holds the CompactionStrategy interface, helpers for safe
// truncation, and the strategies that ship with the harness.
//
// Strategies are called at the top of every agent loop turn; ones with a
// threshold return the input unchanged when the threshold isn't reached.
package compact

import (
	"context"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

type CompactionStrategy interface {
	Compact(ctx context.Context, messages []api.Message) ([]api.Message, error)
}

// SafeSplitPoint walks backward from `desired` to find an index where the
// conversation is in a "clean" state — no tool_use without its tool_result
// on either side of the split. The split happens just before a user message
// that is plain text (not tool_results). Returns 0 if no safe boundary is
// found, meaning "do nothing."
func SafeSplitPoint(messages []api.Message, desired int) int {
	if desired <= 0 {
		return 0
	}
	if desired >= len(messages) {
		return len(messages)
	}
	for i := desired; i > 0; i-- {
		if messages[i].Role == api.RoleUser && !messages[i].HasToolResult() {
			return i
		}
	}
	return 0
}
