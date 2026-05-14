package compact

import (
	"context"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

// SlidingWindow keeps the last KeepLast messages and drops everything older.
// It snaps to a safe boundary so it never separates a tool_use from its
// tool_result.
type SlidingWindow struct {
	KeepLast int
}

func (s *SlidingWindow) Compact(_ context.Context, messages []api.Message) ([]api.Message, error) {
	if len(messages) <= s.KeepLast {
		return messages, nil
	}
	split := SafeSplitPoint(messages, len(messages)-s.KeepLast)
	return messages[split:], nil
}
