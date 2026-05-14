// Package memory adds persistent context to the harness. The agent reads
// from a Store at startup (Preamble), can query it on demand (Recall),
// and writes back via the remember tool (Save). What "memory" looks like
// in practice depends on the Store implementation chosen — file-based,
// embedded vector store, anything that satisfies the three-method
// interface. See follow_along/en/19-agent-memory.md for the design
// rationale and a tour of alternative implementations.
package memory

import (
	"context"
	"time"
)

// Entry is one piece of memory. Kind tags the role of the content so
// implementations can route or group it — e.g., SessionFiles uses
// KindSessionSummary entries to drive the session-file header and the
// index.json record, while everything else goes into the session's body.
type Entry struct {
	Time    time.Time
	Kind    string
	Content string
	Tags    []string
}

// Recognised Kind values. Implementations are free to accept anything else;
// these are the conventions the rest of the harness uses.
const (
	KindFact           = "fact"
	KindDecision       = "decision"
	KindSessionSummary = "session-summary"
	KindPreference     = "preference"
)

// Store is the persistence layer. Three operations cover the natural rhythm
// of agent memory: write (Save), search (Recall), and the always-loaded
// preface that goes into the system prompt at session start (Preamble).
// Lifecycle methods like Close belong on concrete types — not all stores
// have I/O to flush, and the agent loop doesn't need to know.
type Store interface {
	Save(ctx context.Context, e Entry) error
	Recall(ctx context.Context, query string, limit int) ([]Entry, error)
	Preamble(ctx context.Context) (string, error)
}

// Default is the package-level Store the remember and recall tools dispatch
// through, exactly like delegate.go dispatches through subagent.Default.
// Defaults to NoMemory so the harness still boots if memory isn't wired up.
var Default Store = NoMemory{}

// NoMemory is the do-nothing fallback. Every method succeeds with empty
// output. Used as the initial value of Default and as the test seam for
// "what if memory isn't set up at all."
type NoMemory struct{}

func (NoMemory) Save(_ context.Context, _ Entry) error                      { return nil }
func (NoMemory) Recall(_ context.Context, _ string, _ int) ([]Entry, error) { return nil, nil }
func (NoMemory) Preamble(_ context.Context) (string, error)                 { return "", nil }
