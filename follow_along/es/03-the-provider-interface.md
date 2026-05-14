# 03 · La interfaz de proveedor

Hasta ahora el bucle del agente llama a `client.Messages.New(...)` directamente. Los tipos del SDK de Anthropic están salpicados por todos los archivos — `anthropic.MessageParam`, `anthropic.ToolUnionParam`, `anthropic.StopReason`. El harness está casado con un solo backend.

Este es el primer momento de "la abstracción se gana su lugar". Vamos a hacer que el backend del LLM sea intercambiable.

## Lo que queremos

```go
// Cambia esta línea para cambiar de proveedor — ese es el punto entero.
var llm Provider = NewAnthropicProvider(...)
```

Agregar OpenAI, Bedrock, un Ollama local o un mock debería ser un archivo nuevo más una línea en `main.go`. Cualquier cosa más allá de eso y diseñamos mal la abstracción.

## Diseñando la interfaz

La superficie mínima que necesita el bucle del agente:

```go
type Provider interface {
    Send(ctx context.Context, messages []Message, tools []ToolDef) (Response, error)
    Model() string
    SetModel(name string)
}
```

`Send` hace el viaje de ida y vuelta. `Model` / `SetModel` existen por el comando `/model` (capítulo 05) — es una pequeña concesión pero generaliza (todo proveedor de LLM tiene noción de modelo).

`Message`, `ToolDef`, `Response` tienen que ser **tipos agnósticos al proveedor que definamos nosotros mismos**. Esta es la decisión load-bearing: no podemos exponer `anthropic.MessageParam` en la interfaz — eso ataría a los llamadores a la forma de Anthropic y derrotaría la abstracción.

Así que acuñamos los nuestros:

```go
type Role string
const (
    RoleUser      Role = "user"
    RoleAssistant Role = "assistant"
)

type BlockType string
const (
    BlockText       BlockType = "text"
    BlockToolUse    BlockType = "tool_use"
    BlockToolResult BlockType = "tool_result"
)

type Block struct {
    Type       BlockType
    Text       string  // BlockText
    ToolUseID  string  // BlockToolUse, BlockToolResult
    ToolName   string  // BlockToolUse
    ToolInput  string  // BlockToolUse — raw JSON
    ToolResult string  // BlockToolResult
    IsError    bool    // BlockToolResult
}

type Message struct {
    Role    Role
    Content []Block
}

type ToolDef struct {
    Name        string
    Description string
    InputSchema map[string]any
    Required    []string
}

type Response struct {
    Content    []Block
    StopReason StopReason
}
```

Estos tipos son a propósito la **intersección** de lo que cualquier API mayor de LLM necesitaría. Los tipos de bloque mapean limpiamente a la forma nativa de Anthropic. Para OpenAI, tool_use → `tool_calls` y tool_result → un mensaje separado con `role: "tool"`. La traducción vive en el adapter.

## El adapter de Anthropic

Un struct, un archivo más o menos grande. El trabajo interesante está en dos métodos privados, `toMessages` y `toTools`, que traducen nuestros tipos genéricos a la forma del SDK:

```go
type AnthropicProvider struct {
    client    anthropic.Client
    model     anthropic.Model
    maxTokens int64
    system    string
}

func (p *AnthropicProvider) Send(ctx context.Context, messages []Message, tools []ToolDef) (Response, error) {
    resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     p.model,
        MaxTokens: p.maxTokens,
        System:    []anthropic.TextBlockParam{{Text: p.system}},
        Messages:  p.toMessages(messages),
        Tools:     p.toTools(tools),
        Thinking:  anthropic.ThinkingConfigParamUnion{
            OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{},
        },
    })
    if err != nil { return Response{}, err }

    out := Response{StopReason: fromStopReason(resp.StopReason)}
    for _, block := range resp.Content {
        switch v := block.AsAny().(type) {
        case anthropic.TextBlock:
            out.Content = append(out.Content, Block{Type: BlockText, Text: v.Text})
        case anthropic.ToolUseBlock:
            out.Content = append(out.Content, Block{
                Type:      BlockToolUse,
                ToolUseID: v.ID,
                ToolName:  v.Name,
                ToolInput: v.JSON.Input.Raw(),
            })
        }
    }
    return out, nil
}
```

El archivo entero son ~120 líneas. **Es el único lugar en el harness que importa `anthropic-sdk-go`.** Esa es la prueba de si la abstracción es real: si los tipos del SDK se filtran a otros lados, no abstrajiste nada.

> **En el repo actual.** La interfaz vive en [`internal/provider/provider.go`](../internal/provider/provider.go) (15 líneas, sin imports más allá de `context` y nuestro propio `internal/api`). El adapter de Anthropic es [`internal/provider/anthropic.go`](../internal/provider/anthropic.go). Los tipos genéricos compartidos — `Message`, `Block`, `ToolDef`, `Response` — viven en [`internal/api/types.go`](../internal/api/types.go). Tres archivos; cada uno chico; cada uno con una sola responsabilidad.

## Qué te gana esto

Tres victorias concretas, en orden de obviedad:

1. **Puedes cambiar de proveedor.** Escribes `internal/provider/openai.go` con un `OpenAIProvider` que implementa `Provider`. Cambias una línea en `main.go`. El bucle del agente no cambia.

2. **Puedes testear el bucle del agente sin una API key.** Un `MockProvider` cuyo `Send` devuelve respuestas predefinidas te deja hacer unit-tests del bucle, la compactación, el dispatch de herramientas — todo menos el modelo en sí.

3. **Puedes correr dos modelos en una sola sesión.** Los subagentes (capítulo 11) usan el mismo proveedor que el root, pero en principio podrían usar uno más barato. A la interfaz le da igual.

## Qué cuesta

