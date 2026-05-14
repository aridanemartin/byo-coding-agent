package provider

import (
	"encoding/json"
	"time"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

// marshalRequestPayload renders the about-to-be-sent request as pretty JSON
// for the /debug show panel. We include the resolved tool list (names +
// descriptions, schemas elided) so the user can confirm what the model saw,
// without ballooning the payload when the same big schema repeats on every
// turn.
func marshalRequestPayload(model, system string, maxTokens int64, messages []api.Message, tools []api.ToolDef) string {
	type toolBrief struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	briefs := make([]toolBrief, len(tools))
	for i, t := range tools {
		briefs[i] = toolBrief{Name: t.Name, Description: t.Description}
	}
	payload := map[string]any{
		"model":      model,
		"max_tokens": maxTokens,
		"system":     system,
		"tools":      briefs,
		"messages":   messages,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "marshal error: " + err.Error()
	}
	return string(b)
}

// marshalResponsePayload renders the full response shape for /debug show.
// Includes elapsed wall time so the user can tell at a glance which calls
// were expensive.
func marshalResponsePayload(resp api.Response, elapsed time.Duration) string {
	payload := map[string]any{
		"elapsed":     elapsed.String(),
		"stop_reason": resp.StopReason,
		"usage":       resp.Usage,
		"content":     resp.Content,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "marshal error: " + err.Error()
	}
	return string(b)
}
