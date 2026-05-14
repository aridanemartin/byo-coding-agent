# 14 · Agregando soporte de MCP

El harness tiene su propia interfaz `Tool` — structs Go en `internal/tool/`, cada uno con un `Definition()` y un `Execute()`. Eso funcionó porque cada herramienta que queríamos era una operación local que podíamos escribir en Go.

**MCP — el Model Context Protocol** — es el estándar para herramientas que viven *fuera* de tu proceso. Un servidor MCP puede ser un servidor de operaciones de Git, un lector de Slack, una interfaz de consulta a base de datos, un montador de sistema de archivos, cualquier cosa que alguien más haya escrito y publicado. Agregar soporte de MCP significa permitir que el agente use esos servidores como si fueran herramientas locales.

Este capítulo trata de tender el puente entre los dos mundos: un protocolo externo de un lado, nuestra interfaz `Tool` del otro.

## Qué es MCP en realidad

MCP es un protocolo JSON-RPC. Un servidor MCP habla uno de tres transportes:

| Transporte | Para qué se usa |
|---|---|
| **stdio** | Subprocesos locales. El servidor es un binario que lanzas; te comunicas con él por su stdin/stdout. |
| **HTTP / SSE** | Servidores remotos, despliegues multi-tenant. |
| **WebSocket** | Remoto bidireccional, menos común. |

El protocolo define:

- `tools/list` — ¿qué herramientas expone este servidor?
- `tools/call` — invocar una herramienta por nombre con argumentos, recibir un resultado
- `resources/list` / `resources/read` — datos de solo lectura tipo archivo
- `prompts/list` / `prompts/get` — plantillas de prompt provistas por el servidor

Para nuestro harness nos importan `tools/list` y `tools/call`. El resto es opcional.

## El encaje arquitectónico

Observa nuestra interfaz `Tool` de nuevo:

```go
type Tool interface {
    Definition() api.ToolDef
    Execute(ctx context.Context, input string) (result string, isError bool)
}
```

Nada aquí dice "implementado en Go". Dice "dada una cadena de entrada, produce una cadena de resultado". Encaja perfectamente para un despacho remoto.

Así que el diseño es: **un struct wrapper por cada herramienta remota, registrado en el mismo registry `tool.Default` que las locales.** El bucle del agente no tiene idea de qué herramientas son locales y cuáles remotas. Desde el punto de vista del modelo, solo hay una lista plana de herramientas.

```
┌─────────────────────────────────────────────┐
│ registry tool.Default                       │
│                                             │
│  bash         (BashTool — Go local)         │
│  read_file    (ReadFileTool — Go local)     │
│  write_file   (WriteFileTool — Go local)    │
│  git_status   (MCPTool → servidor git MCP)  │
│  git_diff     (MCPTool → servidor git MCP)  │
│  query_db     (MCPTool → servidor postgres) │
└─────────────────────────────────────────────┘
```

## El cliente MCP

No escribes la maquinaria JSON-RPC a mano. Dos opciones:

1. **El SDK oficial de Go**, `github.com/modelcontextprotocol/go-sdk`. Maneja el transporte, el framing y el ciclo de vida.
2. **Los helpers de MCP incorporados en el SDK de Anthropic** — convenientes si ya usas el SDK de Anthropic, pero acoplan tu código MCP a ese proveedor.

Vamos con la opción 1 — mantiene MCP independiente del proveedor del LLM, consistente con la filosofía del capítulo 03.

Un wrapper mínimo:

```go
// internal/mcp/client.go
type Client struct {
    name string
    impl *mcp.Client
}

func NewStdioClient(ctx context.Context, name, command string, args ...string) (*Client, error) {
    transport := mcp.NewStdioTransport(command, args...)
    impl := mcp.NewClient(&mcp.Implementation{Name: "bettatech-harness", Version: "0.1"}, nil)
    if err := impl.Connect(ctx, transport); err != nil {
        return nil, err
    }
    return &Client{name: name, impl: impl}, nil
}

func (c *Client) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
    return c.impl.ListTools(ctx, nil)
}

func (c *Client) CallTool(ctx context.Context, name, input string) (string, bool, error) {
    res, err := c.impl.CallTool(ctx, &mcp.CallToolParams{
        Name:      name,
        Arguments: json.RawMessage(input),
    })
    if err != nil { return "", true, err }
    return res.Text(), res.IsError, nil
}

func (c *Client) Close() error { return c.impl.Close() }
```

