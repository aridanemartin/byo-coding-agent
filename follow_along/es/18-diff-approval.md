# 18 · Aprobación con diff para escrituras

El capítulo 02 introdujo el control de permisos: cada tool call recibe `approve? [y/n]`. Eso está bien para `bash ls`. Es aterrador para `write_file` — lo que estás aprobando es "el agente quiere escribir *algo*", y no puedes ver qué hasta que dices que sí y lees el archivo después.

Este capítulo trata de cerrar esa brecha. Antes de que cualquier `write_file` realmente toque el disco, el harness te muestra el diff *exacto* que el agente propone, en un modal por el que puedes desplazarte. Un único y/n sobre el conjunto — sin aprobación hunk-por-hunk, sin editor — solo la información suficiente para que el y/n signifique algo.

## La intuición que lo hace posible

El modelo nunca ve un diff. Manda un bloque `tool_use`:

```json
{
  "type": "tool_use",
  "name": "write_file",
  "input": { "path": "main.go", "content": "package main\n\nfunc main() { …" }
}
```

Ese bloque llega a nuestro harness *antes* de que nada se ejecute. El bucle del agente tiene todos los argumentos que el modelo quiere usar, completos, en memoria. La ruta. El contenido propuesto entero. Solo tenemos que leer el archivo *actual* del disco y calcular la diferencia. El diff es una capa de UX entre la intención del modelo y los bytes del disco — invisible al modelo, enormemente útil al usuario.

Esta es una de las decisiones load-bearing de la ingeniería de harness en general: **todas las acciones del modelo pasan por nosotros.** Cualquier cosa que podamos computar a partir de los argumentos de la tool — una vista previa del diff, un resumen de dry-run, una estimación del coste en tokens — se la podemos mostrar al usuario *antes* de que la acción suceda. La aprobación con diff es la instancia más útil, pero el patrón generaliza (mira "Dónde más funciona esto" abajo).

## Tres piezas, un flujo

### 1. Construir el diff

`internal/agent/diff.go` tiene un único trabajo exportado: tomar el JSON crudo que el modelo envió para una llamada `write_file` y producir una cadena de diff unificado que el usuario pueda leer.

```go
func buildWriteDiff(rawInput string) string {
    var in struct{ Path, Content string }
    json.Unmarshal([]byte(rawInput), &in)

    existing, err := os.ReadFile(in.Path)
    if err != nil {
        return synthesizeNewFileDiff(in.Path, in.Content)
    }
    diff := difflib.UnifiedDiff{
        A:        difflib.SplitLines(string(existing)),
        B:        difflib.SplitLines(in.Content),
        FromFile: in.Path + " (current)",
        ToFile:   in.Path + " (proposed)",
        Context:  3,
    }
    text, _ := difflib.GetUnifiedDiffString(diff)
    return text
}
```

Tres casos reciben manejo especial:

- **El archivo existe y el contenido difiere.** Diff unificado estándar, tres líneas de contexto, marcadores `+`/`-` por línea.
- **El archivo aún no existe.** Prelude sintetizado (`--- /dev/null` / `+++ path (new file)`) para que todo el cuerpo se renderice como adiciones. Sin esto, `go-difflib` solo emitiría el contenido sin marcadores y el modal se vería como texto plano, no como un diff.
- **Contenido idéntico.** El modelo ocasionalmente pide "escribir" un archivo que es byte-por-byte lo que ya está ahí. Devuelve un marcador "(no changes)" — el modal aún abre con un mensaje claro en lugar de caer al prompt genérico `approve?` y confundir al usuario sobre qué acaba de denegar.

### 2. Conectarlo por el canal de aprobación

`Agent.Confirm` antes era `func(prompt string) bool`. El cambio es un parámetro:

```go
Confirm func(prompt, detail string) bool
```

