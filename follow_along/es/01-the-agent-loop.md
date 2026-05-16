# 01 · El bucle del agente

## Antes de empezar

Dos cosas con las que tropieza casi todo el mundo la primera vez:

1. **Los snippets de los capítulos 01–08 no se corresponden línea a línea con `main.go`.** Muestran cómo se *veía* el harness en ese momento de la construcción. El repo en HEAD ya tiene la misma lógica repartida en paquetes `internal/`, con una interfaz `Tool`, una TUI hecha con Bubble Tea y algunas capas más — la refactorización llega en el capítulo 10; los capítulos 03–09 van metiendo las piezas. Si abres `main.go` esperando que cuadre exactamente con lo que estás leyendo, vas a acabar mareado. Quédate con la *forma*; al final de cada capítulo hay un callout "En el repo actual" que te indica dónde vive ese código hoy.

2. **Si prefieres verlo correr antes de leer la prosa**, hazlo ahora: [`examples/minimal/main.go`](../../examples/minimal/main.go) tiene el agente entero en unas 130 líneas — sin abstracciones, sin TUI, solo el bucle y tres herramientas. `go run ./examples/minimal` y luego vuelves aquí para el porqué.

Aquí está el bucle general de cualquier aplicación que use agentes de IA a modo de arnés:

```
[tu entrada]
    │
    ▼
[agregar a messages]
    │
    ▼
[llamar al modelo] ─────┐
    │                   │
    ▼                   │
[¿hay tool_use?] ──no───┴──▶ [imprimir texto, volver al REPL]
    │
   sí
    │
    ▼
[ejecutar cada herramienta]
    │
    ▼
[agregar tool_results]
    │
    ▼
(volver a "llamar al modelo")
```

Eso es todo. El modelo decide qué hacer; el arnés ejecuta; el bucle continúa hasta que el modelo deja de pedir herramientas. Todo lo demás en este repositorio — proveedores, compactación, subagentes, la TUI — es una capa encima de este bucle.

## Un paréntesis: ¿qué es un REPL?

El diagrama de arriba es el bucle **interno**, un turno completo del agente. También hay un bucle **externo** que envuelve todo esto, llamado **REPL**: **R**ead–**E**val–**P**rint–**L**oop.

Si has escrito o jugado videojuegos, ya viste esta forma. Un **game loop** corre a 60 cuadros por segundo y hace las mismas cuatro cosas cada cuadro:

```
loop forever:
    leer inputs       (teclas, mouse, control)
    actualizar estado (física, IA, lo que cambió desde el último cuadro)
    renderizar        (dibujar el nuevo cuadro en pantalla)
    dormir hasta el siguiente cuadro
```

Un REPL es el mismo esqueleto, solo que ralentizado y dirigido por eventos. En lugar de correr 60 veces por segundo guiado por un temporizador, corre una vez por cada entrada tuya — disparado por lo que escribes, no por el reloj:

```
loop forever:
    leer entrada      (tu línea)
    eval              (hacer algo con ella)
    print             (mostrar el resultado)
    esperar la siguiente línea
```

Ya has usado REPLs con esta forma, probablemente sin ponerles nombre: el prompt `python3` de Python, la consola de JavaScript de un navegador, el propio `bash`. Mismo bucle, mismo propósito, distinto paso de "eval".

El bucle externo de nuestro harness es un REPL. El giro: "eval" significa "correr el bucle del agente sobre tu mensaje", no "ejecutar un trozo de código". Así que el harness tiene dos bucles anidados:

| Bucle | Lo dispara | Una iteración es |
|---|---|---|
| Externo (REPL) | Tus pulsaciones | Leer una línea → correr el agente sobre ella → imprimir → esperar la siguiente línea |
| Interno (bucle del agente) | Las elecciones del modelo | Mandar al modelo → si hay tool_use, ejecutar y agregar → repetir hasta terminar |

Mismo esqueleto que un game loop. El bucle externo es un *tick de juego sobre tu entrada*; el interno es el *paso de actualización* (con el modelo+herramientas haciendo las veces de física+IA). Cuando leas "el bucle" en capítulos posteriores, el contexto te dirá cuál — en su mayoría es el bucle del agente, porque ahí vive el estado interesante.

