# 11 · Subagentes

Un subagente es un bucle de agente separado, lanzado por el agente principal, con su propia ventana de contexto y subconjunto de herramientas, que devuelve su respuesta final como un único tool result.

¿Por qué molestarse?

- **Investigar sin contaminar el contexto.** Leer 20 archivos para encontrar una respuesta usa 20 bloques tool-result en la conversación principal. Un subagente de investigación lee esos 20 archivos, devuelve una respuesta sintetizada, y el agente principal ve esa una sola línea.
- **Especialización.** Un subagente de code-review tiene herramientas y prompts distintos a uno de refactoring. El agente principal elige el especialista correcto.
- **Control de costos.** Los subagentes pueden usar modelos más baratos. (Nuestra implementación no, pero la arquitectura lo soporta trivialmente.)

Este capítulo son dos refactors y una feature nueva, en orden.

## Refactor 1: extraer `Agent` como struct

Ahora mismo `agentLoop` es una función que lee globales a nivel de paquete (`provider`, `messages`, `compactor`, `tools`). Para tener *dos* agentes (root + subagente), cada uno necesita su propio estado.

```go
// internal/agent/agent.go
type Agent struct {
    Name      string
    Provider  provider.Provider
    Tools     *tool.Registry
    Compactor compact.CompactionStrategy
    System    string
    MaxTurns  int
    Verbose   bool
    LogPrefix string
    Quiet     bool
    Confirm   func(prompt string) bool

    messages []api.Message
}

func New(p provider.Provider, system string, tools *tool.Registry) *Agent { /* … */ }
func (a *Agent) Send(ctx context.Context, prompt string) (string, error) { /* … */ }
func (a *Agent) Messages() []api.Message { return a.messages }
func (a *Agent) ClearMessages()            { a.messages = a.messages[:0] }
func (a *Agent) SetMessages(m []api.Message) { a.messages = m }
```

`Send` agrega el prompt que le pasas como un mensaje de rol user y después corre el bucle. La lógica del bucle es lo que `agentLoop` solía ser — movida tal cual, con `provider` → `a.Provider`, `messages` → `a.messages`, etc.

Los campos `LogPrefix` y `Quiet` son cómo distinguimos root de subagente visualmente:

| | Agente root | Subagente |
|---|---|---|
| `LogPrefix` | `""` | `"  ↳ "` |
| `Quiet` | `false` | `true` |
| `Confirm` | `ui.Confirm` (te pregunta) | `nil` (auto-aprobar) |
| `Name` | `""` | `"research"` |

Así que las tool calls se indentan (`  ↳ [tool] read_file ...`), el texto del assistant es silencioso (los comentarios de trabajo del subagente no hacen eco; solo devolvemos la respuesta final), y la aprobación es implícita — que apruebes la llamada delegate del padre cuenta como aprobación para todo lo de adentro.

## Refactor 2: la abstracción `Subagent`

Mismo patrón que Provider, Tool, CompactionStrategy:

```go
// internal/subagent/registry.go
type Subagent interface {
    Name() string
    Description() string
    Run(ctx context.Context, task string) (string, error)
}

type Registry struct { /* … */ }
var Default = NewRegistry()
```

Más un tracker — `Begin(name)` / `Active()` — para que la UI muestre qué está corriendo:

```go
func Begin(name string) func() {
    trk.mu.Lock()
    trk.active[name]++
    trk.mu.Unlock()
    return func() {
        trk.mu.Lock()
        trk.active[name]--
        // …
        trk.mu.Unlock()
    }
}
```

Los subagentes llaman a `defer Begin(name)()` al inicio de `Run`. El indicador de estado de la TUI lee `Active()` para mostrar los subagentes en vuelo.

Un subagente concreto, `Research`, vive en `internal/subagent/research.go`. Construye un `Agent` con herramientas de solo lectura, corre una vez, devuelve el resultado:

