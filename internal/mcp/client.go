// Package mcp adapts external Model Context Protocol servers into the
// harness's local Tool interface. Stdio (subprocess) and HTTP transports
// are both supported; the wrapping bridge in register.go is the same.
package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Client wraps an MCP session and the transport it was opened on. One Client
// corresponds to one connected server; Close terminates it.
type Client struct {
	Name    string
	session *sdk.ClientSession
}

// NewStdioClient launches command/args as a subprocess and connects over its
// stdin/stdout. The Connect call blocks until the server has responded to the
// initialize handshake or the context is canceled.
func NewStdioClient(ctx context.Context, name, command string, args ...string) (*Client, error) {
	transport := &sdk.CommandTransport{Command: exec.Command(command, args...)}
	return connect(ctx, name, transport)
}

// NewHTTPClient connects to a remote MCP server over the Streamable HTTP
// transport. Headers (auth tokens, API keys) are injected via a RoundTripper
// so they ride every request the session makes.
func NewHTTPClient(ctx context.Context, name, endpoint string, headers map[string]string) (*Client, error) {
	httpClient := &http.Client{
		Transport: &headerRoundTripper{
			base:    http.DefaultTransport,
			headers: headers,
		},
	}
	transport := &sdk.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: httpClient,
	}
	return connect(ctx, name, transport)
}

func connect(ctx context.Context, name string, transport sdk.Transport) (*Client, error) {
	impl := &sdk.Implementation{Name: "bettatech-harness", Version: "0.1"}
	c := sdk.NewClient(impl, nil)
	session, err := c.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("mcp connect %s: %w", name, err)
	}
	return &Client{Name: name, session: session}, nil
}

// ListTools returns every tool the server exposes. Called once at startup
// per server, results cached in our local registry.
func (c *Client) ListTools(ctx context.Context) ([]*sdk.Tool, error) {
	res, err := c.session.ListTools(ctx, nil)
	if err != nil {
		return nil, err
	}
	return res.Tools, nil
}

// CallTool dispatches a tool invocation to the remote server. The result's
// content blocks (the SDK supports text, image, audio, etc.) are concatenated
// to a single string — our Tool interface returns one string. Anything that
// isn't text is summarized rather than dropped silently.
func (c *Client) CallTool(ctx context.Context, name string, arguments any) (string, bool, error) {
	res, err := c.session.CallTool(ctx, &sdk.CallToolParams{
		Name:      name,
		Arguments: arguments,
	})
	if err != nil {
		return "", true, err
	}
	return joinContent(res.Content), res.IsError, nil
}

// Close shuts down the session. For stdio transports this terminates the
// subprocess; for HTTP it tears down the persistent connection. Safe to call
// multiple times.
func (c *Client) Close() error {
	if c.session == nil {
		return nil
	}
	return c.session.Close()
}

// joinContent flattens MCP content blocks into one string. We only render
// text natively; non-text blocks get a placeholder so the model knows
// something was there.
func joinContent(blocks []sdk.Content) string {
	var out string
	for i, b := range blocks {
		if i > 0 {
			out += "\n"
		}
		switch v := b.(type) {
		case *sdk.TextContent:
			out += v.Text
		default:
			out += fmt.Sprintf("[non-text content block: %T]", v)
		}
	}
	return out
}

// headerRoundTripper injects a fixed set of headers into every request.
// Used for auth tokens on HTTP MCP transports.
type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.base.RoundTrip(req)
}
