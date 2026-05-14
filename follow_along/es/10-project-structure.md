# 10 · Estructura del proyecto

Para el capítulo 09, el repo tiene 16 archivos Go en el nivel superior. Todo es `package main`. Funciona, pero un lector nuevo tiene que mapear archivos a conceptos por sí mismo.

Este capítulo trata de mover cosas a carpetas — específicamente, a paquetes `internal/`. Es un refactor polémico porque **lo plano es más idiomático en Go** de lo que la gente piensa; lo vamos a hacer de todos modos, y vamos a pagar los costos a la vista.

## Qué dice "best practice" en realidad

La filosofía de layout de proyectos en Go: el layout de paquetes refleja límites de dominio, no tipos de archivo. La biblioteca estándar, `kubectl`, `chi` y la mayoría de los proyectos Go populares mantienen muchos archivos en un solo directorio y dejan que los paquetes emerjan en función de límites semánticos reales.

Un REPL de 600 líneas con un solo binario realmente sí entra bien en un solo paquete. Lo vamos a dividir igualmente, porque **el enfoque BYO tiene una meta diferente**: un aprendiz clonando el repo necesita mapear archivos a conceptos visualmente. Las carpetas hacen obvio "acá viven las herramientas, acá viven los providers" sin tener que leer los headers de los archivos.

Así que este es un tradeoff deliberado: Go menos idiomático, repo más navegable. Si estás usando este codebase como modelo para Go de producción, pésalo según corresponda.

## El layout objetivo

```
.
├── main.go              cableado + REPL + bucle del agente + wrapper executeTool
├── commands.go          registry de comandos de barra (vive en main; toca todo)
└── internal/
    ├── api/             Message, Block, ToolDef, Response, RenderTranscript
    ├── provider/        interfaz Provider + AnthropicProvider
    ├── tool/            interfaz Tool + Registry + bash / readfile / writefile
    ├── compact/         CompactionStrategy + Sliding / Summarize / Logging
    └── ui/              banner, spinner, input (Bubble Tea), helpers de estilo
```

`internal/` es impuesto por el compilador de Go — el código bajo este directorio solo puede ser importado por paquetes dentro del mismo módulo. Esa es la señal correcta para "estas no son APIs públicas, son implementación".

## Qué tuvo que cambiar

Tres categorías de ediciones mecánicas:

### 1. Los tipos compartidos se mueven a `internal/api/`

`Message`, `Block`, `ToolDef`, `Response`, `StopReason`, más helpers como `RenderTranscript` y `Message.HasToolResult`. Las constantes se exportan (`api.RoleUser`, `api.BlockText`). Esta es la capa de la que depende todo lo demás, sin dependencias propias.

### 2. Los tipos cross-package se capitalizan

`type provider` → `type Provider`. `type compactionStrategy` → `type CompactionStrategy`. `safeSplitPoint` → `SafeSplitPoint`. Cualquier cosa llamada desde fuera del paquete tiene que estar exportada. Este es el bloque mecánico más grande; mayormente find-and-replace.

### 3. Renombres de variables para evitar shadowing

El paquete `provider` ahora exporta `Provider`. Una variable llamada `provider` en `main` hace shadowing al import del paquete:

```go
import "github.com/betta-tech/byo-coding-agent/internal/provider"

var provider provider.Provider   // ← error de compilación, más o menos
```

Así que renombramos la variable. Elegimos `llm`:

```go
var llm provider.Provider
```

Se lee natural — `llm.Send(...)`, `llm.SetModel("...")`. Cambio trivial una vez, pero tienes que hacerlo en todos lados.

## Qué no cambió

La arquitectura. El punto entero del refactor es *exponer* lo que ya estaba ahí. Tres puntos de extensión (Provider, Tool, CompactionStrategy) siempre fueron conceptualmente separados; ahora también están físicamente separados.

El bucle del agente, el wrapper executeTool, los comandos — todos siguen viviendo en `package main`. Específicamente:

- `main.go` se queda con el bucle del agente y el cableado.
- `commands.go` se queda con los comandos de barra porque tocan cada punto de extensión — ponerlos en otro lado requeriría pasar todo el estado a través, o hacer todo global *y* exportado. Mejor mantener la capa de integración arriba.

## Por qué `internal/` y no solo `pkg/`

`internal/` es **impuesto por el compilador**: cualquier cosa debajo solo puede ser importada por paquetes dentro del mismo árbol de módulo. Si alguien más hace `go get` de este repo como biblioteca, no puede depender de `internal/tool/`. Esa es la señal correcta para "API estable del harness: no hay".

