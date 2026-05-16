# 02 · El control de permisos

El agente que tenemos hasta ahora va a ejecutar contentísimo cualquier comando de shell que produzca el modelo. Eso está bien para `ls`. No está bien para `rm -rf`. Antes de seguir, necesitamos una forma de controlar las operaciones destructivas.

## La decisión: preguntar cada vez

Hay aproximadamente tres lugares donde puedes meter la lógica de aprobación:

| Dónde | Cómo se ve | Tradeoff |
|---|---|---|
| **Dentro de la herramienta** | El propio `bash` pregunta "¿estás seguro?" | Cada herramienta tiene que saber que hay una persona a la que preguntarle; acopla responsabilidades. No compone. |
| **En la capa del harness** | El bucle del agente pregunta antes de llamar a la herramienta | Un solo lugar, UX consistente, no necesita cooperación de la herramienta. ✓ |
| **En la capa del modelo** | Decirle al modelo "siempre pregunta primero" | Poco confiable. El modelo debe ayudar, no hacer de guardián. |

Lo ponemos en la capa del harness. En concreto, `executeTool` llama a `confirm("approve?")` entre el print `[tool] …` y el dispatch real.

```go
func executeTool(name, rawInput string) (string, bool) {
    fmt.Printf("[tool] %s %s\n", name, rawInput)
    if !confirm("approve?") {
        return "user denied this tool call", true
    }
    // … dispatch
}
```

## La función `confirm`

Lee una línea de stdin; cualquier cosa distinta de `y` / `yes` es un no.

```go
func confirm(prompt string) bool {
    fmt.Printf("%s [y/n] ", prompt)
    if !scanner.Scan() { return false }
    a := strings.ToLower(strings.TrimSpace(scanner.Text()))
    return a == "y" || a == "yes"
}
```

Dos cosas escondidas en esas cinco líneas:

1. **El default es no.** Entrada vacía → false. Ctrl-D → false. Cualquier carácter no reconocido → false. El default conservador es el seguro cuando estás a punto de correr un comando de shell.
2. **El mismo `scanner` que el REPL principal.** Tener dos scanners sobre stdin causa carreras de buffer. Hay un único scanner global, compartido por `main` y `confirm`.

## Por qué "user denied" es un tool result, no una parada brusca

Cuando dices que no, no crasheamos, no abortamos la conversación, no salteamos al modelo. Devolvemos:

```go
return "user denied this tool call", true
```

El `true` es `is_error`. El modelo recibe de vuelta un tool result diciendo que la llamada fue denegada. Comportamiento típico del modelo ante una denegación:

- Probar otro enfoque (otra herramienta, otros argumentos)
- Preguntarte qué prefieres
- Disculparse y parar

Este es el mismo canal que cualquier otra falla de herramienta (archivo no encontrado, error de salida de bash, etc.). El modelo no necesita saber si fue una denegación deliberada o un error del sistema — el contrato es solo "las tool calls pueden fallar; aquí está el mensaje".

Esta es una de las decisiones de diseño más importantes del harness. **El modelo está en un bucle; las fallas son entradas a la siguiente iteración, no excepciones.** Tratar denegaciones, errores y éxitos de forma uniforme es lo que le permite al modelo adaptarse.

## Qué se controla

En esta implementación: **cada tool call**. Cada vez que el modelo quiere invocar `bash`, `read_file` o `write_file`, se te pregunta.

Esto es excesivamente cauteloso para `read_file` y `write_file` — están acotados a paths específicos, son más fáciles de razonar que un comando de shell. Un diseño más matizado usaría una interfaz `PermissionPolicy` con políticas nombradas:

| Política | Comportamiento |
|---|---|
| `AlwaysAllow` | Auto-ejecutar |
| `AlwaysAsk` | Preguntar cada vez (lo que tenemos) |
| `AllowList{names}` | Auto-ejecutar las herramientas nombradas, preguntar por todo lo demás |
| `AskOnce` | Preguntar la primera vez, recordar durante la sesión |

No construimos eso. Lo dejamos como ejercicio. La interfaz encaja limpiamente entre el bucle del agente y el registry:

```
agent loop → permission policy → registry.Execute
```

Un `policy.Decide(name, input) → allow | deny | ask` reemplazaría la llamada inline a `confirm`.

## Tropiezos

**Carreras de scanner.** No crees un segundo `bufio.Scanner` para el prompt de confirmación. Los dos se robarían bytes uno al otro de forma impredecible. Compartan uno.

**Olvidar marcar el error.** Devolver `"user denied"` con `isError: false` hace que el modelo piense que la herramienta tuvo éxito con una salida poco útil. Va a actuar sobre esa confusión. Siempre pon `isError: true` para las denegaciones.

**El supuesto oculto: estás frente al teclado.** En un contexto no interactivo (CI, tests con scripts) el prompt se quedaría colgado esperando input. Acá no manejamos eso. Una versión real de producción auto-negaría cuando stdin no es un TTY.

> **En el repo actual.** `executeTool` en [`main.go`](../main.go) es el wrapper:
>
> ```go
> func executeTool(name, rawInput string) (string, bool) {
>     fmt.Printf("[tool] %s %s\n", name, rawInput)
>     if !ui.Confirm("approve?") {
>         return "user denied this tool call", true
>     }
>     return registry.Execute(ctx, name, rawInput)
> }
> ```
>
> La función `confirm` evolucionó de una simple lectura con `bufio.Scanner` a una transición de estado en Bubble Tea — el capítulo 12 cubre cómo. El contrato `is_error: true` no cambió.

## Ahora prueba

1. Pídele al agente que haga algo destructivo (`delete all the .log files in the current directory`). Aprueba. Mira qué corrió de verdad.
2. Mismo prompt — pero deniega. Mira cómo el modelo maneja la denegación. ¿Te preguntó qué hacer, o simplemente paró?
3. Abre `executeTool` y *quita* la llamada a `confirm` temporalmente. Prueba el prompt destructivo otra vez. Siente la diferencia.

## Fin del arco 1 — el mínimo viable

Tienes un agente que funciona. En estos dos capítulos el harness ha aprendido a sostener una conversación con Claude, correr tres herramientas y pedir permiso antes de tocar nada peligroso. Son unas 150 líneas de Go en un solo archivo y, francamente, se parece más a un juguete que a una herramienta. No pasa nada — *es* un juguete. Lo que le falta es ser *extensible*.

El próximo arco (capítulos 03–08) va de ganarse el derecho a llamar a esto un *harness* de verdad: que el LLM se pueda cambiar, que la conversación se pueda gestionar, que la entrada se sienta humana. La receta se repite — una interfaz pequeña, una implementación por defecto y un hueco para que otros conecten lo suyo.

Siguiente: [03 · La interfaz de proveedor](03-the-provider-interface.md).