Overhead de traducción — cada llamada `Send` recorre los mensajes y traduce bloques en ambas direcciones. Para una conversación de 100 mensajes no es gratis, pero es despreciable al lado de la latencia de red. No optimices esto hasta que el profile lo diga.

Algo de pérdida de features específicos del proveedor. El thinking adaptativo vive en los params de Anthropic, no en nuestra forma genérica. Los campos específicos de Anthropic (`thinking.display`, `output_config.effort`) se configuran al construir el adapter, no se exponen a través de la interfaz. Ese es el tradeoff correcto: las perillas específicas del proveedor se quedan en el paquete del proveedor; el bucle del agente nunca las ve.

## Un alcance inesperado: modelos locales

El primer backend no-Anthropic que la mayoría de la gente quiere es uno local — Ollama, LM Studio, el server de llama.cpp, vLLM. Vale la pena saberlo de entrada: **casi nunca hace falta un tercer adapter.** Cada servidor moderno de modelos locales expone una API *compatible con OpenAI* en una URL configurable. Una vez que tienes un `OpenAIProvider`, apuntarlo a `http://localhost:11434/v1/` es un cambio de una opción al construirlo, no un paquete nuevo.

El SDK de Go de OpenAI acepta una sobreescritura de `BaseURL`. El patrón se ve así:

```go
// internal/provider/openai.go
func NewOpenAIProvider(model, system string, maxTokens int64, baseURL string) *OpenAIProvider {
    opts := []option.RequestOption{}
    if baseURL != "" {
        opts = append(opts, option.WithBaseURL(baseURL))
        // Los servidores locales no validan la API key, pero el SDK se
        // niega a construirse sin una. Un placeholder es suficiente.
        if os.Getenv("OPENAI_API_KEY") == "" {
            opts = append(opts, option.WithAPIKey("local"))
        }
    }
    return &OpenAIProvider{client: openai.NewClient(opts...), /* … */}
}
```

Eso es todo. El bucle del agente, las herramientas, la compactación, MCP, debug — todo — ve la misma interfaz `Provider` y no sabe si las respuestas vienen de una API que cuesta $20K en tokens al mes o del GPU de tu portátil. **Para esto era la abstracción.**

Lo que sí cambia es operativo, no arquitectónico:

| Aspecto | Proveedor cloud | Servidor local |
|---|---|---|
| Tool calling | Funciona en todo modelo moderno | Funciona en Llama 3.1+, Qwen 2.5+, Mistral con plantilla de tools. Los modelos chicos (Phi-3, Llama 3.2 3B) lo simulan mal — apaga la delegación o elige un modelo más grande. |
| Latencia | 200–800 ms por turno | 50 ms en un GPU de la serie M, varios segundos en CPU |
| Ventana de contexto | 200K+ | 8K–128K dependiendo del modelo |
| Costo | Por token | Cero monetario; el `EstimatedCostUSD()` del harness o devuelve 0 (añade una entrada de pricing con ceros) o -1 (tratar como "desconocido") |
| Estrictez del schema de tools | Endurecido | Los servidores locales suelen parsear schemas con más laxitud; los bloques tool_use pueden venir con campos extra que el SDK ignora |

Cuándo *sí* escribirías un proveedor local dedicado en vez de reusar el de OpenAI:

- Quieres el endpoint nativo `/api/chat` de Ollama (menos overhead, menos serialización).
- Necesitas scriptear el servidor — hacer `pull` de un modelo desde dentro del harness, listar modelos disponibles, manejar memoria.
- Quieres un formato no-chat (raw completion, FIM, etc.) que la capa de compatibilidad con OpenAI no expone.

Para cualquiera de esos casos, copias `openai.go`, cambias el SDK y la URL, y ajustas `toMessages`/`fromResponse` al schema nativo. ~150 líneas. Pero solo deberías hacerlo cuando la capa de compatibilidad se vuelva un cuello de botella real — `WithBaseURL` es el punto de partida correcto para el 90% de los casos de uso de modelos locales.

La lección más amplia: cuando una abstracción se diseña alrededor de la *forma* de una interacción en vez de alrededor de un vendor concreto, absorbe backends nuevos casi gratis. La interfaz Provider definida en este capítulo no se diseñó pensando en modelos locales — y aun así los maneja, porque Ollama y compañía adoptaron la misma forma.

## Tropiezos

**Orden de iteración de maps.** Al convertir herramientas, el orden de los campos en `InputSchema` está determinado por la iteración de map, que en Go es aleatoria. Dos peticiones con las "mismas" herramientas pueden serializarse a bytes distintos, rompiendo el prompt caching (capítulo 06). El fix es ordenar los nombres de las herramientas antes de emitirlos — vamos a volver a esto cuando construyamos el registry en el capítulo 09.

**La colisión del nombre de variable.** Cuando el paquete es `provider`, no puedes tener también una variable llamada `provider` en `main`. La renombramos a `llm`:

```go
var llm provider.Provider
```

Se lee natural: `llm.Send(...)`, `llm.SetModel("claude-haiku-4-5")`.

## Ahora prueba

1. Esboza — no tienes que implementarlo de verdad — un `OpenAIProvider`. ¿Cómo se vería su `toMessages`? ¿Dónde va el system prompt en la API de OpenAI vs la de Anthropic?
2. Lee `anthropic.go` e identifica cada lugar donde los tipos del SDK tocan nuestros tipos genéricos. Esas son tus costuras de traducción. Deberían ser exactamente dos: `Send` (response → genérico) y `toMessages` / `toTools` (genérico → SDK).
3. Escribe un `MockProvider` que devuelva una respuesta fija. Úsalo para testear que `agentLoop` corre sin entrar en pánico con un slice de `messages` vacío.

Siguiente: [04 · Pulido de la UI](04-ui-polish.md).
