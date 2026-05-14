# How to add a permission policy

Goal: replace "ask every time" with something smarter — auto-allow `read_file`, ask once per shell command, deny writes outside the working directory.

The harness ships with one strategy: `Agent.Confirm` is called on every tool call, and the answer is yes/no. There's no abstraction yet. The first time you want anything else, you introduce a `PermissionPolicy` interface and route the agent through it. This guide walks both: the one-time abstraction work, then the recipe for adding new policies after that.

## Part 1 — one-time: introduce the abstraction

### 1. Add the policy interface

Create `internal/permission/policy.go`:

```go
// Package permission decides whether a tool call should run, ask, or be denied.
// The agent loop consults a Policy before every tool call.
package permission

import "context"

type Decision int

const (
	DecisionAsk Decision = iota // route through Agent.Confirm
	DecisionAllow                // run without prompting
	DecisionDeny                 // refuse; reason is shown to the model
)

type Policy interface {
	Decide(ctx context.Context, name, input string) (Decision, string)
}
```

The second return value is a reason string. For `DecisionDeny`, it becomes the tool result the model sees (so it can self-correct). For `DecisionAllow` and `DecisionAsk`, it's ignored.

### 2. Refactor `Agent.executeTool`

In [`internal/agent/agent.go`](../../internal/agent/agent.go), add a `Policy` field:

```go
type Agent struct {
	// … existing fields …
	Policy   permission.Policy
}
```

And change `executeTool` to consult it:

```go
func (a *Agent) executeTool(ctx context.Context, name, rawInput string) (string, bool) {
	fmt.Printf("%s[tool] %s %s\n", a.LogPrefix, name, rawInput)

	decision := permission.DecisionAsk
	reason := ""
	if a.Policy != nil {
		decision, reason = a.Policy.Decide(ctx, name, rawInput)
	}

	switch decision {
	case permission.DecisionAllow:
		// fall through
	case permission.DecisionDeny:
		if reason == "" {
			reason = "permission policy denied this tool call"
		}
		return reason, true
	case permission.DecisionAsk:
		if a.Confirm != nil && !a.Confirm("approve?") {
			return "user denied this tool call", true
		}
	}
	return a.Tools.Execute(ctx, name, rawInput)
}
```

Nil `Policy` falls through to the old behavior (always ask). Backward-compatible.

### 3. Wire a default in `main.go`

```go
import "github.com/betta-tech/byo-coding-agent/internal/permission"

// In main(), after constructing rootAgent:
rootAgent.Policy = permission.AlwaysAsk{}
```

You're done with the abstraction. Build, run — behavior is unchanged because `AlwaysAsk` returns `DecisionAsk` for everything, which is exactly what the old code did.

## Part 2 — repeatable: add a new policy

After Part 1, adding a policy is one new file. Each policy is a struct implementing `permission.Policy`.

### Example: `AlwaysAllow`

```go
// internal/permission/always_allow.go
package permission

import "context"

type AlwaysAllow struct{}

func (AlwaysAllow) Decide(_ context.Context, _, _ string) (Decision, string) {
	return DecisionAllow, ""
}
```

### Example: `AllowList`

Auto-allow named tools, ask for everything else:

```go
// internal/permission/allow_list.go
package permission

import "context"

type AllowList struct {
	Names map[string]bool
}

func (a AllowList) Decide(_ context.Context, name, _ string) (Decision, string) {
	if a.Names[name] {
		return DecisionAllow, ""
	}
	return DecisionAsk, ""
}
```

Wire it:

```go
rootAgent.Policy = permission.AllowList{
	Names: map[string]bool{
		"read_file":      true,
		"deepwiki_ask_question": true,
	},
}
```

### Example: `AskOnce`

Ask the first time for each tool name, remember the answer for the session:

```go
// internal/permission/ask_once.go
package permission

import (
	"context"
	"sync"
)

type AskOnce struct {
	mu      sync.Mutex
	allowed map[string]bool
}

func (a *AskOnce) Decide(_ context.Context, name, _ string) (Decision, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.allowed == nil {
		a.allowed = map[string]bool{}
	}
	if a.allowed[name] {
		return DecisionAllow, ""
	}
	// First call: ask, and have the caller remember the answer.
	// We can't mutate from inside Decide because we don't know the answer
	// yet — see "Pitfalls" below for how to handle this.
	return DecisionAsk, ""
}

// AfterAsk is called by the agent loop after a successful Confirm to
// record the user's "yes" so we don't ask again for this tool.
func (a *AskOnce) AfterAsk(name string, allowed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.allowed == nil {
		a.allowed = map[string]bool{}
	}
	a.allowed[name] = allowed
}
```

This one needs a small change to `executeTool` — after the `Confirm` returns, call `policy.AfterAsk` if the policy supports it (type-assertion, same idiom as `tokenReporter` in `commands.go`). Worth doing once, then every `AskOnce`-style policy reuses the hook.

### Example: `PathScoped`

Allow `read_file` / `write_file` only if their path stays under CWD:

```go
// internal/permission/path_scoped.go
package permission

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type PathScoped struct {
	// Inner is consulted for everything that isn't a path-shaped tool.
	Inner Policy
}

func (p PathScoped) Decide(ctx context.Context, name, input string) (Decision, string) {
	switch name {
	case "read_file", "write_file":
		var in struct{ Path string `json:"path"` }
		if err := json.Unmarshal([]byte(input), &in); err != nil {
			return DecisionDeny, "invalid tool input"
		}
		cwd, _ := os.Getwd()
		abs, err := filepath.Abs(in.Path)
		if err != nil || !strings.HasPrefix(abs, cwd+string(filepath.Separator)) {
			return DecisionDeny, "path is outside the working directory"
		}
		return DecisionAllow, ""
	}
	if p.Inner != nil {
		return p.Inner.Decide(ctx, name, input)
	}
	return DecisionAsk, ""
}
```

Composes with other policies: `PathScoped{Inner: AllowList{...}}` enforces the path boundary *and* defers to an allow-list for everything else.

## Conventions

- **Policies are stateless unless they need session memory.** Most are simple structs with config fields and no goroutines.
- **Stateful policies need a mutex.** `AskOnce` is the example — subagents may run on goroutines that hit the same policy.
- **Composition over enum.** Build complex policies by wrapping simpler ones (`PathScoped{Inner: AskOnce{Inner: AllowList{...}}}`), not by adding cases to a `Decide` switch. Cleaner to extend.
- **Reasons are model-facing.** When you return `DecisionDeny`, the reason string is fed to the model as a tool result with `is_error: true`. Write it as if you're telling the model what happened: "path is outside the working directory" works; "ENOENT" doesn't.

## Pitfalls

- **Don't make the policy interactive.** `Decide` should be synchronous and instant. If you need a prompt, return `DecisionAsk` and let the existing `Confirm` flow handle it.
- **Test on subagents.** Subagents (chapter 11) use the same policy as the root agent by default. A policy that asks for shell will block a subagent that was supposed to run unattended. Decide whether subagents get a different policy at registration time.
- **`Decide` runs inside the agent loop**, which holds the conversation context. Don't block on network calls, don't call the model from inside `Decide`. Any "AI-judged permission" feature needs a different design.

## See also

- [`follow_along/en/02-the-permission-gate.md`](../../follow_along/en/02-the-permission-gate.md) for the "ask every time" baseline and the design rationale for putting approval at the harness layer.
