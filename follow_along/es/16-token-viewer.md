# 16 · El visor de tokens

Vamos a implementar esto:
<img width="298" height="129" alt="bettatech-tui-cost" src="https://github.com/user-attachments/assets/b5b81e52-8d34-4375-ba67-0394c33b74e8" />

Cada llamada a la API devuelve un objeto `usage`: cuántos tokens de entrada costó, cuántos tokens de salida produjo, cuántos se sirvieron desde el prompt cache. El harness ha estado tirando esa información a la basura desde el capítulo 01. Este capítulo la conecta a través de todas las capas.

El resultado: un comando `/tokens` que imprime un desglose acumulado, y una línea de estado en vivo en la parte inferior de la TUI que se actualiza después de cada turno. Aproximadamente 80 líneas de código repartidas en cuatro archivos.

## Por qué esto importa

Dos razones, ninguna de vanidad:

1. **Intuición de coste.** Hasta que ves el número subir, "1.000 tokens" es abstracto. Una vez que ves una sola conversación llegar a 200 K tokens tras doce turnos y veinte lecturas de archivo, empiezas a escribir prompts más ajustados y a configurar la compactación antes.
2. **Depuración.** Cuando el agente se siente lento o caro, el panel de tokens te dice a dónde se fue el presupuesto — fallos de caché, salidas de herramienta desbordadas, un `AGENTS.md` que olvidaste recortar.

La funcionalidad también es un buen recorrido de cómo los datos específicos del proveedor (que no están en la interfaz) fluyen hacia arriba a través de las capas del harness. Vale la pena hacerlo una vez.

## Qué nos da la API

La API Messages de Anthropic devuelve esto en cada respuesta:

```json
{
  "usage": {
    "input_tokens": 1234,
    "output_tokens": 567,
    "cache_creation_input_tokens": 0,
    "cache_read_input_tokens": 0
  }
}
```

Los campos de caché son cero salvo que el prompt caching esté activado. El precio difiere por categoría — las lecturas de caché son unas 10× más baratas que los tokens de entrada nuevos — así que rastreamos los cuatro por separado.

## Conectándolo de extremo a extremo

Tres pasos, de arriba abajo:

### 1. `api.Usage` y `Response.Usage`

Una struct nueva en `internal/api/types.go`:

```go
type Usage struct {
    InputTokens         int
    OutputTokens        int
    CacheCreationTokens int
    CacheReadTokens     int
}

func (u Usage) Add(other Usage) Usage { /* suma campo a campo */ }
```

Y un campo en `Response`:

```go
type Response struct {
    Content    []Block
    StopReason StopReason
    Usage      Usage          // ← nuevo
}
```

`Usage` vive en `api` porque es la forma agnóstica al proveedor — cualquier backend que tenga un concepto de "tokens" puede rellenarlo. Los `usage.prompt_tokens` / `completion_tokens` de OpenAI se mapean limpiamente; un hipotético proveedor de modelos locales simplemente lo dejaría en cero.

### 2. El proveedor lo rellena y lo acumula

El adaptador de Anthropic copia el usage desde la respuesta del SDK y lo va sumando a un total acumulado de toda la sesión:

```go
type AnthropicProvider struct {
    // … campos existentes …
    mu    sync.Mutex
    total api.Usage          // acumulado a lo largo de cada llamada a Send
}

// Dentro de Send, después de la llamada a la API:
out.Usage = api.Usage{
    InputTokens:         int(resp.Usage.InputTokens),
    OutputTokens:        int(resp.Usage.OutputTokens),
    CacheCreationTokens: int(resp.Usage.CacheCreationInputTokens),
    CacheReadTokens:     int(resp.Usage.CacheReadInputTokens),
}
p.mu.Lock()
p.total = p.total.Add(out.Usage)
p.mu.Unlock()
```

El mutex importa porque los subagentes (capítulo 11) corren en goroutines que pueden compartir el proveedor con el agente root. Dos llamadas `Send` simultáneas tendrían una carrera sobre el total sin protección. Un `sync.Mutex` simple basta — bloqueamos durante nanosegundos, sin contención que valga la pena medir.