Unas 30 líneas. El trabajo real está en el SDK; esto es solo un wrapper tipado que encaja con las convenciones de nuestra base de código.

## El puente: `MCPTool` implementa `Tool`

Por cada herramienta que el servidor expone, registramos un wrapper que satisface la interfaz local `Tool`:

```go
// internal/mcp/tool.go
type MCPTool struct {
    Client *Client
    def    api.ToolDef
}

func (t *MCPTool) Definition() api.ToolDef { return t.def }

func (t *MCPTool) Execute(ctx context.Context, input string) (string, bool) {
    out, isErr, err := t.Client.CallTool(ctx, t.def.Name, input)
    if err != nil { return err.Error(), true }
    return out, isErr
}
```

Ese es el puente. El bucle del agente, el registry, el flujo de aprobación — ninguno cambia. El modelo ve `git_status` en su lista de herramientas y la llama de la misma forma que llama a `read_file`.

## Cableado en `main.go`

Los servidores MCP son dependencias de runtime (tienes que lanzarlos), así que el registro es explícito. En lugar de fijar la lista en el código Go, el harness la carga desde un archivo JSON al arranque:

```go
// main.go
func setupMCP(ctx context.Context) []*mcp.Client {
    cfg, err := mcp.LoadConfig("mcp.json")
    if err != nil {
        fmt.Fprintf(os.Stderr, "mcp: config error: %v\n", err)
        return nil
    }
    return mcp.Register(ctx, cfg, tool.Default)
}
```

El formato del archivo de configuración:

```json
{
  "servers": [
    {"name": "git", "transport": "stdio", "command": "uvx", "args": ["mcp-server-git"]},
    {"name": "github", "transport": "http", "url": "https://api.githubcopilot.com/mcp/",
     "headers": {"Authorization": "Bearer ${GITHUB_TOKEN}"}}
  ]
}
```

Las referencias `${VAR}` en comandos, argumentos, URLs y valores de cabeceras se expanden vía `os.ExpandEnv` — las credenciales viven en el entorno de tu shell, no en un archivo que podrías subir al repositorio por error.

**El modo de falla es "omite el servidor y continúa".** Si `uvx` no está instalado, o el servidor MCP de git falla al lanzarse, o el JSON tiene una entrada malformada, el harness lo registra y continúa. El agente recibe menos herramientas pero igual funciona. Esto coincide con el patrón existente del harness — perder una herramienta no es un error fatal. Que el propio archivo de configuración no exista ni siquiera produce un mensaje — MCP es opcional, se activa explícitamente.

## Cuándo se lee la configuración

`setupMCP(ctx)` se ejecuta **una vez, al arranque**, y su posición en `main.go` importa por tres razones:

1. **Antes del registro de subagentes** — así el subconjunto curado de herramientas de un subagente puede incluir herramientas respaldadas por MCP (`tool.Default.Subset("read_file", "deepwiki_ask_question")`).
2. **Antes de la redirección del pipe de stdout** (capítulo 12) — los errores de conexión se imprimen en tu terminal real, no dentro del scrollback de la TUI donde son fáciles de pasar por alto.
3. **Antes de `program.Run()`** — la TUI se abre con la lista de herramientas completa ya poblada; `/tools` muestra todo desde el primer frame.

Dos consecuencias que vale la pena señalar:

- **La ruta es relativa al directorio de trabajo** — `mcp.json` en la carpeta desde donde ejecutaste `go run .`. No relativa al binario.
- **No hay recarga en caliente.** Si editas `mcp.json` tienes que reiniciar el harness. La llamada `tools/list` también ocurre una sola vez por servidor, así que un servidor que registre herramientas nuevas a mitad de sesión no las expondrá hasta el reinicio.

