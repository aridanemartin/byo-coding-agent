# 09 · Herramientas plug-and-play

Abstrajimos el backend del LLM (capítulo 03) y la estrategia de compactación (capítulo 07). El siguiente hueco obvio son las herramientas.

Ahora mismo las herramientas viven como dos cosas desconectadas: un slice `[]ToolDef` (mandado al modelo) y un `switch name {}` en `executeTool` (lo que se corre). Agregar una herramienta significa editar dos lugares.

Este capítulo los unifica.

## El patrón, una vez más

Ya hicimos esto dos veces:

```
interfaz → implementaciones swap-friendly → reemplazo de una línea
```

Para herramientas, con un giro: las herramientas son **aditivas** (puedes tener muchas a la vez), no exclusivas (tienes un provider, un compactor). Así que en lugar de "una línea en main.go para cambiar", tenemos un **registry**:

```go
type Tool interface {
    Definition() api.ToolDef
    Execute(ctx context.Context, input string) (result string, isError bool)
}

type Registry struct {
    tools map[string]Tool
}

func (r *Registry) Register(t Tool) { r.tools[t.Definition().Name] = t }
func (r *Registry) Definitions() []api.ToolDef { /* ordenado */ }
func (r *Registry) Execute(ctx context.Context, name, input string) (string, bool) { /* dispatch */ }

var Default = NewRegistry()
```

El bucle del agente llama a `registry.Definitions()` para la request a la API y a `registry.Execute(ctx, name, input)` para el dispatch. No sabe qué herramientas existen.

## Auto-registro vía `init()`

La cereza del postre: las herramientas se registran solas cuando se carga el archivo. Cada herramienta tiene su propio archivo:

```go
// internal/tool/bash.go
package tool

type BashTool struct{}

func init() { Default.Register(&BashTool{}) }

func (BashTool) Definition() api.ToolDef { /* schema */ }
func (BashTool) Execute(ctx context.Context, rawInput string) (string, bool) { /* impl */ }
```

Para agregar una herramienta nueva:

1. Suelta un archivo en `internal/tool/` con `package tool` arriba.
2. Implementa `Tool` (dos métodos).
3. Agrega `func init() { Default.Register(&YourTool{}) }`.

Eso es todo. **Sin ediciones a `main.go`.** Cuando el paquete carga, Go corre el `init()` de cada archivo, cada herramienta se registra, el agente las ve todas.

Este truco — usar `init()` para auto-registro — es el mismo que usan los drivers de `database/sql`. El flujo "suelta un archivo, aparece" es uno de los patrones más placenteros en Go.

## Por qué funciona (y por qué no siempre)

Funciona **porque todos los archivos de herramientas están en el mismo paquete.** Cuando `main` importa `internal/tool`, Go compila todos los archivos de ese directorio, corre todos los `init()`, registra todas las herramientas.

Si dividieras las herramientas en subpaquetes (`internal/tool/bash/`, `internal/tool/read_file/`), perderías el truco: `main` tendría que hacer `import _ "internal/tool/bash"` para cada uno para disparar el `init()` de ese paquete. El problema de "listar cada herramienta en algún lado" vuelve. A propósito mantuvimos todas las herramientas en una sola carpeta para preservar la propiedad.

## Qué puedes hacer con esto

Pruébalo. Haz un archivo nuevo:

```go
// internal/tool/gitdiff.go
package tool

import (
    "context"
    "os/exec"

    "github.com/betta-tech/byo-coding-agent/internal/api"
)

type GitDiffTool struct{}

func init() { Default.Register(&GitDiffTool{}) }

func (GitDiffTool) Definition() api.ToolDef {
    return api.ToolDef{
        Name:        "git_diff",
        Description: "Show uncommitted changes in the current repo.",
        InputSchema: map[string]any{},
        Required:    []string{},
    }
}

func (GitDiffTool) Execute(ctx context.Context, _ string) (string, bool) {
    out, err := exec.CommandContext(ctx, "git", "diff").CombinedOutput()
    if err != nil { return string(out), true }
    return string(out), false
}
```

