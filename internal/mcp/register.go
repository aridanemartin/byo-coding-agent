package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/betta-tech/byo-coding-agent/internal/api"
	"github.com/betta-tech/byo-coding-agent/internal/tool"
)

// MCPTool wraps one remote tool so the agent's local Registry can dispatch
// to it. The agent loop sees a Tool like any other.
type MCPTool struct {
	Client *Client
	Def    api.ToolDef

	// remoteName is the tool's original name on the server. We expose it
	// under "<server>_<name>" in our registry to avoid collisions but call
	// the server using the original name.
	remoteName string
}

func (t *MCPTool) Definition() api.ToolDef { return t.Def }

func (t *MCPTool) Execute(ctx context.Context, input string) (string, bool) {
	// The Anthropic SDK gives us raw JSON for tool inputs; MCP expects an
	// object. We unmarshal into a map and pass that — the SDK marshals it
	// back to JSON on the wire.
	var args map[string]any
	if input != "" {
		if err := json.Unmarshal([]byte(input), &args); err != nil {
			return fmt.Sprintf("invalid tool input: %v", err), true
		}
	}
	out, isErr, err := t.Client.CallTool(ctx, t.remoteName, args)
	if err != nil {
		return err.Error(), true
	}
	return out, isErr
}

// ServerConfig describes one MCP server to connect to. Either Command (with
// optional Args) for stdio, or URL (with optional Headers) for HTTP. The
// loader expands ${VAR} in args, header values, and the URL.
type ServerConfig struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"` // "stdio" or "http"
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	URL       string            `json:"url,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// Config is the top-level JSON shape.
type Config struct {
	Servers []ServerConfig `json:"servers"`
}

// LoadConfig reads a JSON config file. Missing file returns (nil, nil) — MCP
// is opt-in; absence is not an error.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse mcp config %s: %w", path, err)
	}
	return &c, nil
}

// Register connects to every server in cfg and registers their tools into
// the supplied Registry. Returns the open clients so main can defer Close.
// Server failures are logged to stderr and skipped — losing one MCP server
// shouldn't kill the harness.
func Register(ctx context.Context, cfg *Config, registry *tool.Registry) []*Client {
	if cfg == nil {
		return nil
	}
	var clients []*Client
	for _, s := range cfg.Servers {
		client, err := dial(ctx, s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp: skip server %q: %v\n", s.Name, err)
			continue
		}
		defs, err := client.ListTools(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mcp: list tools from %q failed: %v\n", s.Name, err)
			_ = client.Close()
			continue
		}
		for _, d := range defs {
			props, required, ok := splitSchema(d.InputSchema)
			if !ok {
				fmt.Fprintf(os.Stderr, "mcp: skip %s/%s: unrecognized input schema\n", s.Name, d.Name)
				continue
			}
			registry.Register(&MCPTool{
				Client:     client,
				remoteName: d.Name,
				Def: api.ToolDef{
					Name:        s.Name + "_" + d.Name,
					Description: d.Description,
					InputSchema: props,
					Required:    required,
				},
			})
		}
		clients = append(clients, client)
	}
	return clients
}

func dial(ctx context.Context, s ServerConfig) (*Client, error) {
	switch s.Transport {
	case "stdio":
		if s.Command == "" {
			return nil, fmt.Errorf("stdio server %q missing command", s.Name)
		}
		args := make([]string, len(s.Args))
		for i, a := range s.Args {
			args[i] = os.ExpandEnv(a)
		}
		return NewStdioClient(ctx, s.Name, os.ExpandEnv(s.Command), args...)
	case "http":
		if s.URL == "" {
			return nil, fmt.Errorf("http server %q missing url", s.Name)
		}
		headers := make(map[string]string, len(s.Headers))
		for k, v := range s.Headers {
			headers[k] = os.ExpandEnv(v)
		}
		return NewHTTPClient(ctx, s.Name, os.ExpandEnv(s.URL), headers)
	default:
		return nil, fmt.Errorf("unknown transport %q (want stdio or http)", s.Transport)
	}
}

// splitSchema converts the SDK's `any` InputSchema into the (properties,
// required) pair our api.ToolDef expects. The Anthropic provider feeds
// InputSchema as the properties map and Required as a separate field.
func splitSchema(s any) (map[string]any, []string, bool) {
	m, ok := normalizeSchema(s)
	if !ok {
		return nil, nil, false
	}
	props, _ := m["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
	}
	var required []string
	if raw, ok := m["required"].([]any); ok {
		for _, r := range raw {
			if str, ok := r.(string); ok {
				required = append(required, str)
			}
		}
	}
	return props, required, true
}

func normalizeSchema(s any) (map[string]any, bool) {
	if s == nil {
		return map[string]any{}, true
	}
	if m, ok := s.(map[string]any); ok {
		return m, true
	}
	if raw, ok := s.(json.RawMessage); ok {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err == nil {
			return m, true
		}
	}
	return nil, false
}
