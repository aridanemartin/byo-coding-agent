# Ejercicio 5 — Respuestas en streaming

**Objetivo:** renderizar la respuesta del asistente token a token según va llegando del modelo, en lugar de esperar a la respuesta completa.

**Dificultad:** difícil. **Tiempo:** 3–5 horas. **Toca:** `internal/provider/`, `internal/agent/`, `internal/ui/`.

## Lo que ya está en el repo

La interfaz `Provider` en `internal/provider/provider.go` son tres métodos:

```go
type Provider interface {
    Send(ctx context.Context, messages []api.Message, tools []api.ToolDef) (api.Response, error)
    Model() string
    SetModel(name string)
}
```

`Send` es síncrono — devuelve la respuesta completa solo cuando el modelo termina. La UI de Bubble Tea en `internal/ui/program.go` refleja eso: el mensaje del asistente aparece de golpe, no carácter a carácter.

El SDK de Anthropic soporta streaming vía `client.Messages.NewStreaming`. El de OpenAI también. El repo simplemente no usa todavía ninguno de esos puntos de entrada.

Este es el ejercicio más invasivo del set — toca la interfaz del provider, el bucle del agente y la TUI. Y también expone una tensión de diseño real: las llamadas a herramientas *no* pueden hacerse en streaming (hay que parsear el bloque entero antes de despachar), así que una ruta de streaming necesita tratar los deltas de texto y los bloques de herramienta de forma distinta.

## Lo que vas a construir

Una variante en streaming de `Send` y una UI que la consuma.

## Pasos sugeridos

1. **Diseña el tipo de chunk.** Algo así:

   ```go
   type Chunk struct {
       Kind  ChunkKind // TextDelta, ToolStart, ToolDelta, ToolEnd, Stop, Usage
       Text  string    // para TextDelta
       Block api.Block // para ToolEnd
       Usage *api.Usage
   }
   ```

2. **Extiende la interfaz Provider — con cuidado.** Dos opciones:

   a. **Añade un método:** `SendStream(ctx, msgs, tools) (<-chan Chunk, error)`. Pros: separación limpia; el `Send` viejo sigue funcionando. Contras: cada implementación tiene ahora dos métodos haciendo casi lo mismo.

   b. **Hazlo opcional vía interfaz hermana:**

      ```go
      type Streamer interface {
          SendStream(ctx, msgs, tools) (<-chan Chunk, error)
      }
      ```

      Type-assert en el bucle del agente. Pros: providers que no pueden hacer streaming no tienen que. Contras: dos rutas de código.

   Elige (b). Es el mismo patrón que `TotalUsage()` y `EstimatedCostUSD()` en Provider (mira cómo `/tokens` hace una assertion en `commands.go`).

3. **Implementa para Anthropic.** Usa `client.Messages.NewStreaming(ctx, params)` en `internal/provider/anthropic.go`. Traduce los eventos del SDK a tu tipo `Chunk`. Bufferea los eventos `content_block_delta` para los bloques tool_use hasta que llegue el `content_block_stop` correspondiente, y entonces emite un único chunk `ToolEnd` con el `api.Block` ensamblado.

4. **Conéctalo en el bucle del agente.** En `internal/agent/agent.go`, cuando el provider implemente `Streamer`, vacía el canal de chunks y:
   - Para `TextDelta`, reenvía el texto a un callback de UI (introduce `Agent.OnTextDelta func(string)`).
   - Para `ToolEnd`, acumula bloques en `assistantBlocks`.
   - Para `Stop`, finaliza el mensaje y despacha herramientas como hoy.

5. **Conéctalo en la UI.** El programa de Bubble Tea ahora añade el mensaje del asistente entero en un único `tea.Msg`. Añade un nuevo tipo de mensaje `streamDeltaMsg{text string}` que se despache cuando lleguen los deltas, y lo concatenes al buffer visible.

6. **Maneja la cancelación.** Ctrl-C durante un stream debe cerrar el contexto, vaciar el canal y no causar deadlock. Testéalo explícitamente.

## Aceptación

- El texto aparece carácter a carácter (o chunk a chunk) en la TUI.
- Las llamadas a herramientas siguen despachándose correctamente cuando el stream acaba.
- Ctrl-C aborta limpiamente un stream en vuelo y devuelve el control al REPL.
- `/tokens` sigue reportando cuentas correctas después de un mensaje en streaming.
- El provider OpenAI (que no has extendido) sigue funcionando por la ruta no-streaming.

## Extra

- Implementa `Streamer` también para OpenAI (`client.Chat.Completions.NewStreaming`).
- Añade una flag `--no-stream` que fuerce la ruta no-streaming aunque esté disponible, para debugging.
- Mide el time-to-first-token en el panel `/debug`.
- Streamea también la salida de los subagentes — ahora corren silenciosos con `Quiet: true`.