## El vocabulario, en un ejemplo

El resto del capítulo — y los doce siguientes — se apoya en un puñado de términos de la API de Anthropic. Si nunca los has visto, aquí los tienes todos en una sola ida y vuelta.

Mandamos:

```json
{
  "model": "claude-opus-4-7",
  "max_tokens": 8192,
  "system": "Eres un asistente de programación.",
  "tools": [
    {"name": "read_file",
     "description": "Lee un archivo en el path dado.",
     "input_schema": {
       "type": "object",
       "properties": {"path": {"type": "string"}},
       "required": ["path"]
     }}
  ],
  "messages": [
    {"role": "user", "content": "¿qué hay en main.go?"}
  ]
}
```

Recibimos:

```json
{
  "content": [
    {"type": "tool_use",
     "id": "toolu_abc",
     "name": "read_file",
     "input": {"path": "main.go"}}
  ],
  "stop_reason": "tool_use"
}
```

Ese es todo el vocabulario:

- **`messages`** es la conversación hasta ese momento. Vamos añadiéndole turnos; la API no guarda estado, así que el cliente arrastra el historial entero (capítulo 06).
- **`tools`** es la lista de cosas que el modelo puede llamar. Cada herramienta lleva un JSON Schema describiendo sus entradas — JSON Schema es el formato estándar para tipar las entradas de las herramientas de un LLM.
- **bloques de `content`** es lo que devuelve el modelo: `text` cuando tiene algo que decir, `tool_use` cuando quiere que el harness ejecute algo.
- **`stop_reason`** le indica al bucle qué hacer a continuación: `tool_use` significa "ejecuta esas herramientas y vuélveme a preguntar"; `end_turn` significa "imprime y devuelve el control al usuario".
- **`max_tokens`** pone un techo al tamaño de la *salida*, contado en tokens (~4 caracteres de texto en inglés por token).

Si has usado la API de OpenAI, la forma es prácticamente la misma — distintos nombres para la misma idea (`tool_calls` en lugar de `tool_use`, `finish_reason` en lugar de `stop_reason`). La interfaz de provider del capítulo 03 es donde tapamos esa diferencia.

## Qué pasa en un turno

El diagrama del principio del capítulo muestra la imagen completa, pero es más fácil de digerir en dos pasadas — primero sin herramientas, después con ellas.

### Pasada 1: sin herramientas, es solo un cliente de chat

Imagina que el modelo no tiene herramientas. El bucle interno colapsa a:

1. Escribes un mensaje.
2. Lo agregamos a una lista corriente de mensajes.
3. Mandamos la lista completa al modelo.
4. El modelo devuelve una respuesta de texto.
5. La imprimimos.
6. Esperamos tu siguiente entrada.

Eso es un programa que funciona. Conversa. Podrías preguntar "¿cuál es la capital de Francia?" y obtener "París." Pero no puede *hacer* nada — no tiene manos. Desde tu perspectiva, se siente como un wrapper alrededor de la API.

### Pasada 2: las herramientas convierten el chat en un agente

Para darle manos al modelo, agregamos **herramientas** — operaciones con nombre que el harness sabe cómo ejecutar, como `bash`, `read_file`, `write_file`. Cada herramienta tiene un schema JSON que describe sus entradas. Los schemas se mandan junto con cada petición al modelo para que sepa qué está disponible.

Ahora el modelo tiene dos tipos de respuesta que puede devolver:

| Forma de la respuesta | Qué significa | Qué hacemos |
|---|---|---|
| Texto plano | "Aquí está mi respuesta." | Imprimirlo, esperar tu siguiente mensaje |
| Una **petición de tool call** | "Antes de responder, por favor corre `read_file` con `path: main.go` y dime qué contiene." | Ejecutar la herramienta, mandar el resultado de vuelta, llamar al modelo otra vez |

La segunda rama es donde entra el **bucle**. Cuando el modelo pide una herramienta, el harness:

