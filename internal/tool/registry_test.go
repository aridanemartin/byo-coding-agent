package tool

import (
	"context"
	"strings"
	"testing"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

// fakeTool is a Tool used by tests that need a registered tool but don't
// care about its behavior — Execute just returns a canned string.
type fakeTool struct {
	name   string
	result string
	isErr  bool
}

func (f fakeTool) Definition() api.ToolDef {
	return api.ToolDef{Name: f.name, Description: f.name + " description"}
}

func (f fakeTool) Execute(_ context.Context, _ string) (string, bool) {
	return f.result, f.isErr
}

func TestRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeTool{name: "alpha"})

	got, ok := r.Get("alpha")
	if !ok {
		t.Fatal("Get returned not-ok for registered tool")
	}
	if got.Definition().Name != "alpha" {
		t.Errorf("Get returned wrong tool: %s", got.Definition().Name)
	}

	if _, ok := r.Get("missing"); ok {
		t.Error("Get of missing tool returned ok")
	}
}

// Definitions must be sorted by name. Two reasons matter — chapter 09's
// "map iteration is random" warning, and prompt caching (chapter 17),
// which silently invalidates on any byte-level reordering of the request.
func TestDefinitionsAreSortedByName(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeTool{name: "zebra"})
	r.Register(fakeTool{name: "alpha"})
	r.Register(fakeTool{name: "middle"})

	defs := r.Definitions()
	if len(defs) != 3 {
		t.Fatalf("got %d definitions, want 3", len(defs))
	}
	want := []string{"alpha", "middle", "zebra"}
	for i, d := range defs {
		if d.Name != want[i] {
			t.Errorf("defs[%d] = %q, want %q", i, d.Name, want[i])
		}
	}
}

func TestSubsetKeepsOnlyNamedTools(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeTool{name: "a"})
	r.Register(fakeTool{name: "b"})
	r.Register(fakeTool{name: "c"})

	sub := r.Subset("a", "c", "missing")

	if _, ok := sub.Get("a"); !ok {
		t.Error("subset missing 'a'")
	}
	if _, ok := sub.Get("c"); !ok {
		t.Error("subset missing 'c'")
	}
	if _, ok := sub.Get("b"); ok {
		t.Error("subset has 'b' (not requested)")
	}
	if _, ok := sub.Get("missing"); ok {
		t.Error("subset has nonexistent tool")
	}
}

// Subset of nothing is a valid empty registry — used by subagents that
// shouldn't have any tools at all.
func TestSubsetEmpty(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeTool{name: "a"})
	sub := r.Subset()
	if len(sub.Definitions()) != 0 {
		t.Errorf("Subset() should be empty, got %d defs", len(sub.Definitions()))
	}
}

func TestExecuteUnknownReturnsErrorString(t *testing.T) {
	r := NewRegistry()
	result, isErr := r.Execute(context.Background(), "nope", "{}")
	if !isErr {
		t.Error("Execute of unknown tool returned isErr=false")
	}
	if !strings.Contains(result, "unknown tool") {
		t.Errorf("result %q doesn't mention unknown tool", result)
	}
}

func TestExecuteDispatchesToTool(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeTool{name: "echo", result: "ok", isErr: false})

	result, isErr := r.Execute(context.Background(), "echo", "{}")
	if isErr {
		t.Error("Execute of known tool returned isErr=true")
	}
	if result != "ok" {
		t.Errorf("result = %q, want %q", result, "ok")
	}
}

// Tools that fail report it via the (result, isErr) channel rather than a
// Go error — the agent loop relies on this contract (chapter 02).
func TestExecutePropagatesIsErr(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeTool{name: "boom", result: "kaboom", isErr: true})

	result, isErr := r.Execute(context.Background(), "boom", "{}")
	if !isErr {
		t.Error("Execute didn't propagate isErr=true")
	}
	if result != "kaboom" {
		t.Errorf("result = %q, want %q", result, "kaboom")
	}
}