Dos métodos nuevos en el proveedor exponen los totales y el coste en dólares:

```go
func (p *AnthropicProvider) TotalUsage() api.Usage { /* lectura protegida por mutex */ }
func (p *AnthropicProvider) EstimatedCostUSD() float64 { /* total × tabla de precios */ }
```

### 3. Tabla de precios

El coste depende del modelo. Hardcodeamos las tarifas que el harness conoce:

```go
type pricing struct {
    InputPerMillion         float64
    OutputPerMillion        float64
    CacheCreationPerMillion float64
    CacheReadPerMillion     float64
}

var modelPricing = map[string]pricing{
    "claude-opus-4-7":   {15.00, 75.00, 18.75, 1.50},
    "claude-sonnet-4-6": {3.00,  15.00, 3.75,  0.30},
    "claude-haiku-4-5":  {1.00,  5.00,  1.25,  0.10},
}
```

Los modelos desconocidos devuelven `-1` desde `EstimatedCostUSD()`. El comando de barra y la línea de estado lo renderizan como `(modelo desconocido)` en lugar de imprimir $0.0000 — los ceros silenciosos son peores que un espacio en blanco visible.

## Por qué estos métodos se quedan fuera de la interfaz `Provider`

La interfaz `Provider` en el capítulo 03 fue deliberadamente mínima:

```go
type Provider interface {
    Send(...) (Response, error)
    Model() string
    SetModel(name string)
}
```

`TotalUsage()` y `EstimatedCostUSD()` no están ahí. Dos razones:

1. **No todo backend tiene un concepto de tokens** en la misma forma. Un proveedor que solo hace streaming podría reportar tokens de otra manera; un proveedor de modelo local podría no reportarlos en absoluto. Forzar que la interfaz tenga estos métodos significa que cada implementación tiene que implementarlos, aunque sean no-ops.
2. **El harness los llama en exactamente un sitio** — la `usageFunc` de `main.go`. Sabemos que tenemos un `AnthropicProvider` ahí porque es lo que construimos. No hace falta type assertion.

Este es el modismo de **tipado estructural** en el que puedes apoyarte en Go: las capacidades opcionales van en el tipo concreto; los llamadores que las necesitan o conocen el tipo concreto o hacen una type assertion. La interfaz se mantiene estrecha.

## El comando de barra

`/tokens` llama a los métodos directamente (como `rootAgent.Provider` está tipado como la interfaz, hacemos una type assertion a través de un pequeño contrato):

```go
type tokenReporter interface {
    TotalUsage() api.Usage
    EstimatedCostUSD() float64
}

func cmdTokens(_ string) {
    stats, ok := rootAgent.Provider.(tokenReporter)
    if !ok {
        fmt.Println(ui.Dimmed("este proveedor no reporta uso de tokens"))
        return
    }
    // imprime con formato stats.TotalUsage() y stats.EstimatedCostUSD()
}
```

La interfaz `tokenReporter` existe solo dentro de `commands.go`. Definirla en línea es el idiom de Go para "quiero preguntar si este valor puede hacer X" sin acoplar al productor y al consumidor. Mismo patrón que `io.Reader` vs `bytes.Buffer` — los buffers no importan `io` para satisfacer `Reader`; la interfaz es el contrato del consumidor.

Salida:

```
session usage:
  input          12,034
  output         3,891
  est. cost      $0.4729
```

Las líneas de caché solo se imprimen cuando el valor no es cero — el ruido visual cuesta más que el detalle ausente.

## La línea de estado en vivo

El capítulo 12 reservó una línea encima de la caja de entrada para el spinner. Cuando el agente está inactivo, esa línea estaba en blanco. La reutilizamos para el uso de tokens.

```go
switch m.state {
case stateRunning:
    statusLine = "  " + m.spinner.View() + " " + Dimmed("thinking..."+activeSubagentSummary())
case stateIdle:
    statusLine = "  " + Dimmed(m.usageStatus())
}
```

