// Package tool defines the Tool interface and a Registry that holds tools
// by name. Tools live as one file per tool in this package and self-register
// via init(), so adding a tool means dropping a file in this directory.
package tool

import (
	"fmt"
	"sort"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

type Tool interface {
	Definition() api.ToolDef
	Execute(input string) (result string, isError bool)
}

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(t Tool) {
	r.tools[t.Definition().Name] = t
}

// Definitions returns all registered tool schemas, sorted by name so the
// output is deterministic (important for prompt caching when you turn it on).
func (r *Registry) Definitions() []api.ToolDef {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]api.ToolDef, 0, len(r.tools))
	for _, n := range names {
		out = append(out, r.tools[n].Definition())
	}
	return out
}

// Execute dispatches a tool call by name. Unknown tools return an error
// result rather than panicking — the model can read it and recover.
func (r *Registry) Execute(name, input string) (string, bool) {
	t, ok := r.tools[name]
	if !ok {
		return fmt.Sprintf("unknown tool: %s", name), true
	}
	return t.Execute(input)
}

// Default is the package-level registry. Tools in this package self-register
// to it via init() — the "drop a file in, it appears" pattern.
var Default = NewRegistry()
