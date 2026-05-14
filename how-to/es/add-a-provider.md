# Cómo añadir un proveedor nuevo

Objetivo: ejecutar el harness contra un backend de LLM distinto — OpenAI, Bedrock, un Ollama local o un mock para pruebas.

La interfaz `Provider` ([`internal/provider/provider.go`](../../internal/provider/provider.go)) son tres métodos. Implementarlos en un archivo nuevo es el cambio completo en la capa de abstracción; el bucle del agente, las herramientas, la compactación y la TUI no saben con qué backend están hablando.

## Pasos

### 1. Crea `internal/provider/your_provider.go`

```go
package provider

import (
	"context"

	"github.com/betta-tech/byo-coding-agent/internal/api"
)

type YourProvider struct {
	client    *yourSDK.Client
	model     string
	system    string
	maxTokens int
}

func NewYourProvider(model, system string, maxTokens int) *YourProvider {
	return &YourProvider{
		client:    yourSDK.NewClient(),
		model:     model,
		system:    system,
		maxTokens: maxTokens,
	}
}

func (p *YourProvider) Model() string        { return p.model }
func (p *YourProvider) SetModel(name string) { p.model = name }

func (p *YourProvider) Send(ctx context.Context, messages []api.Message, tools []api.ToolDef) (api.Response, error) {
	req := p.toRequest(messages, tools)        // ↓ adaptador
	sdkResp, err := p.client.Chat(ctx, req)
	if err != nil {
		return api.Response{}, err
	}
	return p.fromResponse(sdkResp), nil        // ↓ adaptador
}
```

Los dos métodos privados (`toRequest`, `fromResponse`) son los únicos lugares donde se permite que aparezcan los tipos del SDK. Si un `yourSDK.Foo` aparece en cualquier otro sitio de la base de código, la abstracción se ha filtrado.

### 2. Implementa los dos métodos de traducción

`toRequest` mapea `[]api.Message` → la forma nativa de mensajes del SDK, y `[]api.ToolDef` → la forma de herramientas del SDK. Mira los `toMessages` y `toTools` de [`internal/provider/anthropic.go`](../../internal/provider/anthropic.go) como patrón de referencia.

Para OpenAI específicamente:

| tipo api | mapeo OpenAI |
|---|---|
| `Message{Role: User}` | `{role: "user", content: ...}` |
| `Message{Role: Assistant}` con `BlockToolUse` | `{role: "assistant", tool_calls: [...]}` |
| `BlockToolResult` | Un mensaje separado: `{role: "tool", tool_call_id: ..., content: ...}` |
| `system` (campo de nivel superior) | Primer mensaje: `{role: "system", content: ...}` |

La forma de `tool_result` es la mayor divergencia de OpenAI: los resultados son sus propios mensajes con `role: "tool"`, no bloques dentro de un mensaje de usuario. Tu `toRequest` tiene que aplanar un `api.Message` que contiene varios bloques `BlockToolResult` en varios mensajes de OpenAI.

`fromResponse` hace lo inverso: content / tool_calls / finish_reason del SDK → `api.Response{Content, StopReason, Usage}`.

### 3. Conéctalo en `main.go`

Cambia una línea:

```go
// Antes
llm := provider.NewAnthropicProvider(anthropic.ModelClaudeOpus4_7, 8192, sysPrompt)

// Después
llm := provider.NewYourProvider("gpt-4o", sysPrompt, 8192)
```

Compila, ejecuta. El resto del harness no cambia.

## Convenciones

- **Solo el archivo nuevo importa el SDK.** Esta es la prueba de si la abstracción es real. Si encuentras tipos del SDK filtrados a `internal/agent/`, `internal/tool/` o `main.go`, arréglalo ahora — será mucho más difícil después.
- **`Send` debe rellenar `Usage`** si quieres que `/tokens` funcione. Mapea los conteos de tokens por llamada del proveedor a `api.Usage`. Los campos de caché pueden ser cero si el proveedor no los expone.
- **`StopReason` es un enum de tres valores** — `StopEndTurn` (el modelo terminó), `StopToolUse` (el modelo quiere herramientas), `StopOther` (todo lo demás: max_tokens, refusal, etc.). El bucle del agente solo bifurca con `StopToolUse`.
- **Ordena las definiciones de herramientas antes de enviarlas.** La iteración de mapas en Go es aleatoria; solicitudes consecutivas con las "mismas" herramientas pueden serializarse a bytes distintos, lo que rompe el prompt caching. El adaptador de Anthropic maneja esto en la capa del registro — copia el patrón.

## Proveedor mock para pruebas

Un stub mínimo que devuelve respuestas predefinidas, útil para probar el bucle del agente sin una clave de API:

```go
type MockProvider struct {
	Responses []api.Response
	calls     int
}

func (p *MockProvider) Send(ctx context.Context, _ []api.Message, _ []api.ToolDef) (api.Response, error) {
	if p.calls >= len(p.Responses) {
		return api.Response{StopReason: api.StopEndTurn}, nil
	}
	r := p.Responses[p.calls]
	p.calls++
	return r, nil
}

func (p *MockProvider) Model() string        { return "mock" }
func (p *MockProvider) SetModel(name string) {}
```

Inyecta respuestas de texto predefinidas, bloques `tool_use` predefinidos, etc. — útil para pruebas unitarias de la compactación, el bucle del agente, los comandos de barra.

## Rastreo de tokens (opcional)

Si tu proveedor puede reportar el uso de tokens, añade un método `TotalUsage()` fuera de la interfaz que devuelva los conteos acumulados de la sesión. El comando `/tokens` hace type-assertion sobre este método (mira [`follow_along/es/16-token-viewer.md`](../../follow_along/es/16-token-viewer.md)) — impleméntalo y el visor de tokens funciona sin más.

## Ver también

- [`follow_along/es/03-the-provider-interface.md`](../../follow_along/es/03-the-provider-interface.md) para entender por qué la interfaz tiene la forma que tiene.
- [`internal/provider/anthropic.go`](../../internal/provider/anthropic.go) para la implementación de referencia.