1. Ejecuta la herramienta localmente (p.ej. corre `read_file`, captura la salida).
2. Agrega un **tool result** al slice de mensajes.
3. Manda la conversación ya más larga de vuelta al modelo.
4. El modelo ve el resultado y decide qué hacer a continuación — responder o llamar a otra herramienta.

> **Cómo se ve realmente una herramienta en el repo.** Aquí está la herramienta `read_file` viva — [`internal/tool/readfile.go`](../internal/tool/readfile.go) en este codebase:
>
> ```go
> type ReadFileTool struct{}
>
> func (ReadFileTool) Definition() api.ToolDef {
>     return api.ToolDef{
>         Name:        "read_file",
>         Description: "Read the contents of a file at the given path.",
>         InputSchema: map[string]any{
>             "path": map[string]any{
>                 "type":        "string",
>                 "description": "Path to the file to read.",
>             },
>         },
>         Required: []string{"path"},
>     }
> }
>
> func (ReadFileTool) Execute(_ context.Context, rawInput string) (string, bool) {
>     var in struct{ Path string `json:"path"` }
>     json.Unmarshal([]byte(rawInput), &in)
>     data, err := os.ReadFile(in.Path)
>     if err != nil { return err.Error(), true }
>     return string(data), false
> }
> ```
>
> Un struct que implementa dos métodos. `Definition` devuelve el schema que ve el modelo. `Execute` hace el trabajo y devuelve `(result string, isError bool)`. El capítulo 09 cubre por qué las herramientas terminan con esta forma; el resto de este capítulo muestra un precursor más simple donde las tres herramientas se despachan con un switch.

Ese último paso es recursivo de espíritu pero iterativo en el código. Un solo mensaje tuyo puede disparar una llamada al modelo ("París.") o veinte (leer tres archivos, correr dos comandos bash, y finalmente sintetizar una respuesta). El modelo elige; el harness obedece. Seguimos iterando mientras el modelo siga pidiendo herramientas, y paramos en el momento en que devuelve texto plano.

### Juntando las piezas

Así que el bucle interno tiene exactamente dos salidas:

- El modelo devuelve texto → imprimirlo, volver al REPL, esperar tu siguiente mensaje.
- El modelo devuelve una tool call → ejecutar la herramienta, agregar el resultado, preguntar otra vez.

Esa es la imagen conceptual completa. Todo lo que sigue en este capítulo es detalle a nivel de cables: cómo se ven en realidad la petición y la respuesta, qué herramientas exponemos y cómo estructurar el código Go.

## Un turno, paso a paso

Escribes `list the files here`. Esta es la secuencia exacta que corre — once pasos para una entrada del usuario, porque el modelo decide que necesita una herramienta primero:

```
1.  El REPL lee tu línea.

2.  El REPL agrega a messages:
      [{role: user, content: "list the files here"}]

3.  El bucle del agente hace POST a api.anthropic.com/v1/messages con
      {system, tools, messages}

4.  Claude responde:
      content:     [{type: tool_use, id: "toolu_01",
                     name: "bash", input: {"command": "ls"}}]
      stop_reason: "tool_use"

5.  El bucle agrega el turno del asistente a messages y recorre su content:
      - Ve un bloque tool_use.
      - Imprime  [tool] bash {"command":"ls"}
      - Pregunta: approve? [y/n]

6.  Escribes y.

7.  El harness corre  sh -c "ls" , captura stdout:
      "main.go\nREADME.md\n..."

8.  El bucle agrega un tool_result a messages:
      {role: user, content: [{type: tool_result,
                              tool_use_id: "toolu_01",
                              content: "main.go\nREADME.md\n...",
                              is_error: false}]}

9.  stop_reason era tool_use → el bucle itera. POST a Claude otra vez.

10. Claude responde:
       content:     [{type: text, text: "Aquí están los archivos: ..."}]
       stop_reason: "end_turn"

11. El bucle recorre content → imprime el texto. stop_reason ≠ tool_use →
    vuelve al REPL, espera tu siguiente línea.
```