Corre `go run .`, escribe `/tools`. `git_diff` está en la lista. El modelo la puede llamar.

## La limpieza que recibió `executeTool`

Después de este refactor, `executeTool` en `main.go` baja de un gran switch a un wrapper delgado:

```go
func executeTool(name, rawInput string) (string, bool) {
    fmt.Printf("[tool] %s %s\n", name, rawInput)
    if !confirm("approve?") {
        return "user denied this tool call", true
    }
    return registry.Execute(ctx, name, rawInput)
}
```

Tres preocupaciones separadas:

| Preocupación | Dueño |
|---|---|
| Logging | `main` (el print `[tool] …`) |
| Aprobación | `main` (llama a `confirm`) |
| Dispatch | `registry.Execute` |

El comportamiento de cada herramienta está en su propio archivo. Cada preocupación transversal está en `main`. Esta es la forma que quieres: una capa superior delgada que sabe de cada punto de extensión y los controla, más una colección gorda de archivos chicos que no se conocen entre sí.

## Entradas de herramientas y el argumento `context.Context`

Una vez que nos ponemos serios, el `Execute` de cada herramienta recibe un `context.Context`. `bash` de verdad lo usa (`exec.CommandContext`) así que un comando de larga duración se puede cancelar cuando el agente es interrumpido. Otras (`read_file`, `write_file`) lo aceptan e ignoran.

Pasar el contexto por cada capa (agent → registry → tool) es un idiom de Go. Rinde frutos cuando agregas timeout/cancelación después. Mejor agregarlo ahora que retrofitearlo.

## Tropiezos

**La iteración de map es random.** `Definitions()` devolvería herramientas en un orden aleatorio si iteraras `r.tools` directamente. Dos llamadas a la API se serializarían distinto, rompiendo prompt caching. El fix es una línea extra: ordenar por nombre primero.

```go
sort.Strings(names)
for _, n := range names {
    out = append(out, r.tools[n].Definition())
}
```

**`Default` es un global.** Como todos los globales, es un lugar tentador para colgar estado. Aguanta. Las herramientas deberían ser chicas y enfocadas. Si una herramienta necesita configuración, recíbela como campo de struct y deja que `main` la construya — mira la herramienta delegate en el capítulo 11 para un ejemplo.

**Auto-registro vs registro explícito.** `init()` funciona genial para herramientas que son tipos puros (sin configuración necesaria). Para herramientas que necesitan un `Provider`, un `Config` o cualquier otra dependencia de runtime, no puedes auto-registrar — necesitas un `registry.Register(&MyTool{Provider: llm})` explícito en `main`. Nos topamos con esto con los subagentes en el capítulo 11.

> **En el repo actual.** Todo lo relacionado con herramientas vive en [`internal/tool/`](../internal/tool/):
>
> - [`registry.go`](../internal/tool/registry.go) — la interfaz `Tool`, el struct `Registry` y el registry global `Default`
> - [`bash.go`](../internal/tool/bash.go), [`readfile.go`](../internal/tool/readfile.go), [`writefile.go`](../internal/tool/writefile.go) — un archivo por herramienta, cada uno con un `init()` de 1 línea que se registra a sí mismo
>
> El bucle del agente en [`internal/agent/agent.go`](../internal/agent/agent.go) llama a `a.Tools.Definitions()` para la request a la API y a `a.Tools.Execute(ctx, name, input)` para el dispatch. No sabe qué herramientas existen — ese es el punto entero.

## Ahora prueba

1. Agrega una herramienta `web_fetch` que reciba una URL y devuelva el body de la respuesta. (Usa `net/http`. Pon un timeout.)
2. Lee `internal/tool/registry.go` y encuentra el método `Subset(...names)`. Devuelve un Registry nuevo que contiene solo las herramientas nombradas. Después lee `internal/subagent/research.go` para ver por qué eso importa (próximo capítulo).
3. Prueba registrar dos herramientas con el mismo `Name()`. ¿Qué pasa? ¿Dónde en el registry agregarías un check de duplicados?

Siguiente: [10 · Estructura del proyecto](10-project-structure.md).