```go
type Research struct {
    Provider provider.Provider
    Tools    *tool.Registry  // subset curado
}

func (Research) Name() string        { return "research" }
func (Research) Description() string { /* … */ }

func (r Research) Run(ctx context.Context, task string) (string, error) {
    defer Begin(r.Name())()
    a := agent.New(r.Provider, researchSystem, r.Tools)
    a.Name = r.Name()
    a.LogPrefix = "  ↳ "
    a.Quiet = true
    a.MaxTurns = 10
    return a.Send(ctx, task)
}
```

## La herramienta delegate

¿Cómo *invoca* el agente principal a un subagente? A través de una herramienta, como todo lo demás:

```go
type DelegateTool struct {
    Subagent subagent.Subagent
}

func (d *DelegateTool) Definition() api.ToolDef {
    return api.ToolDef{
        Name:        "delegate_" + d.Subagent.Name(),
        Description: d.Subagent.Description(),
        InputSchema: map[string]any{
            "task": map[string]any{"type": "string", "description": "…"},
        },
        Required: []string{"task"},
    }
}

func (d *DelegateTool) Execute(ctx context.Context, rawInput string) (string, bool) {
    var in struct{ Task string `json:"task"` }
    json.Unmarshal([]byte(rawInput), &in)
    fmt.Println(ui.Dimmed("↳ delegating to " + d.Subagent.Name() + " subagent"))
    result, err := d.Subagent.Run(ctx, in.Task)
    fmt.Println(ui.Dimmed("← " + d.Subagent.Name() + " subagent done"))
    if err != nil { return err.Error(), true }
    return result, false
}
```

`main` construye un `DelegateTool` por cada subagente y lo registra en `tool.Default`. El modelo ve `delegate_research` en su lista de herramientas y puede elegir llamarla.

## Dónde vive DelegateTool (y por qué)

Hay un problema sutil: `DelegateTool` necesita `subagent.Subagent`. Si la ponemos en `internal/tool/`, eso crea `tool → subagent`. Pero `subagent` importa `agent`, y `agent` importa `tool`. Tenemos un ciclo:

```
tool → subagent → agent → tool
```

El fix: poner `DelegateTool` en `main`. Main importa todo, sin ciclo. La interfaz `Tool` vive en `internal/tool/`, pero cualquier cosa puede implementarla.

Este es un patrón recurrente — el lugar más limpio para el "pegamento" entre dos abstracciones suele ser arriba, no dentro de ninguna de las dos abstracciones.

## Por qué los subagentes no auto-registran

Las herramientas se auto-registran vía `init()`. ¿Por qué no los subagentes?

Los subagentes necesitan configuración: un `Provider`, a veces un subset de herramientas, a veces un modelo. Nada de eso existe en tiempo de `init()` — esos son valores de runtime construidos en `main`.

Así que el registro es explícito:

```go
// main.go
func registerSubagents(llm provider.Provider) {
    subagent.Default.Register(subagent.Research{
        Provider: llm,
        Tools:    tool.Default.Subset("read_file"),
    })
    for _, sa := range subagent.Default.All() {
        tool.Default.Register(&DelegateTool{Subagent: sa})
    }
}
```

Dos líneas por subagente — una para construir + registrar el subagente, una (implícita en el loop) para registrar su herramienta delegate. Menos magia que el patrón de herramientas, más honesta sobre la dependencia.

## Lograr que el modelo realmente delegue

La sorpresa más grande de este capítulo: **Opus 4.7 tiene sesgo en contra de usar subagentes por default.** Comportamiento documentado. Un system prompt suave que dice "usa el subagente cuando necesites muchas lecturas de archivo" se interpreta como "ya lo hago yo mismo, está bien".

El fix está en el prompt. Hazlo explícito:

> For READ-ONLY INVESTIGATION you SHOULD call delegate_research rather than reading files yourself. This includes questions like:
>
> - "where is X defined?"
> - "what fields does Y have?"
> - …
>
> Prefer delegating for investigation, even when you think one or two reads would do it.