Si quieres extender esto — un comando de barra `/reload-mcp`, un observador sobre el archivo, una ruta de configuración por proyecto al estilo XDG — el punto de entrada es una sola función (`setupMCP`) y el resto del harness no nota la diferencia.

## Por qué prefijar los nombres con el del servidor

Dos servidores MCP podrían exponer ambos `read_file`. La herramienta de un servidor MCP podría ocultar a una local. El registry es un namespace plano; el segundo registro gana en silencio.

Prefijar con el nombre del servidor (`git_status`, `filesystem_read_file`) evita todo el problema. Trivial, pero fácil de olvidar hasta que estás depurando "¿por qué mi `read_file` devuelve un JSON raro?".

## La aprobación sigue funcionando

El modelo llama a `git_status`. El harness pide aprobación. Dices que sí. `registry.Execute` despacha a `MCPTool.Execute`, que hace el RPC. El resultado vuelve como un `tool_result`, igual que una herramienta local.

El control de permisos está en un solo lugar. No le importa que la llamada vaya a un subproceso. Esa es la recompensa de haber puesto la aprobación en la capa del harness allá en el capítulo 02.

## Ciclo de vida y limpieza

Los servidores MCP por stdio son subprocesos. Sobreviven entre turnos. Hay que apagarlos cuando el harness termina, o tendrás fugas de procesos — cada `go run .` seguido de Ctrl-D deja un `mcp-server-git` huérfano.

```go
// en main
clients := registerMCPServers(ctx)
defer func() {
    for _, c := range clients { _ = c.Close() }
}()
```

Para los transportes HTTP, "close" significa derribar la conexión de larga duración. Misma forma.

## Tropiezos

**Traducción de esquema.** Los esquemas de entrada de MCP son JSON Schema. Nuestro `ToolDef.InputSchema` también es JSON Schema (`map[string]any`). En su mayoría coinciden, pero los servidores MCP a veces usan `$ref` y otras características avanzadas que la API de Anthropic rechaza. Si el esquema de una herramienta falla la validación, omite esa herramienta en lugar del servidor entero.

**`tools/list` lento.** Algunos servidores MCP hacen trabajo real al inicio — abren bases de datos, obtienen credenciales, escanean sistemas de archivos. `ListTools` puede tardar segundos. Lanzar servidores serialmente en `main` bloquea el arranque del REPL. La respuesta de producción es lanzarlos en goroutines y registrar a medida que terminan; aceptamos la latencia serial para un proyecto de aprendizaje.

**Reutilizar el contexto equivocado.** Cada `CallTool` debería pasar el `ctx` del agente. Si el contexto del agente se cancela (Ctrl-C), las llamadas MCP en vuelo también se cancelan. No uses `context.Background()` dentro de `Execute` — perderías la cancelación.

**Confiar en herramientas remotas.** Un servidor MCP que no escribiste tú es código que no auditaste. El control de permisos está haciendo trabajo de seguridad real aquí — cada llamada igual pasa por `approve?`. No auto-apruebes herramientas MCP solo porque "parecen" de solo lectura.

## Ahora prueba

1. Instala uno de los servidores MCP estándar (`uvx mcp-server-git` o `npx @modelcontextprotocol/server-filesystem .`). Copia `mcp.example.json` a `mcp.json`, agrega una entrada para él, ejecuta el harness y escribe `/tools` para confirmar que aparecen las herramientas nuevas.
2. Hazle al agente una pregunta relacionada con git (`¿qué cambió desde main?`). Observa cómo elige `git_diff` de MCP en lugar de ejecutar `bash`. Compara la calidad del resultado.
3. Escribe un servidor MCP *minúsculo* tuyo. Hay SDKs para Python, TypeScript, Go. Expón una herramienta: `current_time`. Conéctala al harness. Ciclo completo: probablemente menos de una hora.

← [volver al índice](README.md)
