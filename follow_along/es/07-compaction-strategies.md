# 07 · Estrategias de compactación

Las conversaciones crecen. Cada turno re-manda la historia entera. Para el turno 30 estás pagando plata real para re-tokenizar horas de chat pasado. Para el turno 100 empiezas a chocar con la ventana de contexto.

Este capítulo trata de manejar eso. Como los providers, la compactación es algo que vas a querer cambiar y experimentar, así que recibe el mismo tratamiento: una interfaz, múltiples implementaciones, una línea en `main.go` para cambiar.

## La interfaz

```go
type CompactionStrategy interface {
    Compact(ctx context.Context, messages []api.Message) ([]api.Message, error)
}
```

Las estrategias se llaman al inicio de cada turno del bucle del agente. La mayor parte del tiempo devuelven la entrada sin cambios (no se alcanzó el umbral). Cuando sí actúan, devuelven un slice acortado.

## Tres estrategias

| Estrategia | Qué hace | Cuándo usarla |
|---|---|---|
| `NoCompaction` | Devuelve la entrada sin cambios | Default; controlas el presupuesto en otro lado |
| `SlidingWindow{KeepLast: N}` | Tira todo menos los últimos N mensajes | Barata, sin llamada a la API, pierde contexto viejo |
| `Summarize{Threshold, KeepRecent, …}` | Le pide al modelo que resuma los turnos viejos, los reemplaza con el resumen | Preserva el contexto anterior como un mensaje sintético; cuesta una llamada extra a la API cuando se dispara |

Más un **decorator**:

| Wrapper | Qué hace |
|---|---|
| `WithLogging(inner, path)` | Envuelve cualquier estrategia; loguea diffs antes/después a un archivo. Útil para comparar estrategias. |

```go
// En main.go — cambia la línea para cambiar el comportamiento
compactor = compact.NoCompaction{}
compactor = &compact.SlidingWindow{KeepLast: 10}
compactor = &compact.Summarize{Provider: llm, Threshold: 20, KeepRecent: 6}
compactor = compact.WithLogging(&compact.SlidingWindow{KeepLast: 10}, "compactions.log")
```

## El truco del "safe split"

Esta es la única pieza de compactación que es genuinamente sutil.

Un truncado ingenuo puede dejar un bloque `tool_use` en la porción descartada y su `tool_result` correspondiente en la porción que se mantiene. La siguiente llamada a la API ve un `tool_result` sin un `tool_use` que lo precede — y devuelve 400. La conversación es irrecuperable sin cirugía manual.

El fix: caminar hacia atrás desde tu punto de split deseado hasta encontrar un límite "limpio" — un mensaje del usuario que contenga texto, no una respuesta tool_result. Eso marca el inicio de un turno nuevo, sobre el cual siempre es seguro hacer split.

```go
// SafeSplitPoint camina hacia atrás desde `desired` para encontrar un índice donde
// la conversación está en un estado "limpio" — sin tool_use sin su tool_result
// a ningún lado del split.
func SafeSplitPoint(messages []api.Message, desired int) int {
    if desired <= 0 { return 0 }
    if desired >= len(messages) { return len(messages) }
    for i := desired; i > 0; i-- {
        if messages[i].Role == api.RoleUser && !messages[i].HasToolResult() {
            return i
        }
    }
    return 0
}
```

Toda estrategia pasa por acá. Si no podemos encontrar un límite seguro cerca de donde queríamos, no hacemos nada — mejor que una conversación rota.

## Summarization

La estrategia interesante. Cuando la conversación alcanza `Threshold`:

1. Encontramos un safe split point cerca de `len(messages) - KeepRecent`.
2. Tomamos la mitad vieja (todo lo anterior al split).
3. Le pedimos al modelo que la resuma. Usamos el *mismo* provider — recursivo pero acotado; la llamada de summarization es de un solo tiro, sin herramientas, devuelve una sola respuesta de texto.
4. Reemplazamos la mitad vieja con un mensaje de usuario sintético: `"[earlier conversation summary] ..."`.
5. Mantenemos la mitad reciente tal cual.

```go
func (s *Summarize) Compact(ctx context.Context, messages []api.Message) ([]api.Message, error) {
    if len(messages) < s.Threshold { return messages, nil }
    split := SafeSplitPoint(messages, len(messages)-s.KeepRecent)
    if split == 0 { return messages, nil }
    old, recent := messages[:split], messages[split:]

    resp, err := s.Provider.Send(ctx, []api.Message{{
        Role:    api.RoleUser,
        Content: []api.Block{{Type: api.BlockText, Text: instructions + "\n\n" + api.RenderTranscript(old)}},
    }}, nil)
    if err != nil { return messages, fmt.Errorf("summarize: %w", err) }

    // … extraer el texto del resumen, anteponerlo como mensaje sintético …
    return append([]api.Message{{Role: api.RoleUser, ...}}, recent...), nil
}
```

