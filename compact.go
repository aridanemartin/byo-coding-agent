package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// CompactionStrategy decides whether and how to shrink the message history.
// Called at the top of every agent loop turn; strategies that have a threshold
// return the input unchanged when the threshold isn't reached.
type CompactionStrategy interface {
	Compact(ctx context.Context, messages []Message) ([]Message, error)
}

// NoCompaction is the default — never modifies messages.
type NoCompaction struct{}

func (NoCompaction) Compact(_ context.Context, messages []Message) ([]Message, error) {
	return messages, nil
}

// SlidingWindow keeps the last KeepLast messages and drops everything older.
// It snaps to a safe boundary so it never separates a tool_use from its
// tool_result.
type SlidingWindow struct {
	KeepLast int
}

func (s *SlidingWindow) Compact(_ context.Context, messages []Message) ([]Message, error) {
	if len(messages) <= s.KeepLast {
		return messages, nil
	}
	split := safeSplitPoint(messages, len(messages)-s.KeepLast)
	return messages[split:], nil
}

// Summarize calls the provider to summarize old turns once the history
// exceeds Threshold, replacing them with a single synthetic user message.
type Summarize struct {
	Provider     Provider
	Threshold    int    // compact once len(messages) >= Threshold
	KeepRecent   int    // most-recent N messages left untouched
	Instructions string // optional override for the summarization prompt
}

const defaultSummarizeInstructions = `Summarize the following conversation concisely. ` +
	`Preserve facts, decisions, file paths, code identifiers, and anything else ` +
	`needed to continue the conversation. Output the summary directly with no preamble.`

func (s *Summarize) Compact(ctx context.Context, messages []Message) ([]Message, error) {
	if len(messages) < s.Threshold {
		return messages, nil
	}
	split := safeSplitPoint(messages, len(messages)-s.KeepRecent)
	if split == 0 {
		return messages, nil // nothing safe to compact
	}
	old, recent := messages[:split], messages[split:]

	instructions := s.Instructions
	if instructions == "" {
		instructions = defaultSummarizeInstructions
	}
	prompt := instructions + "\n\n" + renderTranscript(old)

	resp, err := s.Provider.Send(ctx, []Message{{
		Role:    RoleUser,
		Content: []Block{{Type: BlockText, Text: prompt}},
	}}, nil)
	if err != nil {
		return messages, fmt.Errorf("summarize: %w", err)
	}

	var summary string
	for _, b := range resp.Content {
		if b.Type == BlockText {
			summary = b.Text
			break
		}
	}
	if summary == "" {
		return messages, errors.New("summarize: empty response")
	}

	fmt.Printf("%s[compacted %d messages → summary]%s\n", ansiDim, len(old), ansiReset)
	return append([]Message{{
		Role:    RoleUser,
		Content: []Block{{Type: BlockText, Text: "[earlier conversation summary]\n" + summary}},
	}}, recent...), nil
}

// safeSplitPoint walks backward from `desired` to find an index where the
// conversation is in a "clean" state — no tool_use without its tool_result on
// either side of the split. The split happens just before a user message that
// is plain text (not tool_results).
func safeSplitPoint(messages []Message, desired int) int {
	if desired <= 0 {
		return 0
	}
	if desired >= len(messages) {
		return len(messages)
	}
	for i := desired; i > 0; i-- {
		if messages[i].Role == RoleUser && !hasToolResult(messages[i]) {
			return i
		}
	}
	return 0
}

// LoggingStrategy wraps another strategy and appends each non-trivial
// compaction (before / after) to a file. Use it to inspect strategies and
// compare them side-by-side without touching their implementations.
type LoggingStrategy struct {
	Inner    CompactionStrategy
	FilePath string
}

// WithLogging is sugar for &LoggingStrategy{Inner: inner, FilePath: path}.
func WithLogging(inner CompactionStrategy, path string) *LoggingStrategy {
	return &LoggingStrategy{Inner: inner, FilePath: path}
}

func (l *LoggingStrategy) Compact(ctx context.Context, messages []Message) ([]Message, error) {
	before := messages
	after, err := l.Inner.Compact(ctx, messages)
	if err != nil {
		return after, err
	}
	// Skip the no-op case: the strategy decided not to act this turn.
	if len(after) == len(before) {
		return after, nil
	}
	if l.FilePath == "" {
		return after, nil
	}
	if writeErr := l.writeEvent(before, after); writeErr != nil {
		fmt.Printf("compaction log write failed: %v\n", writeErr)
	}
	return after, nil
}

func (l *LoggingStrategy) writeEvent(before, after []Message) error {
	f, err := os.OpenFile(l.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintln(f, "=========================")
	fmt.Fprintf(f, "[%s] compaction event\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(f, "BEFORE (%d messages):\n%s\n", len(before), renderTranscript(before))
	fmt.Fprintln(f, "---")
	fmt.Fprintf(f, "AFTER (%d messages):\n%s\n", len(after), renderTranscript(after))
	return nil
}

func hasToolResult(m Message) bool {
	for _, b := range m.Content {
		if b.Type == BlockToolResult {
			return true
		}
	}
	return false
}

// printCompaction writes a before/after diff to stdout. Used by /verbose mode.
func printCompaction(before, after []Message) {
	fmt.Println(ansiDim + "─── compaction ───" + ansiReset)
	fmt.Printf("BEFORE (%d messages):\n%s", len(before), renderTranscript(before))
	fmt.Println(ansiDim + "─── after ───" + ansiReset)
	fmt.Printf("AFTER (%d messages):\n%s", len(after), renderTranscript(after))
	fmt.Println(ansiDim + "──────────────" + ansiReset)
}

func renderTranscript(msgs []Message) string {
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