Cada capítulo posterior es una capa más sobre este trazado. La compactación (capítulo 07) recorta `messages` entre los pasos 2 y 3. Las políticas de permisos (capítulo 02) deciden qué pasa en el paso 6. Los subagentes (capítulo 11) cambian el paso 7 por un bucle de agente recursivo, y las herramientas MCP (capítulo 14) lo cambian por una llamada JSON-RPC a otro proceso. La forma del trazado no cambia — lo que cambia es lo que hace cada paso.

## El contrato con el modelo

Una sola llamada a la API Messages de Anthropic tiene esta forma:

- **Entrada:** un prompt `system`, un array de `messages` y un array opcional de `tools` (cada uno con un schema JSON para su entrada).
- **Salida:** una respuesta con bloques de `content` (texto y/o `tool_use`) y un `stop_reason`.

El `stop_reason` es lo que dirige el bucle:

| Stop reason | Qué significa | Qué hacemos |
|---|---|---|
| `end_turn` | El modelo terminó | Imprimir texto, volver al REPL |
| `tool_use` | El modelo quiere llamar herramientas | Ejecutarlas, agregar resultados, llamar otra vez |

Hay otros stop reasons (`max_tokens`, `refusal`, etc.) — los manejamos tratando cualquier cosa que no sea `tool_use` como "terminamos con este turno".

## Eligiendo la superficie de herramientas

Le podríamos haber dado al modelo **una sola herramienta bash** y darlo por hecho — `bash` puede leer archivos, escribir archivos, hacer de todo. O le podríamos haber dado decenas de herramientas especializadas.

Elegimos tres:

- `bash` — para todo lo que no tenemos una herramienta dedicada
- `read_file` — explícita, le da al harness un gancho para hacer chequeos de staleness después si queremos
- `write_file` — igual, además es fácil de exponer en la UI como "el modelo está escribiendo este archivo"

**La razón por la que ascendimos las operaciones de archivos a herramientas dedicadas** no es que sean necesarias — es que son **controlables**. Una herramienta `read_file` le da al harness una costura específica por acción para registrar, auditar o restringir. Bash nos da solo un string opaco de comando. La aprobación (próximo capítulo) tiene sentido por herramienta; no lo tiene si solo tienes bash.

Esta es la primera vez que importa la división harness/modelo: al modelo le da igual que le des una herramienta o tres. La forma de tu superficie de herramientas es una decisión del harness.

## El bucle básico en Go

El esqueleto, más o menos:

```go
func main() {
    client := anthropic.NewClient()
    var messages []anthropic.MessageParam

    scanner := bufio.NewScanner(os.Stdin)
    for {
        fmt.Print("> ")
        if !scanner.Scan() { return }
        userInput := scanner.Text()
        if userInput == "" { continue }

        messages = append(messages, anthropic.NewUserMessage(
            anthropic.NewTextBlock(userInput),
        ))
        messages = agentLoop(messages)
    }
}

func agentLoop(messages []anthropic.MessageParam) []anthropic.MessageParam {
    for {
        resp, _ := client.Messages.New(ctx, anthropic.MessageNewParams{
            Model:     anthropic.ModelClaudeOpus4_7,
            MaxTokens: 8192,
            System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
            Messages:  messages,
            Tools:     tools,
        })
        messages = append(messages, resp.ToParam()) // assistant turn

        var toolResults []anthropic.ContentBlockParamUnion
        for _, block := range resp.Content {
            switch v := block.AsAny().(type) {
            case anthropic.TextBlock:
                fmt.Println(v.Text)
            case anthropic.ToolUseBlock:
                result, isErr := executeTool(v.Name, v.JSON.Input.Raw())
                toolResults = append(toolResults,
                    anthropic.NewToolResultBlock(v.ID, result, isErr))
            }
        }

        if resp.StopReason != anthropic.StopReasonToolUse {
            return messages
        }
        messages = append(messages, anthropic.NewUserMessage(toolResults...))
    }
}
```

El REPL completo es el bucle externo; el bucle del agente es el interno. Están anidados a propósito: el REPL es una conversación, y cada turno de la conversación es potencialmente múltiples viajes de ida y vuelta entre modelo y herramientas.

## El switch de `executeTool`

El dispatcher de herramientas es un switch sobre el nombre de la herramienta. Cada caso decodifica el JSON de entrada, hace el trabajo y devuelve un string + una flag de error:

