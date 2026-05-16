# 14 · Adding MCP support

The Tool registry from chapter 09 lets you drop a Go file into `internal/tool/` and have the agent pick it up automatically — `Definition()` plus `Execute()`, `init()`-registered, done. That worked because every tool we wanted was a local operation we could write in Go.

**MCP — the Model Context Protocol** — is the standard for tools that live *outside* your process. An MCP server can be a Git operations server, a Slack reader, a database query interface, a filesystem mounter, anything someone else has written and published. Adding MCP support means letting the agent use those servers as if they were local tools.

This chapter is about bridging the two worlds: an external protocol on one side, our `Tool` interface on the other.

## What MCP actually is

MCP is a JSON-RPC protocol. An MCP server speaks one of three transports:

| Transport | Used for |
|---|---|
| **stdio** | Local subprocesses. The server is a binary you launch; you talk to it on its stdin/stdout. |
| **HTTP / SSE** | Remote servers, multi-tenant deployments. |
| **WebSocket** | Bidirectional remote, less common. |

The protocol defines:

- `tools/list` — what tools does this server expose?
- `tools/call` — invoke a tool by name with arguments, get a result back
- `resources/list` / `resources/read` — file-like read-only data
- `prompts/list` / `prompts/get` — server-provided prompt templates

For our harness we care about `tools/list` and `tools/call`. The rest is optional.

### "Do I need to install or download anything?"

Short answer: **MCP the protocol never mandates a download.** It's transport-agnostic JSON-RPC. What you need depends on the transport:

