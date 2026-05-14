// Package subagent defines the Subagent abstraction and a registry that
// holds them by name. Subagents are focused agents the root agent can
// delegate to via the delegate_<name> tool. Each subagent has its own
// system prompt, tool subset, model, and iteration budget.
//
// Subagents need configuration (a Provider, at least), so they don't
// self-register at init() time the way tools do. Construct them in main
// and call Default.Register.
package subagent

import (
	"context"
	"sort"
	"sync"
)

// Subagent is one focused agent type. Run takes a task description and
// returns the final text — same shape as a tool but with its own context
// window underneath.
type Subagent interface {
	Name() string
	Description() string
	Run(ctx context.Context, task string) (string, error)
}

type Registry struct {
	mu        sync.RWMutex
	subagents map[string]Subagent
}

func NewRegistry() *Registry {
	return &Registry{subagents: map[string]Subagent{}}
}

func (r *Registry) Register(s Subagent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subagents[s.Name()] = s
}

func (r *Registry) All() []Subagent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.subagents))
	for n := range r.subagents {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Subagent, 0, len(names))
	for _, n := range names {
		out = append(out, r.subagents[n])
	}
	return out
}

// Active tracks subagents currently running. Used by the UI to show
// "what's executing right now."
type tracker struct {
	mu     sync.Mutex
	active map[string]int // name -> count of in-flight runs
}

var trk = &tracker{active: map[string]int{}}

// Begin marks a subagent run as started. Returns a function to be deferred
// that decrements the count on completion.
func Begin(name string) func() {
	trk.mu.Lock()
	trk.active[name]++
	trk.mu.Unlock()
	return func() {
		trk.mu.Lock()
		trk.active[name]--
		if trk.active[name] == 0 {
			delete(trk.active, name)
		}
		trk.mu.Unlock()
	}
}

// Active returns a snapshot of currently running subagents (name -> count).
// Used by the /subagents command and any UI affordance that wants to show
// what's in flight.
func Active() map[string]int {
	trk.mu.Lock()
	defer trk.mu.Unlock()
	out := make(map[string]int, len(trk.active))
	for k, v := range trk.active {
		out[k] = v
	}
	return out
}

// Default is the package-level registry. Configure subagents in main and
// register them here; the delegate tool reads from this registry to expose
// each one to the model.
var Default = NewRegistry()
