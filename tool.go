package main

import (
	"fmt"
	"sort"
)

// Tool is a self-contained capability the model can call. Each tool packages
// its schema (Definition) with its behavior (Execute) so adding one means
// writing one file — no edits to a central switch statement.
type Tool interface {
	Definition() ToolDef
	Execute(input string) (result string, isError bool)
}

// ToolRegistry holds tools by name. The agent loop reads its Definitions to
// tell the model what's available, and dispatches via Execute by name.
type ToolRegistry struct {
	tools map[string]Tool
}

func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: map[string]Tool{}}
}

func (r *ToolRegistry) Register(t Tool) {
	r.tools[t.Definition().Name] = t
}

// Definitions returns all registered tool schemas, sorted by name so the
// output is deterministic (important for prompt caching when you turn it on).
func (r *ToolRegistry) Definitions() []ToolDef {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]ToolDef, 0, len(r.tools))
	for _, n := range names {
		out = append(out, r.tools[n].Definition())
	}
	return out
}

// Execute dispatches a tool call by name. Unknown tools return an error
// result rather than panicking — the model can read it and recover.
func (r *ToolRegistry) Execute(name, input string) (string, bool) {
	t, ok := r.tools[name]
	if !ok {
		return fmt.Sprintf("unknown tool: %s", name), true
	}
	return t.Execute(input)
}

// registry is the global tool registry. Tools self-register via init() in
// their own file — this is the "drop a file in, it appears" pattern.
var registry = NewToolRegistry()
