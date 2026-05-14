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

El harness ya incluye uno: [`internal/provider/mock.go`](../../internal/provider/mock.go). Implementa `Provider` con un slice de respuestas predefinidas, semántica opcional `RepeatLast` para tests del tipo "loop infinito", un campo `Err` para ejercitar caminos de error, y captura de llamadas para que los tests puedan hacer aserciones sobre los mensajes y herramientas que el agente fue construyendo:

```go
p := provider.NewMockProvider(
    api.Response{
        StopReason: api.StopToolUse,
        Content: []api.Block{{Type: api.BlockToolUse, ToolUseID: "t1", ToolName: "echo"}},
    },
    api.Response{
        StopReason: api.StopEndTurn,
        Content:    []api.Block{{Type: api.BlockText, Text: "done"}},
    },
)
a := agent.New(p, "", reg)
got, _ := a.Send(ctx, "hi")
p.LastSent() // mensajes pasados al Send más reciente
p.Calls()    // cuántas idas y vueltas se ejecutaron
```

[`internal/agent/agent_test.go`](../../internal/agent/agent_test.go) lo usa para probar el bucle del agente end-to-end sin consumir tokens de la API: el camino feliz solo-texto, una ida y vuelta con tool-use, el clamp de MaxTurns, y la propagación de errores del proveedor. Copia ese archivo como punto de partida cuando quieras probar tu propio proveedor o estrategia.

## Rastreo de tokens (opcional)

Si tu proveedor puede reportar el uso de tokens, añade un método `TotalUsage()` fuera de la interfaz que devuelva los conteos acumulados de la sesión. El comando `/tokens` hace type-assertion sobre este método (mira [`follow_along/es/16-token-viewer.md`](../../follow_along/es/16-token-viewer.md)) — impleméntalo y el visor de tokens funciona sin más.

## Ejemplo resuelto: el proveedor de OpenAI

[`internal/provider/openai.go`](../../internal/provider/openai.go) es un segundo proveedor completo, que implementa la misma interfaz `Provider` contra la API Chat Completions de OpenAI. Léelo en paralelo con `anthropic.go` para ver los patrones de traducción en la práctica — el mismo slice `api.Message` se despliega hacia formas muy distintas en cada SDK:

| Concepto | Forma en Anthropic | Forma en OpenAI |
|---|---|---|
| System prompt | Campo de nivel superior en la solicitud | Primer mensaje con `role:"system"` en el array |
| Tool result | Bloque dentro de un mensaje de rol user | Mensaje separado con `role:"tool"` |
| Definición de herramienta | `properties` + `required` como dos campos | Un objeto JSON Schema que envuelve ambos |
| Stop reason | `end_turn` / `tool_use` | `stop` / `tool_calls` |
| Reporte de caché | `cache_creation_input_tokens` + `cache_read_input_tokens` | Solo `prompt_tokens_details.cached_tokens` |

La interfaz absorbe todo eso; el bucle del agente y las herramientas no saben cuál se está usando. Cambia en tiempo de ejecución con `LLM_PROVIDER=openai` (y opcionalmente `LLM_MODEL=gpt-5-codex`).

## Ver también

- [`follow_along/es/03-the-provider-interface.md`](../../follow_along/es/03-the-provider-interface.md) para entender por qué la interfaz tiene la forma que tiene.
- [`internal/provider/anthropic.go`](../../internal/provider/anthropic.go) y [`internal/provider/openai.go`](../../internal/provider/openai.go) para las dos implementaciones de referencia.
