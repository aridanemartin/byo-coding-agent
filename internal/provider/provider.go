// Package provider defines the LLM-backend interface and houses each
// concrete implementation. The harness only talks to providers through
// the Provider interface — swap implementations to swap models or SDKs.
package provider

import (
	"context"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

type Provider interface {
	Send(ctx context.Context, messages []api.Message, tools []api.ToolDef) (api.Response, error)
	Model() string
	SetModel(name string)
}
