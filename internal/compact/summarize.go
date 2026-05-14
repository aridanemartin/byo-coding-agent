package compact

import (
	"context"
	"errors"
	"fmt"

	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/provider"
)

// Summarize calls the provider to summarize old turns once the history
// exceeds Threshold, replacing them with a single synthetic user message.
type Summarize struct {
	Provider     provider.Provider
	Threshold    int    // compact once len(messages) >= Threshold
	KeepRecent   int    // most-recent N messages left untouched
	Instructions string // optional override for the summarization prompt
}

const defaultSummarizeInstructions = `Summarize the following conversation concisely. ` +
	`Preserve facts, decisions, file paths, code identifiers, and anything else ` +
	`needed to continue the conversation. Output the summary directly with no preamble.`

func (s *Summarize) Compact(ctx context.Context, messages []api.Message) ([]api.Message, error) {
	if len(messages) < s.Threshold {
		return messages, nil
	}
	split := SafeSplitPoint(messages, len(messages)-s.KeepRecent)
	if split == 0 {
		return messages, nil
	}
	old, recent := messages[:split], messages[split:]

	instructions := s.Instructions
	if instructions == "" {
		instructions = defaultSummarizeInstructions
	}
	prompt := instructions + "\n\n" + api.RenderTranscript(old)

	resp, err := s.Provider.Send(ctx, []api.Message{{
		Role:    api.RoleUser,
		Content: []api.Block{{Type: api.BlockText, Text: prompt}},
	}}, nil)
	if err != nil {
		return messages, fmt.Errorf("summarize: %w", err)
	}

	var summary string
	for _, b := range resp.Content {
		if b.Type == api.BlockText {
			summary = b.Text
			break
		}
	}
	if summary == "" {
		return messages, errors.New("summarize: empty response")
	}

	fmt.Printf("[compacted %d messages → summary]\n", len(old))
	return append([]api.Message{{
		Role:    api.RoleUser,
		Content: []api.Block{{Type: api.BlockText, Text: "[earlier conversation summary]\n" + summary}},
	}}, recent...), nil
}
