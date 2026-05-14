# Construye Tu Propio Agente de Código

🌐 **Idiomas:** [English](README.md) · **Español**

Una introducción práctica a la **ingeniería de harness** — la disciplina de construir el andamiaje alrededor de un LLM que lo convierte en un agente útil. Vas a construir un agente de código funcional en Go, y luego experimentar con las partes que importan: proveedores, herramientas, estrategias de compactación y permisos.

## ¿Qué es la ingeniería de harness?

El modelo es el motor. El harness es todo lo demás: el bucle que lo invoca, las herramientas que puede usar, cómo se moldea su conversación con el tiempo, qué tiene permitido hacer, cómo el usuario habla con él.

Si el harness está bien diseñado, un modelo de gama media se siente excelente. Si está mal diseñado, un modelo de vanguardia se siente roto. La mayoría de las decisiones interesantes en herramientas como Claude Code, OpenCode y Aider viven en sus harness, no en sus modelos.

La ingeniería de harness ocurre en tres niveles — **construir** el bucle y las abstracciones, **extenderlas** con herramientas o integraciones nuevas, y **configurar** el comportamiento mediante archivos como `AGENTS.md` y `mcp.json`. La mayoría del tiempo de quienes trabajan con esto se va al nivel superior; este libro se enfoca en construir porque ahí es donde se forma el modelo mental. Una vez que has construido un harness, lees cada archivo de configuración con ojos nuevos.

Este proyecto es una versión simplificada y legible de esas herramientas, diseñada para ser explorada.

## Qué vas a construir

Un agente de código terminal (~600 líneas de Go) que:

- Habla con Claude (o cualquier LLM que conectes)
- Llama herramientas — `bash`, `read_file`, `write_file` — para actuar sobre tu sistema de archivos
- Pide aprobación antes de cada llamada a una herramienta
- Compacta conversaciones largas usando estrategias intercambiables
- Soporta comandos de barra (`/help`, `/model`, `/compact`, `/verbose`, …)
- Tiene un input TUI con historial, edición de línea y prompt estilizado

## Requisitos previos