`detail` es contenido opcional en formato largo. Para cualquier tool excepto `write_file`, el agente pasa `""` y el flujo viejo se preserva. Para `write_file`, el agente llama a `buildWriteDiff(rawInput)` y pasa el resultado:

```go
prompt, detail := "approve?", ""
if name == "write_file" {
    if d := buildWriteDiff(rawInput); d != "" {
        detail = d
        prompt = "approve write to " + path + "?"
    }
}
if a.Confirm != nil && !a.Confirm(prompt, detail) {
    return "user denied this tool call", true
}
```

Es un cambio de signatura controlado — hay un solo caller en el codebase (el cableado de la TUI en `main.go`), así que lo actualizamos y seguimos. La compatibilidad hacia atrás sería una interfaz con un opcional por defecto vacío, pero para una API interna con un solo caller no merece la pena la abstracción.

### 3. Renderizar el modal

El mensaje `ApprovalRequest` de la TUI gana un campo `Detail string`. Cuando no está vacío, el handler:

1. Pone el estado del harness en `stateAwaitingApproval` (igual que antes).
2. Llama a `layout()` para redimensionar el viewport `debugView` existente a las dimensiones del modal.
3. Pasa el diff por `HighlightPayload(detail, width)` y lo establece como contenido del viewport.

`HighlightPayload` ya detectaba JSON (capítulo 16). Le enseñamos a reconocer también diffs unificados — un prefijo `--- ` basta — y a pasarlo al lexer `diff` de Chroma. A partir de ahí, las líneas `+` salen en verde, las `-` en rojo, los headers `@@` en otro color, todo gratis.

`View()` chequea el estado approval-con-detail y enruta a `viewApprovalModal()`, estructuralmente idéntico al modal de detalle de debug del capítulo 12: centrado, full-screen, título + separador + cuerpo desplazable + línea de hint. Las únicas diferencias son un borde amarillo (en lugar de cian) para que sea inconfundiblemente un estado "tienes que decidir", y una línea de hint distinta (`y / n` en lugar de `esc / tab`).

Cuando el usuario pulsa y/n, la respuesta vuelve por el canal de reply, el estado vuelve a `stateRunning`, y `layout()` corre otra vez para restaurar el split normal panel/viewport antes del siguiente render.

## Caso límite: qué cuenta como "write_file"

La detección en `agent.executeTool` es una comparación de cadena: `if name == "write_file"`. Ese es el nombre de la tool local. **No se dispara para tools de escritura respaldadas por MCP** como `filesystem_write_file` (si cableas el [servidor MCP de filesystem](14-mcp-support.md)) ni para cualquier otra tool de un servidor externo que escriba archivos.

Esa es una elección de scope deliberada: el harness puede construir un diff para `write_file` porque conoce su esquema de argumentos (`path` + `content`). Para tools de MCP tendríamos que inspeccionar sus esquemas en tiempo de ejecución, parsear argumentos con forma de `path`, y confiar en que la semántica del servidor realmente corresponde a "sobrescribir un archivo". Es factible pero más grande.

La versión conservadora que enviamos: solo el `write_file` local recibe el tratamiento del diff; las tools de escritura por MCP siguen pasando por el prompt `approve?` plano sin preview. Los usuarios que quieran preview para escrituras vía MCP pueden o bien preferir la tool local o esperar a una generalización que introspeccione esquemas.

## Dónde más funciona este patrón

La aprobación con diff es una instancia de un patrón general: **sintetiza una vista previa a partir de los argumentos de la tool y muéstrasela antes de ejecutar.** Otras tools donde esto funcionaría en este harness:

| Tool | Vista previa |
|---|---|
| `bash` | Ejecutar con el flag `-n` del shell (solo chequeo de sintaxis), o con un estilo `--dry-run` si el comando lo tiene (`rm -i`, `rsync -n`). |
| `delegate_research` | Listar el subconjunto curado de tools al que tendrá acceso el subagente + el system prompt bajo el que correrá. |
| Cualquier tool MCP | La descripción JSON-Schema de la tool + los argumentos. Menos accionable que un diff, más accionable que nada. |