`usageStatus()` formatea los mismos números que el comando de barra, en una línea:

```
  12,034 in · 3,891 out · ~$0.4729
```

Con actividad de caché, añade un término `cache 0/1,200` en el medio.

### Cómo llegan los números al modelo

La TUI no sabe de proveedores. Recibe una `UsageFunc` en su construcción:

```go
type UsageFunc func() (api.Usage, float64)

func NewProgram(runner AgentRunner, usageFunc UsageFunc) *tea.Program { /* … */ }
```

`main.go` proporciona el closure:

```go
usageFunc := func() (api.Usage, float64) {
    return llm.TotalUsage(), llm.EstimatedCostUSD()
}
program := ui.NewProgram(runner, usageFunc)
```

El closure se llama desde `View()` cada vez que la TUI re-renderiza el área de entrada. Eso pasa con cada pulsación de tecla, cada mensaje agregado, cada tick — pero el coste es un mutex lock y cuatro lecturas de int. Suficientemente barato como para no molestarse en cachear.

### Por qué un closure y no un mensaje de Bubble Tea

Podríamos haber enviado un `UsageMsg` después de cada turno, actualizando un campo en el modelo. Eso sería más idiomático en Bubble Tea. El closure es más simple y los datos son de solo lectura — la TUI nunca los muta — así que no hay beneficio en el patrón de mensaje.

La regla general: **`tea.Msg` es para cambios de estado a los que quieres que reaccione el loop de Update. Un closure que devuelve un snapshot está bien cuando el único consumidor es `View`.**

## Tropiezos

**Variación de precios.** Las tarifas cambian. Los precios hardcodeados en `modelPricing` se desactualizarán, a veces mucho. Márcalos con un comentario de fecha, prefija los costes mostrados con `~`, y pon la URL de origen en el comentario para que los revisores sepan dónde actualizar.

**Estimaciones de tokens desde el modelo.** No confundas esto con `client.Messages.CountTokens(...)`, que estima *antes* de una llamada. Nuestro visor de tokens reporta los *reales* que la API devolvió. La API de estimación existe si quieres una pista del tipo "este turno costaría ~X" antes de enviar; aquí no la usamos.

**Sesiones multi-proveedor.** Si cambias de proveedor a mitad de sesión (`/model` cambia dentro de Anthropic, pero imagina un hipotético `/provider openai`), el total acumulado es específico del proveedor. Dividir los subtotales por proveedor es una extensión razonable; no la hacemos.

**Streaming.** Cuando agregues streaming (capítulo 13), el SDK entrega `usage` solo en el evento final `message_stop`. Mueve la actualización del acumulador a ese evento. La impresión por-turno del coste en el bucle del agente no cambia.

> **En el repositorio actual.** Las piezas de este capítulo viven en:
>
> - [`internal/api/types.go`](../internal/api/types.go) — struct `Usage` y campo `Response.Usage`.
> - [`internal/provider/anthropic.go`](../internal/provider/anthropic.go) — el acumulador, el mutex, `TotalUsage()`, `EstimatedCostUSD()`, la tabla `modelPricing`.
> - [`commands.go`](../commands.go) — el comando `/tokens` y la interfaz `tokenReporter` en línea.
> - [`internal/ui/program.go`](../internal/ui/program.go) — `UsageFunc`, el campo en el modelo del harness, `usageStatus()`, el renderizado de la línea de estado.

## Ahora prueba

1. Ejecuta el harness, hazle una pregunta pequeña, después `/tokens`. Compara el reparto entrada/salida — el system prompt + las definiciones de herramientas suman lo suyo.
2. Ten una conversación más larga (10+ turnos). Observa el contador en vivo en la línea de estado subir entre turnos. Nota cómo las llamadas a subagentes añaden bloques grandes de entrada (el prompt de research + las descripciones de herramientas enviadas al subagente).
3. Pon `/model claude-haiku-4-5` y haz la misma pregunta de nuevo. La línea de coste debería bajar 15×. Los conteos de tokens apenas cambian — la diferencia es enteramente la tabla de precios.

← [volver al índice](README.md)
