package compact

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

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

func (l *LoggingStrategy) Compact(ctx context.Context, messages []api.Message) ([]api.Message, error) {
	before := messages
	after, err := l.Inner.Compact(ctx, messages)
	if err != nil {
		return after, err
	}
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

func (l *LoggingStrategy) writeEvent(before, after []api.Message) error {
	f, err := os.OpenFile(l.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	fmt.Fprintln(f, "=========================")
	fmt.Fprintf(f, "[%s] compaction event\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(f, "BEFORE (%d messages):\n%s\n", len(before), api.RenderTranscript(before))
	fmt.Fprintln(f, "---")
	fmt.Fprintf(f, "AFTER (%d messages):\n%s\n", len(after), api.RenderTranscript(after))
	return nil
}