- **stdio (local subprocess).** The server is a program on your machine. The client `exec`s it on demand and pipes JSON-RPC over stdin/stdout. So *something* has to live on disk — but that "something" can arrive any way you like:
  - installed ahead of time (`pip install mcp-server-foo`, `npm install -g ...`, a `wget`'d binary)
  - fetched lazily on first run by a launcher like `uvx` or `npx -y`, which then caches it
  - a script you wrote yourself in the project folder

  The protocol just says "run this command and talk to me." Whether the command involves a download is between you and that command.

- **HTTP / SSE / WebSocket (remote).** The server runs *somewhere else* — your LAN, a cloud service, a vendor's endpoint. You connect to a URL, possibly with auth headers. Nothing to install on your side; nothing to spawn. The server has to be reachable when you call it.

Nothing is "always running." stdio servers live for the duration of the session (the client spawns them, the client kills them). Remote servers have to be running on the other end, but that's their operator's problem.

> For the protocol spec and a catalog of public servers, see the official docs at [modelcontextprotocol.io](https://modelcontextprotocol.io). The spec itself is at [spec.modelcontextprotocol.io](https://spec.modelcontextprotocol.io).

## The architectural fit

Look at our `Tool` interface again:

```go
type Tool interface {
    Definition() api.ToolDef
    Execute(ctx context.Context, input string) (result string, isError bool)
}
```

Nothing in there says "implemented in Go." It says "given an input string, produce a result string." That's a perfect fit for remote dispatch.

So the design is: **one wrapper struct per remote tool, registered in the same `tool.Default` registry as the local ones.** The agent loop has no idea which tools are local and which are remote. From the model's point of view, there's just one flat tool list.

```
┌─────────────────────────────────────────────┐
│ tool.Default registry                       │
│                                             │
│  bash         (BashTool — local Go)         │
│  read_file    (ReadFileTool — local Go)     │
│  write_file   (WriteFileTool — local Go)    │
│  git_status   (MCPTool → git MCP server)    │
│  git_diff     (MCPTool → git MCP server)    │
│  query_db     (MCPTool → postgres server)   │
└─────────────────────────────────────────────┘
```

## The MCP client

You don't write the JSON-RPC machinery by hand. Two options:

1. **The official Go SDK**, `github.com/modelcontextprotocol/go-sdk`. Handles transport, framing, lifecycle.
2. **The Anthropic SDK's built-in MCP helpers** — convenient if you already use the Anthropic SDK, but couples your MCP code to that provider.

We go with option 1 — it keeps MCP independent of the LLM provider, consistent with chapter 03's philosophy.

A minimal wrapper:

```go
// internal/mcp/client.go
type Client struct {
    name string
    impl *mcp.Client
}

func NewStdioClient(ctx context.Context, name, command string, args ...string) (*Client, error) {
    transport := mcp.NewStdioTransport(command, args...)
    impl := mcp.NewClient(&mcp.Implementation{Name: "bettatech-harness", Version: "0.1"}, nil)
    if err := impl.Connect(ctx, transport); err != nil {
        return nil, err
    }
    return &Client{name: name, impl: impl}, nil
}

func (c *Client) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
    return c.impl.ListTools(ctx, nil)
}

func (c *Client) CallTool(ctx context.Context, name, input string) (string, bool, error) {
    res, err := c.impl.CallTool(ctx, &mcp.CallToolParams{
        Name:      name,
        Arguments: json.RawMessage(input),
    })
    if err != nil { return "", true, err }
    return res.Text(), res.IsError, nil
}

func (c *Client) Close() error { return c.impl.Close() }
```

About 30 lines. The real work is in the SDK; this is just a typed wrapper that matches our codebase's idioms.

## The bridge: `MCPTool` implements `Tool`

For each tool the server exposes, we register a wrapper that satisfies the local `Tool` interface:

```go
// internal/mcp/tool.go
type MCPTool struct {
    Client *Client
    def    api.ToolDef
}

func (t *MCPTool) Definition() api.ToolDef { return t.def }

func (t *MCPTool) Execute(ctx context.Context, input string) (string, bool) {
    out, isErr, err := t.Client.CallTool(ctx, t.def.Name, input)
    if err != nil { return err.Error(), true }
    return out, isErr
}
```

That's the bridge. The agent loop, the registry, the approval flow — none of them change. The model sees `git_status` in its tool list and calls it the same way it calls `read_file`.

## Wiring in `main.go`

MCP servers are runtime dependencies (you have to launch them), so registration is explicit. Rather than hardcoding the list in Go, the harness loads it from a JSON file at startup:

```go
// main.go
func setupMCP(ctx context.Context) []*mcp.Client {
    cfg, err := mcp.LoadConfig("mcp.json")
    if err != nil {
        fmt.Fprintf(os.Stderr, "mcp: config error: %v\n", err)
        return nil
    }
    return mcp.Register(ctx, cfg, tool.Default)
}
```

The config file format:

```json
{
  "servers": [
    {"name": "git", "transport": "stdio", "command": "uvx", "args": ["mcp-server-git"]},
    {"name": "github", "transport": "http", "url": "https://api.githubcopilot.com/mcp/",
     "headers": {"Authorization": "Bearer ${GITHUB_TOKEN}"}}
  ]
}
```

`${VAR}` references in commands, args, URLs, and header values are expanded via `os.ExpandEnv` — credentials live in your shell environment, not in a file you might accidentally commit.

**Failure mode is "skip the server, keep going."** If `uvx` isn't installed, or the git MCP server crashes at launch, or the JSON has a malformed entry, the harness logs and continues. The agent gets fewer tools but still works. This matches the existing harness pattern — losing a tool isn't a fatal error. The config file itself being absent isn't even a log — MCP is opt-in.

## When the config gets read

`setupMCP(ctx)` runs **once, at startup**, and its position in `main.go` matters in three ways:

1. **Before subagent registration** — so a subagent's curated tool subset can include MCP-backed tools (`tool.Default.Subset("read_file", "deepwiki_ask_question")`).
2. **Before the stdout pipe redirect** (chapter 12) — connection errors print to your real terminal, not into the TUI scrollback where they're easy to miss.
3. **Before `program.Run()`** — the TUI opens with the full tool list already populated; `/tools` shows everything from the first frame.

Two consequences worth flagging:

- **The path is relative to the working directory** — `mcp.json` in whatever folder you ran `go run .` from. Not relative to the binary.
- **No hot-reload.** Edit `mcp.json` and you have to restart the harness. The `tools/list` call also only happens once per server, so a server that registers new tools mid-session won't surface them until restart.

If you want to take this further — a `/reload-mcp` slash command, a watcher on the file, an XDG-style per-project config path — the entry point is one function (`setupMCP`) and the rest of the harness doesn't know the difference.

## Why name-prefix the tools

Two MCP servers might both expose `read_file`. An MCP server's tool might shadow a local one. The registry is a flat namespace; the second registration silently wins.

Prefixing with the server name (`git_status`, `filesystem_read_file`) sidesteps the whole problem. Trivial, but easy to forget until you're debugging "why is my `read_file` returning weird JSON."

## Approval still works

The model calls `git_status`. The harness asks for approval. You say yes. `registry.Execute` dispatches to `MCPTool.Execute`, which makes the RPC. The result comes back as a `tool_result`, just like a local tool.

The permission gate is one place. It doesn't care that the call is going to a subprocess. That's the payoff for putting approval at the harness layer back in chapter 02.

## Lifecycle and cleanup

Stdio MCP servers are subprocesses. They survive across turns. They need to be shut down when the harness exits, or you leak processes — each `go run .` followed by Ctrl-D leaves an `mcp-server-git` orphaned.

```go
// in main
clients := registerMCPServers(ctx)
defer func() {
    for _, c := range clients { _ = c.Close() }
}()
```

For HTTP transports, "close" means tearing down the long-lived connection. Same shape.

## Pitfalls

**Schema translation.** MCP's input schemas are JSON Schema. Our `ToolDef.InputSchema` is also JSON Schema (`map[string]any`). Mostly they line up, but MCP servers occasionally use `$ref` and other advanced features that the Anthropic API rejects. If a tool's schema fails validation, skip that tool rather than the whole server.

**Slow `tools/list`.** Some MCP servers do real work at startup — open databases, fetch credentials, scan filesystems. `ListTools` can take seconds. Launching servers serially in `main` blocks REPL startup. The production answer is launching them in goroutines and registering as they finish; we accept the serial latency for a learning project.

**Re-using the wrong context.** Every `CallTool` should pass `ctx` from the agent. If the agent's context is canceled (Ctrl-C), in-flight MCP calls cancel too. Don't use `context.Background()` inside `Execute` — you'd lose cancellation.

**Trusting remote tools.** An MCP server you didn't write is code you didn't audit. The permission gate is doing real safety work here — every call still goes through `approve?`. Don't auto-approve MCP tools just because they "look" read-only.

## Now try

1. Install one of the standard MCP servers (`uvx mcp-server-git` or `npx @modelcontextprotocol/server-filesystem .`). Copy `mcp.example.json` to `mcp.json`, add an entry for it, run the harness, and type `/tools` to confirm the new tools appear.
2. Ask the agent a git-related question (`what's changed since main?`). Watch it pick `git_diff` from MCP instead of running `bash`. Compare the result quality.
3. Write a *tiny* MCP server of your own. There are SDKs for Python, TypeScript, Go. Expose one tool: `current_time`. Wire it into the harness. Total round trip: probably under an hour.

← [back to TOC](README.md)
