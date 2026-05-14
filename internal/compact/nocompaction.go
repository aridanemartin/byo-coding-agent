package compact

import (
	"context"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

// NoCompaction is the default — never modifies messages.
type NoCompaction struct{}

func (NoCompaction) Compact(_ context.Context, messages []api.Message) ([]api.Message, error) {
	return messages, nil
}
