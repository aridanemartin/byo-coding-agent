# Cómo añadir una herramienta nueva

Objetivo: darle al modelo una capacidad nueva — por ejemplo, obtener una URL o ejecutar `git diff`.

El registro de herramientas usa auto-registro vía `init()`, así que añadir una herramienta significa soltar un archivo en `internal/tool/`. Sin ediciones a `main.go`.

## Pasos

### 1. Crea `internal/tool/your_tool.go`

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

### 2. Ejecuta el harness

```sh
go run .
```

Escribe `/tools` — `web_fetch` está en la lista. Pídele al agente algo como "obtén https://example.com y resúmelo".

Eso es todo.

## Convenciones

- **Devuelve los errores como tool results.** `(message, true)` significa "esto falló, esta es la razón". No devuelvas errores de Go desde `Execute` — el bucle del agente no tiene forma de recuperarse de esos.
- **Usa el contexto.** El trabajo de larga duración (HTTP, shell, recorridos de archivos) debería aceptar `ctx` y pasarlo para que la cancelación con Ctrl-C se propague.
- **Limita tu salida.** Un cuerpo de respuesta de 10 MB va a hacer estallar la ventana de contexto del modelo. Usa `io.LimitReader`, trunca cadenas o pagina.
- **Sé preciso en `Description`.** El modelo elige herramientas leyendo sus descripciones. Las descripciones vagas ("hace cosas con URLs") causan mal uso; las específicas ("Fetch a URL and return the response body…") no.
- **JSON Schema es lo que tienes.** `type`, `description`, `enum`, `items`, `properties` son todos compatibles. Required va en el slice `Required` separado, no en el mapa del esquema.

## Cuando `init()` no funciona

Las herramientas que necesitan configuración en tiempo de ejecución (claves de API, un `*Provider` compartido, una struct de configuración) no pueden auto-registrarse porque esos valores no existen al cargar el paquete. Dos opciones:

**A. Construir en main y registrar explícitamente:**

```go
// main.go
tool.Default.Register(&YourTool{APIKey: os.Getenv("YOUR_KEY")})
```

**B. Usar una factoría que vive en main:**

Mira [`delegate.go`](../../delegate.go) para el ejemplo canónico — `DelegateTool` necesita un `Subagent`, que solo existe en tiempo de ejecución, así que se construye y se registra en `main.go`.

## Limitar qué subagentes ven la herramienta

Por defecto cada herramienta es visible para cada agente que use `tool.Default`. Para restringir, usa un `Subset`:

```go
subagent.Default.Register(subagent.Research{
	Provider: llm,
	Tools:    tool.Default.Subset("read_file", "web_fetch"),
})
```

El subagente de research ahora solo ve esas dos; el agente root sigue viendo todas.

## Ver también

- [`follow_along/es/09-plug-and-play-tools.md`](../../follow_along/es/09-plug-and-play-tools.md) para la justificación del diseño.
- [`follow_along/es/14-mcp-support.md`](../../follow_along/es/14-mcp-support.md) para ver cómo MCP envuelve herramientas remotas detrás de la misma interfaz.