La summarization del modelo está sesgada por la forma de su system prompt — cuando resume en el contexto de "eres un asistente de código", tiende a preservar paths de archivos, nombres de funciones y decisiones, y a tirar el chit-chat. Eso es un feliz accidente. Un diseño más riguroso te dejaría sobreescribir el prompt de summarization explícitamente.

## El patrón decorator

La pieza más limpia de este diseño es `LoggingStrategy`. Implementa `CompactionStrategy` *y* envuelve una:

```go
type LoggingStrategy struct {
    Inner    CompactionStrategy
    FilePath string
}

func (l *LoggingStrategy) Compact(ctx context.Context, messages []api.Message) ([]api.Message, error) {
    before := messages
    after, err := l.Inner.Compact(ctx, messages)
    if err != nil { return after, err }
    if len(after) != len(before) {
        l.writeEvent(before, after)
    }
    return after, nil
}
```

Este es el **patrón decorator** en un solo struct. El logging es ortogonal a la elección de estrategia; no debería requerir un campo `LogTo string` en cada estrategia. En cambio, *cualquier cosa* que implemente la interfaz puede ser envuelta. Las estrategias futuras no necesitan código específico de logging; obtienen logging gratis.

El mismo patrón aparece en todas partes en sistemas bien diseñados: middleware HTTP, wrappers de observabilidad, decorators de retry. Vale la pena reconocerlo una vez para poder usarlo siempre.

## El comando de barra para testing

`/compact [strategy]` y `/verbose` existen sobre todo para que puedas experimentar sin reiniciar el harness:

| Comando | Efecto |
|---|---|
| `/compact` | Corre la estrategia configurada ahora |
| `/compact sliding` | Corre un `SlidingWindow{KeepLast: 6}` ad-hoc |
| `/compact summarize` | Corre un `Summarize{Threshold: 0, …}` ad-hoc (Threshold: 0 lo fuerza a dispararse) |
| `/compact none` | Corre `NoCompaction` (no-op; útil para "¿cuál es mi baseline?") |
| `/verbose [on\|off]` | Alterna la impresión antes/después en vivo en la compactación |

Esto tiene forma de BYO: puedes sondear interactivamente cómo cada estrategia afecta la misma conversación. Los diffs son iluminadores. Sliding window tira decisiones; summarization machaca la sintaxis; ninguno es perfecto.

## Tropiezos

**Recursión en summarization.** La estrategia `Summarize` llama a `Provider.Send`. Si tu provider de alguna manera corriera `Compactor.Compact` dentro de `Send`, recursarías para siempre. No lo hagas — la compactación se sienta en `agent.loop`, alrededor de `Provider.Send`, nunca dentro.

**El sesgo del system prompt.** Como la summarization comparte el provider, hereda el system prompt. Si tu system prompt es muy específico ("eres un code reviewer"), el resumen puede ser filtrado a través de esa lente. Acéptalo o sobreescribe `Instructions` en `Summarize`.

**La compactación rompiendo el prompt cache.** Si activas prompt caching (nosotros no lo hacemos, pero podrías), cada evento de compactación invalida el cache para ese prefijo — los bytes del prefijo acaban de cambiar. Tanto sliding window como summarization reescriben el prefijo. La vida útil del cache queda topeada en "tiempo entre compactaciones".

> **En el repo actual.** Todo lo relacionado con compactación vive en [`internal/compact/`](../internal/compact/):
>
> - [`strategy.go`](../internal/compact/strategy.go) — la interfaz `CompactionStrategy` + `SafeSplitPoint`
> - [`nocompaction.go`](../internal/compact/nocompaction.go) — el no-op por default
> - [`slidingwindow.go`](../internal/compact/slidingwindow.go) — tira los más viejos
> - [`summarize.go`](../internal/compact/summarize.go) — summarization dirigida por el modelo
> - [`logging.go`](../internal/compact/logging.go) — el decorator `WithLogging(inner, path)`
>
> Un archivo por estrategia, una interfaz que todas implementan. Agregar una nueva (p.ej. el ejercicio `TokenBudget` de abajo) es un archivo nuevo + una línea en `main.go` para activarla.

## Ahora prueba

1. Corre el harness, ten una conversación de 15+ turnos, después `/compact sliding` y `/compact summarize` consecutivos. Compara los `messages` resultantes (usa `/verbose on` primero).
2. Envuelve una estrategia con `WithLogging` y compara dos estrategias sobre la misma conversación corriéndolas con logging a archivos distintos. Diff los archivos.
3. Escribe una estrategia `TokenBudget` que tire los mensajes más viejos hasta que el conteo estimado de tokens esté bajo un umbral configurable. Empieza con el conteo de bytes como proxy de token; después cambia a una llamada `count_tokens` real.

Siguiente: [08 · Mejor entrada](08-better-input.md).
