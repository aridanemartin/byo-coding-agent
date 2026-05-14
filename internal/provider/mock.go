package provider

import (
	"context"
	"sync"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

// MockProvider is a Provider implementation that returns canned responses.
// It's the recommended way to test the agent loop, compaction strategies,
// and any code that talks through the Provider interface — no API key, no
// network, no spend.
//
// Beyond returning Responses in order, the mock records every Send call so
// tests can assert on the messages and tools the agent built up. See
// [internal/agent/agent_test.go] for examples of every pattern (text-only,
// tool-use loop, max-turns clamp, provider error).
//
// Safe for concurrent use — the agent loop is single-threaded today but
// subagents (chapter 11) run on goroutines that may share a provider.
type MockProvider struct {
	// Responses is consumed front-to-back. The i-th Send returns
	// Responses[i] (or a fall-through depending on RepeatLast — see below).
	Responses []api.Response

	// RepeatLast, when true, causes the final element of Responses to be
	// returned for every Send after the slice is exhausted. Useful for
	// tests that need a "loops forever" provider (e.g. asserting that
	// Agent.MaxTurns kicks in). When false (the default) Send returns a
	// plain end_turn response once Responses is empty.
	RepeatLast bool

	// Err, if non-nil, is returned from every Send call instead of a
	// response. Lets tests exercise error-handling paths in the agent
	// loop. Responses is ignored while Err is set.
	Err error

	// ModelName backs Model() / SetModel(). Defaults to "mock" when zero.
	ModelName string

	mu    sync.Mutex
	sent  [][]api.Message
	tools [][]api.ToolDef
	calls int
}

// NewMockProvider constructs a MockProvider preloaded with the given
// canned responses. Equivalent to &MockProvider{Responses: responses} but
// reads better at call sites:
//
//	p := provider.NewMockProvider(
//	    api.Response{StopReason: api.StopEndTurn, Content: ...},
//	)
func NewMockProvider(responses ...api.Response) *MockProvider {
	return &MockProvider{Responses: responses}
}

// Send returns the next canned response (or Err, or a default end_turn),
// recording the messages and tools it was called with for later assertion.
func (p *MockProvider) Send(_ context.Context, messages []api.Message, tools []api.ToolDef) (api.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Err != nil {
		p.calls++
		return api.Response{}, p.Err
	}

	// Snapshot inputs so tests can introspect what the agent built up.
	// The defensive copy stops a test from accidentally mutating shared
	// state that the agent owns.
	p.sent = append(p.sent, append([]api.Message(nil), messages...))
	p.tools = append(p.tools, append([]api.ToolDef(nil), tools...))

	var r api.Response
	switch {
	case p.calls < len(p.Responses):
		r = p.Responses[p.calls]
	case p.RepeatLast && len(p.Responses) > 0:
		r = p.Responses[len(p.Responses)-1]
	default:
		r = api.Response{StopReason: api.StopEndTurn}
	}
	p.calls++
	return r, nil
}

func (p *MockProvider) Model() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ModelName == "" {
		return "mock"
	}
	return p.ModelName
}

func (p *MockProvider) SetModel(name string) {
	p.mu.Lock()
	p.ModelName = name
	p.mu.Unlock()
}

// Calls returns how many times Send has been invoked.
func (p *MockProvider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// SentAt returns a copy of the messages passed to the i-th Send call.
// Indexing is 0-based and matches the order of Responses. Returns nil
// when i is out of range.
func (p *MockProvider) SentAt(i int) []api.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	if i < 0 || i >= len(p.sent) {
		return nil
	}
	return p.sent[i]
}

// LastSent is shorthand for the most recent SentAt.
func (p *MockProvider) LastSent() []api.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.sent) == 0 {
		return nil
	}
	return p.sent[len(p.sent)-1]
}
