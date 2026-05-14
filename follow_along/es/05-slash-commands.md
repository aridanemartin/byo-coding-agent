# 05 · Comandos de barra

El REPL que tenemos hasta ahora hace una sola cosa: toma una línea de entrada y la manda al modelo. No hay forma de preguntar "¿qué modelo estoy usando?", limpiar la conversación, cambiar de backend, o siquiera salir limpiamente sin Ctrl-D.

Es hora de una paleta de comandos. Toda línea que empieza con `/` se intercepta antes de llegar al modelo.

## El dispatcher

El patrón es un registry minúsculo — un map de nombre a handler — más una función de ruteo en el REPL:

```go
type command struct {
    description string
    usage       string
    run         func(args string)
}

var commands = map[string]command{}

func init() {
    commands["help"]    = command{description: "show available commands",        run: cmdHelp}
    commands["model"]   = command{description: "show or change the model",       run: cmdModel}
    commands["clear"]   = command{description: "clear conversation history",     run: cmdClear}
    commands["tools"]   = command{description: "list available tools",           run: cmdTools}
    commands["exit"]    = command{description: "exit the harness",               run: cmdExit}
}

func runCommand(line string) bool {
    if !strings.HasPrefix(line, "/") { return false }
    parts := strings.SplitN(strings.TrimPrefix(line, "/"), " ", 2)
    name := parts[0]
    args := ""
    if len(parts) > 1 { args = strings.TrimSpace(parts[1]) }
    c, ok := commands[name]
    if !ok {
        fmt.Printf("unknown command: /%s (try /help)\n", name)
        return true
    }
    c.run(args)
    return true
}
```

El REPL entonces se vuelve:

```go
userInput := strings.TrimSpace(scanner.Text())
if userInput == "" { continue }
if runCommand(userInput) { continue }   // ← línea nueva
// si no, mandar al modelo
```

Tres cosas para notar:

1. **`runCommand` devuelve `bool`** — si la línea fue manejada o no. La tarea del REPL es decidir qué hacer con esa línea; los comandos y "mandar al modelo" son dos casos.
2. **Los comandos desconocidos imprimen un error pero devuelven `true`.** De lo contrario, escribir `/asdf` se mandaría al modelo, lo cual confunde — ¿fue un typo o el modelo vio un mensaje con prefijo de barra?
3. **El estado para los comandos vive a nivel de paquete.** `provider`, `messages`, `compactor` (después) son todos de paquete. Los comandos los mutan directamente. REPL de una sola goroutine significa que no hay locking.

## Subiendo el estado a globales

Antes de este capítulo, `messages` era una variable local en `main`. Para dejar que los comandos la muten, la subimos (y a `provider`) al scope de paquete:

```go
var (
    provider Provider
    messages []Message
)
```

`agentLoop` pierde su parámetro `messages` y ahora muta el global. El REPL pierde su patrón de retornar-y-reasignar.

Esta es una de esas decisiones donde "buen estilo Go" no concuerda con "lo que es fácil de enseñar". Las arquitecturas estrictas pasan el estado a través de structs y métodos. Para un REPL chico con una sola goroutine y sin tests, los globales son más simples y claros. Vamos a revisar esto cuando agreguemos subagentes (capítulo 11) y necesitemos múltiples instancias de `Agent`.

## `/model`: comandos parametrizados

El comando interesante es `/model`. Sin args muestra el modelo actual y lista sugerencias. Con un arg, setea el modelo:

```go
var knownModels = []string{
    "claude-opus-4-7",
    "claude-opus-4-6",
    "claude-sonnet-4-6",
    "claude-haiku-4-5",
}

func cmdModel(args string) {
    if args == "" {
        fmt.Printf("current: %s\n", provider.Model())
        fmt.Println("suggestions:")
        for _, m := range knownModels { fmt.Printf("  %s\n", m) }
        return
    }
    provider.SetModel(args)
    fmt.Printf("model: %s\n", args)
}
```

Dos notas de diseño:

1. **No validamos el id del modelo.** `/model claude-foo` tiene éxito; la *siguiente* llamada a la API falla con un 404. Está bien — el error se propaga por el mismo camino que cualquier otro error de la API.
2. **`Provider.Model()` / `Provider.SetModel(name)` existen por esto** (capítulo 03). Una concesión a las preocupaciones específicas del proveedor, pero todo proveedor de LLM tiene noción de modelo, así que generaliza.

## `/clear`: lo trivial que pueden ser las operaciones de estado

El slice `messages` es la historia *entera* de la conversación. El modelo es sin estado. Así que:

```go
func cmdClear(_ string) {
    messages = messages[:0]
    fmt.Println("conversation cleared")
}
```

Una línea. El modelo no tiene memoria; limpiar nuestro slice local ES limpiar la conversación. Vamos a explorar por qué funciona esto en el capítulo 06.

## `/help`: descubriendo qué hay disponible

Un error común al hacer BYO es sobre-ingeniar la ayuda. Solo lista los comandos, alfabetizados, con descripciones:

```go
func cmdHelp(_ string) {
    names := make([]string, 0, len(commands))
    for n := range commands { names = append(names, n) }
    sort.Strings(names)
    for _, n := range names {
        c := commands[n]
        display := "/" + n
        if c.usage != "" { display = c.usage }
        fmt.Printf("  %-22s %s\n", display, c.description)
    }
}
```

El campo `usage` es para comandos que reciben args. `/model` se muestra como `/model [name]`; `/clear` se muestra solo como `/clear`. Detalle minúsculo; importa mucho cuando tienes diez comandos.

## Tropiezos

**Mutar los headers de slice vs el almacenamiento subyacente.** `messages = messages[:0]` mantiene el array subyacente (bueno para reutilizar memoria) y resetea el length. `messages = nil` también está bien. `messages = []Message{}` está bien pero asigna. `len(messages) = 0` no existe.

**`SplitN` en lugar de `Split`.** Usamos `strings.SplitN(s, " ", 2)` para que los args con espacios (`/model claude-opus-4-7`) sobrevivan como un solo string. Un `Split` plano dividiría en cada espacio y rompería comandos como `/some-command arg with spaces`.

**¿Dónde viven los comandos en términos de paquete?** Los dejamos en `main.go` (bueno, `commands.go` en el mismo paquete que `main`) a propósito. Los comandos tocan *todos* los puntos de extensión — provider, messages, tools, compaction. Ponerlos en su propio paquete requeriría o bien pasarles todo el estado, o hacer todo global *y* exportado. Mejor mantener la capa de integración arriba.

> **En el repo actual.** Todos los comandos viven en [`commands.go`](../commands.go). El patrón de registry (un `map[string]command`, un dispatcher `runCommand`, una función `cmdX` por comando) no cambia desde este capítulo. Para el capítulo 11 ya agregamos `/compact`, `/verbose`, `/subagents`; cada uno es una entrada nueva en `init()` y una función `cmdX` nueva. La forma escala linealmente.

## Ahora prueba

1. Agrega un comando `/quit` como alias para `/exit`. El enfoque ingenuo es duplicar la entrada; uno más limpio es hacer que los aliases sean ciudadanos de primera clase. ¿Cuál se siente bien?
2. Prueba `/clear` después de varios turnos. Después pregúntale al modelo "¿qué acabamos de discutir?" Verifica que no tiene memoria.
3. Esboza un comando `/save` que escriba el slice `messages` actual a un archivo JSON. Esboza un `/load` que lo lea de vuelta. Este es un camino a la persistencia de la conversación; el capítulo 13 menciona otros.

Siguiente: [06 · El estado de la conversación](06-conversation-state.md).