`pkg/` es convención — los proyectos Go más viejos lo usan para "código que está pensado para ser importado", pero Go mismo no impone nada. Para un binario que no está pensado para ser reutilizado como biblioteca, `internal/` es lo correcto.

## Module path

El module path cambia de `harness` a `github.com/betta-tech/byo-coding-agent` para coincidir con la URL de GitHub. Esto no es estrictamente necesario (puedes tener cualquier nombre de módulo), pero coincidir con la URL es la convención, y ahorra renombres futuros si/cuando alguien forkee.

```sh
go mod edit -module github.com/betta-tech/byo-coding-agent
```

Después cada import interno se vuelve:

```go
import "github.com/betta-tech/byo-coding-agent/internal/api"
```

Los imports largos son el costo de los module paths completamente calificados. Los editores los auto-completan; los humanos hacen grep por el último componente.

## Tropiezos

**Ciclos de importación.** El riesgo más grande. El primer ciclo con el que nos topamos fue:

```
agent → ui → subagent → agent
```

(`agent` usaba `ui.StartSpinner`; `ui` necesitaba `subagent.Active()` para el status bar; `subagent.Research` construía un `Agent`.)

El fix fue hacer que `agent` no importe `ui` — quitar la llamada al spinner del bucle del agente (el status bar lo reemplaza en el capítulo 12), e inline un diff de compactación en texto plano. Dirección de dependencia más limpia de todos modos: agent depende solo de api, compact, provider, tool.

Reglas de pulgar para evitar estos:

- `api` no depende de nada. Es el fondo del stack de dependencias.
- Los paquetes de lógica (`agent`, `compact`) dependen de `api` y entre sí selectivamente. Nunca dependen de UI.
- Los paquetes de UI dependen de los paquetes de lógica y de `api`, nunca al revés.

**Dónde vive DelegateTool.** Todavía no la tenemos (el capítulo 11 la introduce), pero spoiler: `DelegateTool` termina en `main` en lugar de `internal/tool/` para evitar `tool → subagent → agent → tool`. A veces la respuesta correcta es "no lo pongas en el paquete obvio".

**Los tests ahora son por paquete.** Con un layout plano, `internal_test.go` podía tocar cualquier cosa. Con paquetes `internal/`, escribes tests por paquete, y las APIs exportadas son lo único alcanzable desde los tests de otros paquetes. Eso es buena disciplina pero un cambio.

## Qué mantuvimos simple

**No** creamos subpaquetes dentro de `internal/provider/anthropic/` o `internal/tool/bash/`. Solo hay `internal/provider/` (con la interfaz y la impl de Anthropic) e `internal/tool/` (con la interfaz y cada herramienta como archivo).

El anidado más profundo hubiera sido más "idiomático" en algunos sentidos pero hubiera matado el truco de auto-registro (cada subpaquete necesitaría su propio import en `main`). El layout más plano preserva "suelta un archivo, aparece".

> **En el repo actual.** El layout en este capítulo es exactamente el que está en HEAD. Recórrelo desde arriba:
>
> - [`main.go`](../main.go) + [`commands.go`](../commands.go) + [`delegate.go`](../delegate.go) — la capa de cableado
> - [`internal/api/`](../internal/api/) — tipos compartidos, sin dependencias internas
> - [`internal/provider/`](../internal/provider/) — interfaz Provider + impl Anthropic
> - [`internal/tool/`](../internal/tool/) — interfaz Tool + registry + cada herramienta
> - [`internal/compact/`](../internal/compact/) — estrategias + decorator
> - [`internal/agent/`](../internal/agent/) — el struct agent (agregado en el capítulo 11)
> - [`internal/subagent/`](../internal/subagent/) — abstracción de subagentes (capítulo 11)
> - [`internal/ui/`](../internal/ui/) — banner, spinner, programa Bubble Tea (capítulo 12)

## Ahora prueba

1. Imagina que eres un lector nuevo. Sin mirar `main.go`, navega el repo e intenta anotar: ¿dónde vive el bucle del agente? ¿Dónde viven las herramientas? ¿Dónde sucede la traducción de la API? Si la estructura está clara, deberías responder en menos de un minuto.
2. Corre `go mod why github.com/anthropics/anthropic-sdk-go` y rastrea el path del import. Solo un paquete debería depender de él.
3. Intenta agregar un paquete nuevo `internal/cache/` que dependa de `internal/agent`. ¿Se rompe algo? (No debería — agent no depende de cache, así que no hay ciclo.) Ahora inviértelo. (Ahora tienes agent → cache, lo que está bien si cache no depende de agent.)

Siguiente: [11 · Subagentes](11-subagents.md).