El patrón es siempre el mismo: en `Agent.executeTool`, después de que el modelo emite el bloque `tool_use` pero antes de que despachemos, tenemos una oportunidad para materializar lo que está a punto de pasar. Cuanto más rica sea la vista previa, más significativo es el y/n del usuario.

## Tropiezos

**El diff captura el archivo tal como estaba cuando el modelo decidió.** Si editas el archivo en otra terminal entre el `tool_use` del modelo y tu y/n, el contenido propuesto se aplica sobre tus ediciones, no sobre lo que muestra el diff. No hay actualización en vivo — el diff es un snapshot en el momento de la aprobación. Para una sesión interactiva de programación esto casi siempre está bien; para batch / automatización, tenlo en cuenta.

**Los archivos binarios se renderizan como basura.** `go-difflib` está orientado a líneas y asume texto. Pedirle al modelo que escriba un PNG mostraría un diff de bytes codificados ininteligibles. No intentamos detectar contenido binario; en la práctica el modelo muy rara vez pide escribir binarios desde dentro de este harness.

**Los archivos grandes inflan el payload del diff.** Un archivo de 50 KB con una línea cambiada todavía produce un diff pequeño (solo el hunk cambiado + 3 líneas de contexto). Un reescribido de 50 KB produce un diff de 50 KB. El modal hace scroll, pero el usuario tiene que leer más. Sin resumen automático; lo dejamos visible.

**Discordancias de codificación.** Si el archivo en disco es UTF-16 o cualquier codificación no-UTF-8, `os.ReadFile` devuelve los bytes textuales y `difflib` divide por líneas en `\n` literalmente. El diff se va a ver raro pero no va a crashear. La tool `write_file` en sí escribe UTF-8 incondicionalmente — si el modelo está "editando" un archivo UTF-16, aprobar la escritura *va a* destrozar su codificación.

**El modelo nunca se entera del diff.** Tanto si apruebas como si deniegas, el modelo recibe de vuelta la cadena estándar del tool result — o el éxito/contenido de `write_file` o `"user denied this tool call"`. Nunca ve el diff en sí. Esto es intencional (el límite de statelessness del capítulo 06) pero vale la pena saberlo: explicarle al modelo "rechacé esto por X razón" requiere que tú lo escribas en el siguiente turno.

> **En el repositorio actual.** El helper del diff es [`internal/agent/diff.go`](../../internal/agent/diff.go). La signatura de Confirm vive en [`internal/agent/agent.go`](../../internal/agent/agent.go). El renderer del modal y los cambios en el ruteo de mensajes están en [`internal/ui/program.go`](../../internal/ui/program.go) — busca `viewApprovalModal` y `approvalDetail`. El lexer de diff de Chroma se conecta en [`internal/ui/highlight.go`](../../internal/ui/highlight.go) vía el helper `detectLanguage`.

## Ahora prueba

1. Pídele al agente que cree un archivo pequeño (`write a haiku.txt with a haiku`). Observa el modal mostrando `--- /dev/null` / `+++` y todas las adiciones en verde. Aprueba. Compara el archivo resultante con el diff.
2. Pídele que modifique el mismo archivo (`rewrite haiku.txt with a different haiku`). El modal ahora muestra el diff unificado contra el contenido existente — líneas `-` para lo que sale, líneas `+` para lo que entra, líneas de contexto sin cambiar.
3. Deniega una escritura (presiona `n`). El agente recibe de vuelta `"user denied this tool call"` — lee su siguiente respuesta de texto para ver cómo reacciona. Normalmente te va a preguntar qué prefieres.
4. Prueba un refactor: pídele al agente que renombre una función en un archivo. Nota que el modal hace obvio si el renombre es correcto *antes* de que toque disco — ese es el punto entero.

← [volver al índice](README.md)