```go
func executeTool(name, rawInput string) (string, bool) {
    fmt.Printf("[tool] %s %s\n", name, rawInput)
    switch name {
    case "bash":
        var in struct{ Command string `json:"command"` }
        json.Unmarshal([]byte(rawInput), &in)
        out, err := exec.Command("sh", "-c", in.Command).CombinedOutput()
        if err != nil {
            return fmt.Sprintf("%s\n[exit error: %v]", out, err), true
        }
        return string(out), false
    case "read_file":
        // similar
    case "write_file":
        // similar
    default:
        return fmt.Sprintf("unknown tool: %s", name), true
    }
}
```

Tres cosas que vale la pena señalar:

1. **La función nunca devuelve un `error` de Go.** Los fallos se convierten en *strings que el modelo lee*. Si `read_file` falla porque el path no existe, el tool result es `"no such file or directory"` con `is_error: true`. El modelo lo ve, se disculpa o prueba un path distinto, y continúa. Si devolviéramos un error de Go y crasheáramos el bucle, el modelo no tendría manera de recuperarse.

2. **Hay un print arriba.** `fmt.Printf("[tool] %s %s\n", name, rawInput)` — pura observabilidad. Te deja ver las acciones del agente conforme ocurren. No es load-bearing.

3. **El caso default es defensivo.** Los modelos a veces alucinan nombres de herramientas. Devolver un resultado de error (en lugar de hacer panic) le permite al modelo autocorregirse.

## Tropiezos que tuvimos

**Olvidar `resp.ToParam()`.** La respuesta del modelo tiene que ser agregada de vuelta a `messages` antes de la siguiente iteración del bucle — si no, el modelo no tiene idea de qué dijo en el turno anterior. El `.ToParam()` del SDK convierte la respuesta a la forma correcta. Es fácil saltárselo la primera vez que escribes esto.

**IDs de tool result.** Cada bloque `tool_use` tiene un `id`; cada `tool_result` que mandas de vuelta tiene que referenciar ese id vía `tool_use_id`. Si no coinciden, la API devuelve un 400 hablando de un tool result huérfano. El `NewToolResultBlock(id, content, isErr)` del SDK te arma el bloque.

**Terminación del bucle.** Si chequeas el campo equivocado (p.ej., `stop_reason == "end_turn"` en lugar de `!= "tool_use"`), o vas a iterar para siempre o no vas a iterar nunca. El chequeo confiable es "¿la respuesta contiene algún bloque `tool_use`?" — equivalente a `stop_reason == "tool_use"`.

> **En el repo actual.** El bucle del agente vive en [`internal/agent/agent.go`](../internal/agent/agent.go) como el método `(*Agent).loop` (el capítulo 11 cubre por qué terminó siendo un método sobre un struct). El wrapper `executeTool` en la capa del harness está en [`main.go`](../main.go). El dispatch de switch único de arriba evolucionó a un `tool.Registry` — el capítulo 09 cubre ese refactor.

## Ahora prueba

1. **Instrumenta el bucle.** Abre [`examples/minimal/main.go`](../../examples/minimal/main.go) y añade un `log.Printf` antes de cada paso del trazado de arriba: justo antes de `client.Messages.New` (paso 3), después de recibir la respuesta (paso 4) imprimiendo `stop_reason` y los tipos de bloque, después de cada `executeTool` (paso 7), y justo antes de retornar al REPL (paso 11). Corre `go run ./examples/minimal`, pídele `list the files here`, y compara los logs con los 11 pasos. Bonus: imprime `len(messages)` en cada paso — verás exactamente cómo crece.
2. Corre el agente y pídele `list the files here`. Mira pasar volando los prints `[tool] bash ...`.
3. Pídele `write a hello.txt with a haiku in it`. Dos tool calls en un turno — observa el bucle.
4. Pídele `read the file /does/not/exist`. El modelo recibe un string de error y o te lo reporta o prueba un path distinto. Este es el contrato de "errores como tool results" en acción.

Siguiente: [02 · El control de permisos](02-the-permission-gate.md).