Y haz que la descripción de la herramienta sea imperativa, no descriptiva:

> Investigate the codebase or filesystem and return a focused answer. Prefer this over reading files yourself when the user asks ANY question about the code.

Esto es ingeniería de harness en su forma más directa: la personalidad aparente del modelo es tuya para moldearla. Si los subagentes no se están usando, el problema casi siempre es tu prompt, no la arquitectura.

## El resultado visible

Cuando preguntas "¿dónde está definido el bucle del agente?" — y el prompt es correcto — la conversación se ve así:

```
[tool] delegate_research {"task":"locate the agent loop"}
approve? [y/n] y
↳ delegating to research subagent
  ↳ [tool] read_file {"path":"main.go"}
  ↳ [tool] read_file {"path":"internal/agent/agent.go"}
← research subagent done (2.1s)
The agent loop is in internal/agent/agent.go, line 88...
```

Tres pistas visibles: el header, las tool calls indentadas, el footer con el tiempo transcurrido.

## Tropiezos

**Subagentes solo síncronos.** Nuestros subagentes corren inline — la tool call del agente padre se bloquea hasta que el subagente termina. Eso significa que no puedes fanout de dos subagentes en paralelo. El paralelismo es factible pero requiere que el padre emita ambas tool calls en un turno, y que el registry de subagentes soporte llamadas `Run` concurrentes (el tracker sí; el resto es plumbing).

**Visibilidad mientras corre.** Como el REPL está bloqueado mientras el agente corre, no puedes escribir `/subagents` para ver qué está en vuelo. El tracker `subagent.Active()` está preparado para eso, pero solo la TUI del capítulo 12 realmente muestra los datos en tiempo real.

**Subsets de herramientas.** `tool.Default.Subset("read_file")` construye un registry nuevo que contiene *solo* `read_file`. El subagente solo ve ese subset, así que no puede correr comandos de shell. Esto es curaduría, no restricción — el registry es un valor común y corriente, no una construcción de permisos. Si necesitas restricciones reales (sandbox), tienen que vivir en el `Execute` de la herramienta misma.

> **En el repo actual.** Las piezas de este capítulo:
>
> - El struct `Agent`: [`internal/agent/agent.go`](../internal/agent/agent.go). Lee la lista de campos arriba — `Name`, `LogPrefix`, `Quiet`, `Confirm` son los campos que difieren entre root y subagente.
> - La interfaz `Subagent` y el active-tracker: [`internal/subagent/registry.go`](../internal/subagent/registry.go).
> - El subagente de research: [`internal/subagent/research.go`](../internal/subagent/research.go). Nota el system prompt y cómo se construye con `LogPrefix: "  ↳ "`, `Quiet: true`, y `MaxTurns: 10`.
> - La herramienta delegate: [`delegate.go`](../delegate.go) (en la raíz del repo, no en `internal/tool/`, para evitar el ciclo de imports descrito arriba).
> - Registro de subagentes: mira `registerSubagents()` en [`main.go`](../main.go).

## Ahora prueba

1. Agrega un segundo subagente: `CodeReview`. Herramientas: `read_file`, `bash` (así puede correr linters). System prompt: "You are a code reviewer. …". Regístralo. Confirma que `delegate_codereview` aparece en `/tools`.
2. Ten una conversación larga. Justo después de una llamada delegate, escribe `/subagents`. (No vas a atrapar uno en vuelo a menos que seas rápido — pero los subagentes registrados siempre se muestran.)
3. Modifica `Research.Run` para que los subagentes respeten una función Confirm pasada desde el padre. Ese es el camino a la aprobación *recursiva* — cada tool call del subagente también te pregunta. UX más pesada, más visibilidad.

Siguiente: [12 · La TUI completa](12-full-tui.md).
