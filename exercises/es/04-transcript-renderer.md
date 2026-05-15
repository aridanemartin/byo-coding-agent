# Ejercicio 4 — Renderizador de transcripción intercambiable

**Objetivo:** convertir la función `RenderTranscript` actual en una interfaz `Renderer` con múltiples implementaciones, y añadir un comando `/export`.

**Dificultad:** media. **Tiempo:** 1–2 horas. **Toca:** `internal/api/`, `commands.go`.

## Lo que ya está en el repo

`internal/api/types.go` tiene una función `RenderTranscript([]Message) string` que se usa desde la estrategia de compactación por resumen y desde el decorador del log de compactación. Es un único formato fijo — bien para esos usos internos, pero inútil si quieres exportar una sesión terminada como algo que un humano o otra herramienta pueda leer.

El harness tiene otros cuatro puntos de extensión que siguen exactamente la misma forma: interfaz pequeña + implementación Default + sitio para que otros enchufen las suyas. La transcripción es el que falta de forma obvia.

## Lo que vas a construir

Una interfaz `Renderer`, dos o tres implementaciones, y un slash command:

```go
type Renderer interface {
    Render(io.Writer, []Message) error
}
```

- `TerminalRenderer{}` — el formato de texto actual, factorizado fuera de `RenderTranscript`.
- `MarkdownRenderer{}` — H2 por turno (`## user`, `## assistant`), bloques con fences para input/output de herramientas, código inline para nombres de herramienta.
- `JSONRenderer{}` — round-trippable; los tests pueden cargarlo de vuelta.
- (Opcional) `HTMLRenderer{}` — comparte Goldmark con `cmd/sitegen`.

## Pasos sugeridos

1. **Promueve `RenderTranscript` a método.** Mueve la lógica existente a `TerminalRenderer.Render` en un nuevo `internal/api/render.go`. Mantén `RenderTranscript` como wrapper fino para no romper a los callers:

   ```go
   func RenderTranscript(msgs []Message) string {
       var b strings.Builder
       _ = (TerminalRenderer{}).Render(&b, msgs)
       return b.String()
   }
   ```

2. **Implementa markdown y JSON.** Ambas son funciones puras sobre `[]Message`. La parte más difícil es decidir cómo formatear los bloques `tool_use` y `tool_result` — elige algo legible, no necesariamente fiel al formato del wire.

3. **Añade el slash command.** Siguiendo el patrón de `commands.go`, registra `/export <format> [path]`:

   - `/export markdown` → imprime a stdout
   - `/export markdown ./session.md` → escribe al archivo
   - El formato por defecto es markdown si lo omites

4. **Testea el round-trip de JSON.** Un test pequeño que serialice una transcripción conocida y la deserialice de vuelta a una `[]Message` equivalente.

## Aceptación

- `/export markdown ./out.md` produce un archivo markdown legible con una sección por turno y bloques de herramienta con code fences.
- `/export json ./out.json` round-trippea: un loader lee el JSON de vuelta a `[]Message` y el resultado equivale al original.
- `compact.Summarize` y `compact.WithLogging` (que ambos llaman a `RenderTranscript`) siguen funcionando — el wrapper preservó su comportamiento.
- Los tests existentes pasan.

## Extra

- Un comando `/import <path>` que lee un export JSON de vuelta a la sesión activa.
- Un renderizador en streaming que escribe incrementalmente durante el bucle del agente (útil para sesiones largas donde quieras output progresivo).
- Un renderizador HTML que produzca un archivo standalone con el theme gruvbox de código que casa con la web.
