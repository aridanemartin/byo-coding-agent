# How to add a new tool

Goal: give the model a new capability — say, fetching a URL or running `git diff`.

The tool registry uses `init()` self-registration, so adding a tool means dropping a file in `internal/tool/`. No edits to `main.go`.

## Steps

### 1. Create `internal/tool/your_tool.go`

```go
package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"io"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

type WebFetchTool struct{}

func init() { Default.Register(&WebFetchTool{}) }

func (WebFetchTool) Definition() api.ToolDef {
	return api.ToolDef{
		Name:        "web_fetch",
		Description: "Fetch a URL and return the response body as text. Use for public web pages or APIs.",
		InputSchema: map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch (must be http or https).",
			},
		},
		Required: []string{"url"},
	}
}

func (WebFetchTool) Execute(ctx context.Context, rawInput string) (string, bool) {
	var in struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(rawInput), &in); err != nil {
		return err.Error(), true
	}
	req, err := http.NewRequestWithContext(ctx, "GET", in.URL, nil)
	if err != nil {
		return err.Error(), true
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err.Error(), true
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return err.Error(), true
	}
	return string(body), false
}
```

### 2. Run the harness

```sh
go run .
```

Type `/tools` — `web_fetch` is in the list. Ask the agent something like "fetch https://example.com and summarize it."

That's it.

## Conventions

- **Return errors as tool results.** `(message, true)` means "this failed, here's why." Don't return Go errors from `Execute` — the agent loop has no way to recover from those.
- **Use the context.** Long-running work (HTTP, shell, file walks) should accept `ctx` and pass it through so Ctrl-C cancellation propagates.
- **Cap your output.** A 10 MB response body will blow the model's context window. Use `io.LimitReader`, truncate strings, or paginate.
- **Be precise in `Description`.** The model picks tools by reading their descriptions. Vague descriptions ("does stuff with URLs") cause misuse; specific ones ("Fetch a URL and return the response body…") don't.
- **JSON Schema is what you have.** `type`, `description`, `enum`, `items`, `properties` are all supported. Required goes in the separate `Required` slice, not in the schema map.

## When `init()` won't work

Tools that need runtime configuration (API keys, a shared `*Provider`, a config struct) can't self-register because those values don't exist at package-load time. Two options:

**A. Construct in main, register explicitly:**

```go
// main.go
tool.Default.Register(&YourTool{APIKey: os.Getenv("YOUR_KEY")})
```

**B. Use a factory that lives in main:**

See [`delegate.go`](../../delegate.go) for the canonical example — `DelegateTool` needs a `Subagent`, which only exists at runtime, so it's constructed and registered in `main.go`.

## Limiting which subagents see the tool

By default every tool is visible to every agent that uses `tool.Default`. To restrict, use a `Subset`:

```go
subagent.Default.Register(subagent.Research{
	Provider: llm,
	Tools:    tool.Default.Subset("read_file", "web_fetch"),
})
```

The research subagent now sees only those two; the root agent still sees everything.

## See also

- [`follow_along/en/09-plug-and-play-tools.md`](../../follow_along/en/09-plug-and-play-tools.md) for the design rationale.
- [`follow_along/en/14-mcp-support.md`](../../follow_along/en/14-mcp-support.md) for how MCP wraps remote tools behind the same interface.