- Go 1.21+ (el proyecto usa genéricos y la función `max`)
- Una API key de Anthropic — [console.anthropic.com](https://console.anthropic.com) → Settings → API Keys

## Inicio rápido

```sh
git clone git@github.com:betta-tech/byo-coding-agent.git
cd byo-coding-agent
export ANTHROPIC_API_KEY=sk-ant-...
go run .
```

Escribe `/help` para ver los comandos. Prueba con:

- `list the files here`
- `write a hello.txt with a haiku in it`
- `read main.go and tell me how the agent loop works`

## Arquitectura en 60 segundos

El harness está construido alrededor de tres puntos de extensión ortogonales. Cada uno vive en su propio paquete bajo `internal/`, con una interfaz pequeña y una implementación de referencia. Cambiar implementaciones es una sola línea.

```
┌─────────────────────────────────────────────────────┐
│  main.go        cableado · bucle REPL · bucle agente│
│  commands.go    /help · /model · /compact · …       │
└─────────────────────────────────────────────────────┘
        │
        ├── internal/api/             tipos compartidos (Message, Block, ToolDef, …)
        │
        ├── internal/provider/        interfaz Provider + impl Anthropic
        │     Envía mensajes → recibe respuesta. Cámbialo para añadir OpenAI, etc.
        │
        ├── internal/tool/            interfaz Tool + Registry + un archivo por herramienta
        │     Se auto-registra vía init() — añades un archivo y aparece.
        │
        ├── internal/compact/         interfaz CompactionStrategy + estrategias
        │     SlidingWindow, Summarize, NoCompaction, decorador WithLogging.
        │
        └── internal/ui/              banner, spinner, input Bubble Tea, estilos
              Affordances de TUI. Legible pero menos interesante.
```

## Estructura del proyecto

```
.
├── main.go              cableado + REPL + bucle del agente + wrapper executeTool
├── commands.go          registro de comandos de barra
└── internal/
    ├── api/             Message, Block, ToolDef, Response, RenderTranscript
    ├── provider/        interfaz Provider + AnthropicProvider
    ├── tool/            interfaz Tool + Registry + bash / readfile / writefile
    ├── compact/         CompactionStrategy + SlidingWindow / Summarize / Logging
    └── ui/              banner, spinner, input (Bubble Tea), helpers de estilo
```

`internal/` es reforzado por el compilador de Go — los paquetes dentro solo pueden ser importados por código del mismo módulo, lo cual es la señal correcta de "esto no está diseñado para ser reutilizado como librería."

## Los tres puntos de extensión

### 1. Proveedores

¿Quieres usar OpenAI, Bedrock o un modelo local? Implementa `Provider`:

```go
type Provider interface {
    Send(ctx context.Context, messages []Message, tools []ToolDef) (Response, error)
    Model() string
    SetModel(name string)
}
```

Luego cambia una línea en `main.go`:

```go
llm = provider.NewOpenAIProvider(...)  // en lugar de provider.NewAnthropicProvider
```

El adaptador es el único lugar que conoce el formato del SDK. El resto del harness maneja tipos genéricos `Message` / `Block` / `ToolDef`. Mira `internal/provider/anthropic.go` como referencia.

### 2. Herramientas

¿Quieres agregar `git_diff`, `web_search`, `kubectl`? Crea **un solo archivo** bajo `internal/tool/`:

```go
// internal/tool/gitdiff.go
package tool

import (
    "os/exec"

    "github.com/betta-tech/byo-coding-agent/internal/api"
)

type GitDiffTool struct{}

func init() { Default.Register(&GitDiffTool{}) }

func (GitDiffTool) Definition() api.ToolDef {
    return api.ToolDef{
        Name:        "git_diff",
        Description: "Muestra los cambios sin commitear en el repo actual.",
        InputSchema: map[string]any{},
        Required:    []string{},
    }
}

func (GitDiffTool) Execute(_ string) (string, bool) {
    out, err := exec.Command("git", "diff").CombinedOutput()
    if err != nil { return string(out), true }
    return string(out), false
}
```

Añade el archivo. Corre `go run .`. Escribe `/tools` — `git_diff` aparece en la lista, el modelo puede llamarla. **Sin modificaciones a `main.go`** — `main` ya importa `internal/tool`, así que el `init()` del nuevo archivo se ejecuta cuando se carga el paquete.

### 3. Estrategias de compactación

¿Quieres probar diferentes formas de manejar conversaciones largas? Implementa `CompactionStrategy`:

```go
type CompactionStrategy interface {
    Compact(ctx context.Context, messages []Message) ([]Message, error)
}
```

Vienen incluidas tres:

| Estrategia | Qué hace |
|---|---|
| `compact.NoCompaction{}` | Por defecto — nunca modifica los mensajes |
| `&compact.SlidingWindow{KeepLast: 10}` | Conserva los últimos N mensajes, descarta los anteriores |
| `&compact.Summarize{Provider: llm, Threshold: 20, KeepRecent: 6}` | Le pide al modelo que resuma turnos antiguos cuando el historial supera `Threshold` |

Envuelve cualquiera con `compact.WithLogging(inner, "compactions.log")` para grabar diffs antes/después a un archivo — útil para comparar estrategias.

Cambia una línea en `main.go`:

```go
compactor = &compact.Summarize{Provider: llm, Threshold: 20, KeepRecent: 6}
```

Detalle sutil: una truncación ingenua puede dejar un bloque `tool_use` sin su `tool_result` correspondiente, y la API responderá 400. El helper `SafeSplitPoint` en `internal/compact/strategy.go` retrocede hasta encontrar un límite "limpio". Todas las estrategias pasan por él.

## Comandos

| Comando | Efecto |
|---|---|
| `/help` | Lista todos los comandos |
| `/model [nombre]` | Muestra el modelo actual o lo cambia |
| `/clear` | Borra el historial de conversación |
| `/tools` | Lista las herramientas registradas |
| `/compact [sliding\|summarize\|none]` | Ejecuta compactación (estrategia configurada o ad-hoc) |
| `/verbose [on\|off]` | Activa/desactiva la impresión del antes/después al compactar |
| `/exit` | Salir |

## Ahora intenta

En orden aproximado de dificultad:

1. **Agrega una herramienta `git_diff`.** Lee `tool_bash.go` para entender el patrón, escribe un nuevo `tool_git_diff.go`. Verifica que `/tools` la lista.
2. **Agrega una estrategia de compactación `TokenBudget`** que descarte los mensajes más antiguos hasta que el conteo estimado de tokens esté bajo un umbral configurable. Empieza con una aproximación basada en bytes; después intercámbiala por una llamada real a `count_tokens`.
3. **Agrega una abstracción `PermissionPolicy`.** Actualmente cada llamada a herramienta pasa por `confirm`. Refactoriza para que una política decida — `AlwaysAllow`, `AlwaysAsk`, `AllowList{names}`. La política se enchufa en `main.go` como los otros puntos de extensión.
4. **Agrega un segundo proveedor.** OpenAI, un Ollama local, o un `MockProvider` que registre las llamadas (muy útil para el siguiente ejercicio).
5. **Agrega tests.** Con `MockProvider` puedes probar el bucle del agente de extremo a extremo sin una llamada a la API. Las estrategias de compactación son fáciles de probar con historiales sintéticos.

## Qué no está incluido todavía

- **Streaming.** El modelo devuelve una respuesta completa antes de que renderizemos algo. Los agentes de código reales transmiten tokens conforme llegan.
- **Tests.** Nada está automatizado todavía — ver ejercicio 5.
- **Prompt caching.** Cada turno reenvía el historial completo a precio completo.
- **Input multilínea.** El `textarea` de Bubble Tea desbloquearía Shift-Enter para saltos de línea.
- **Políticas de permisos.** La aprobación está hardcodeada como "preguntar cada vez" — ver ejercicio 3.
- **Soporte para MCP.** Sin servidores de herramientas externos.

Cada uno es un capítulo siguiente válido.

## El viaje — léelo como un libro

[`follow_along/`](follow_along/README.md) contiene 14 archivos markdown del tamaño de capítulos que cuentan el *por qué* de cada capa del harness, en el orden en que se construyó (en inglés). Empieza por [`follow_along/00-introduction.md`](follow_along/00-introduction.md) y lee en orden — alrededor de una hora en total.

## Reconocimientos

La estructura toma decisiones arquitectónicas visibles en Claude Code, OpenCode y Aider. El marco "build your own X" viene de proyectos como *Build Your Own Redis*, *Crafting Interpreters* y *Writing An Interpreter In Go* de Thorsten Ball.
