# 12 · La TUI completa

En el capítulo 08 usamos Bubble Tea solo para la caja de entrada — un programa one-shot que corría, tomaba una línea, y salía. El REPL seguía iterando e imprimiendo a stdout.

Ahora vamos hasta el fondo: **un solo programa Bubble Tea es dueño de toda la UI.** Un viewport para el scrollback, una caja de entrada con borde, un prompt de aprobación que toma el control cuando hace falta, un spinner arriba del input mientras el agente trabaja, y un indicador de estado que muestra qué subagentes están corriendo.

Este es el capítulo donde el harness empieza a verse como Claude Code u OpenCode.

## El cambio arquitectónico

Antes: bucle REPL → entrada → el agente corre sincrónicamente, imprime a stdout → siguiente entrada.

Después: programa Bubble Tea → caja de entrada → envías → el agente corre **en una goroutine**, postea eventos al programa → el programa actualiza el viewport → evento done → siguiente entrada.

Todo el asunto es una máquina de estados dentro del model:

```go
type modelState int
const (
    stateIdle modelState = iota
    stateRunning
    stateAwaitingApproval
)
```

Transiciones:

| Desde | Evento | A |
|---|---|---|
| `stateIdle` | envías una línea | `stateRunning` |
| `stateRunning` | el agente llama a `Confirm` | `stateAwaitingApproval` |
| `stateAwaitingApproval` | eliges y/n | `stateRunning` |
| `stateRunning` | el agente retorna | `stateIdle` |

## El truco que lo hizo tratable

El bucle del agente, los comandos de barra, y las herramientas todos usan `fmt.Println` plano para imprimir. Reescribir las ~40 sitios de print para empujar eventos tea.Msg sería tedioso.

En cambio, **redirigimos `os.Stdout` a un pipe** y reenviamos cada línea al programa como un `AppendMsg`:

```go
// main.go
originalStdout := os.Stdout
r, w, _ := os.Pipe()
os.Stdout = w

program := ui.NewProgram(runner)

go func() {
    scanner := bufio.NewScanner(r)
    for scanner.Scan() {
        program.Send(ui.AppendMsg(scanner.Text() + "\n"))
    }
}()
```

Todo `fmt.Println` existente escribe al pipe. El forwarder lee cada línea y la postea al programa. El handler `Update` del programa la agrega a su buffer de scrollback:

```go
case AppendMsg:
    m.output.WriteString(string(msg))
    m.viewport.SetContent(m.output.String())
    if m.followBottom { m.viewport.GotoBottom() }
```

Cero refactor de los sitios de print. Herramientas, comandos, logs del agente — todos fluyen al viewport tal cual.

Hay una sutileza: Bubble Tea también escribe a stdout para su propio renderizado. Si redirigimos *eso*, tenemos un bucle infinito. El fix es `tea.WithOutput(originalStdout)` — decirle a Bubble Tea que escriba al stdout *original*, no al redirigido.

En realidad, en este codebase usamos `tea.WithAltScreen()` en su lugar — Bubble Tea toma el control del terminal en modo "alternate screen" y escribe ahí directamente. Mismo efecto (la salida de Bubble Tea bypasa el pipe), distinto mecanismo.

## El model y sus partes

```go
type harness struct {
    runner AgentRunner    // closure que dispatcha comandos o corre el agente

    viewport viewport.Model  // scrollback
    input    textinput.Model // entrada con borde
    spinner  spinner.Model   // braille animado mientras corre

    state          modelState
    approvalPrompt string
    approvalReply  chan bool

    output *strings.Builder  // ← tiene que ser un puntero (próximo tropiezo)
    followBottom bool         // auto-scroll si estamos abajo
}
```

El View() compone las partes:

```go
func (m harness) View() string {
    return lipgloss.JoinVertical(
        lipgloss.Left,
        m.viewport.View(),
        m.inputArea(),
    )
}
```

`inputArea` es donde la máquina de estados se asoma. Devuelve o la caja de aprobación y/n o la caja de entrada normal, con una línea opcional de spinner arriba cuando está corriendo. Siempre reserva 5 líneas así el layout no salta en transiciones de estado.

## El flujo de aprobación

Esta es la transición de estado más interesante. Cuando el agente llama a `Confirm("approve?")`, necesitamos bloquear hasta que tú elijas. Pero no podemos *literalmente* bloquear en el Update de Bubble Tea — eso congelaría la UI entera.

La solución: un channel.

```go
// En main.go, cuando se configura el agente root:
rootAgent.Confirm = func(prompt string) bool {
    reply := make(chan bool, 1)
    program.Send(ui.ApprovalRequest{Prompt: prompt, Reply: reply})
    return <-reply
}
```

La goroutine del agente manda un `ApprovalRequest` al programa y se bloquea en el channel reply. El Update del programa maneja `ApprovalRequest` cambiando el estado a `stateAwaitingApproval` y guardando el channel. Cuando presionas y o n, el programa escribe al channel — desbloqueando la goroutine del agente — y vuelve el estado a `stateRunning`.

```go
case ApprovalRequest:
    m.state = stateAwaitingApproval
    m.approvalPrompt = msg.Prompt
    m.approvalReply = msg.Reply
    return m, nil
```

El agente nunca toca Bubble Tea directamente. Llama a una función (`Confirm`) que casualmente está implementada en términos de channel + Bubble Tea. Separación limpia; el agente es reutilizable en contextos no-TUI.

## El spinner arriba del input

`bubbles/spinner` provee un indicador braille animado. Tres reglas gobiernan cuándo anima:

1. **Se emite tick cuando el estado pasa a running.** Dentro del handler de la tecla Enter: `return m, tea.Batch(m.runOnce(text), m.spinner.Tick)`.
2. **El handler de TickMsg se auto-perpetúa mientras corre.** Cada tick re-renderiza y emite el siguiente tick. Cuando el estado no es running, devolvemos nil — la cadena se corta.
3. **La vista muestra la línea del spinner solo cuando está corriendo.** Otras veces la línea está en blanco pero reservada, manteniendo el layout estable.

La línea de estado también incluye cualquier subagente activo inline: `⠹ thinking... · research`. Sin un status bar separado; la línea del spinner alcanza.

## Romper ciclos: `agent` ya no importa `ui`

Agregar `ui → subagent` (para `subagent.Active()`) creó `agent → ui → subagent → agent`. El fix fue tirar los imports directos a UI de `agent` — quitar la llamada a `ui.StartSpinner` (el status bar lo reemplaza), inline un diff de compactación en texto plano en lugar de llamar a `ui.PrintCompaction`.

Este es en realidad un diseño más limpio: el agente es lógica pura sin conocimiento de UI. Imprime líneas a stdout vía `fmt.Println`, la TUI captura esas líneas vía el truco del pipe. El agente no sabe que *hay* una TUI.

## El panic de `strings.Builder`

Mientras construíamos esto, el programa hizo panic con:

```
panic: strings: illegal use of non-zero Builder copied by value
```

Qué pasó: Bubble Tea pasa el model por valor a través de `Update`. `strings.Builder` corre un `copyCheck` en cada escritura que detecta cuando ha sido copiado — ese es el punto entero del mecanismo de seguridad de `Builder`. El campo `output strings.Builder` del model estaba siendo copiado en cada Update, disparando el panic en el siguiente WriteString.

Fix: usa `*strings.Builder`. El puntero sobrevive intacto a la copia por valor.

```go
type harness struct {
    // …
    output *strings.Builder   // ← puntero, no valor
}
```

**Regla general para models de Bubble Tea:** cualquier cosa en un model que no se puede copiar de manera segura — `sync.Mutex`, `strings.Builder`, file handles, cualquier cosa con `noCopy` — tiene que vivir detrás de un puntero. El patrón "devuelve un model nuevo desde Update" parece inmutable pero en realidad es "copia + muta + devuelve".

## Qué obtienes

Concretamente:

- **Scrollback real.** PgUp/PgDn/Home/End para hacer scroll del viewport.
- **Auto-follow cuando estás abajo.** Haz scroll hacia arriba, el viewport se queda donde lo pusiste. Haz scroll hacia abajo, vuelve a seguir la salida nueva.
- **Indicador de subagente en vivo.** Durante una corrida, la línea del spinner muestra `⠹ thinking... · research` cuando un subagente de research está en vuelo.
- **Aprobación inline.** El prompt y/n reemplaza la caja de entrada con una versión con borde amarillo. Una sola tecla; no hace falta Enter.
- **Terminal restaurado al salir.** El modo alt-screen significa que Ctrl-D te devuelve al prompt de tu shell con el estado del terminal anterior intacto.

## Tropiezos

**No intentes imprimir desde dentro de Update.** El Update del model corre en el bucle principal de Bubble Tea. Si llamas a `fmt.Println` desde ahí, estás escribiendo al pipe redirigido, que manda un `AppendMsg` de vuelta a Update. Recursivo, eventualmente se bloquea. Usa `tea.Cmd` para cualquier efecto secundario.

**El tracking de `AtBottom()` del viewport.** Usamos `m.followBottom = m.viewport.AtBottom()` después de reenviar los eventos al viewport. Esto es lo que hace que "auto-scroll pero deja de seguir cuando hiciste scroll hacia arriba" funcione. Fácil de olvidar; resulta o en saltar-siempre-al-fondo o en nunca-seguir.

**Subagentes de ejemplo que nunca terminan.** Como la goroutine del agente se puede bloquear en un channel de confirm, necesitas una salida si te quedas atrapado. No tenemos — Ctrl-D cierra el programa entero. Producción también manejaría Ctrl-C como "abortar la operación actual" mandando una señal de cancelación a través del context.

> **En el repo actual.** Las piezas de este capítulo:
>
> - La TUI entera: [`internal/ui/program.go`](../internal/ui/program.go). El model es `harness`; los mensajes son `AppendMsg` / `ApprovalRequest` / `agentDoneMsg`; la máquina de estados son las tres constantes arriba.
> - El truco del pipe de stdout y cómo la función Confirm del agente se cablea para mandar `ApprovalRequest`: [`main.go`](../main.go). Lee la función `main()` de arriba a abajo — setup del pipe, construcción del programa, forwarder en goroutine, `program.Run()`.
> - `ui.SuppressSpinner = true` en `main.go` deshabilita el spinner legacy del capítulo 04 (que corrompería la TUI escribiendo escapes `\r` al pipe).

## Ahora prueba

1. Mientras el agente está corriendo, haz scroll hacia arriba con PgUp. Nota que el auto-follow para. Haz scroll de vuelta abajo (End). Nota que retoma.
2. Fuerza una tarea de larga duración y observa el spinner. Después dispara un subagente — mira la línea del spinner actualizarse para incluir el nombre del subagente.
3. Prueba reemplazar `tea.WithAltScreen()` con ninguna opción (así Bubble Tea renderiza inline). El manejo del cursor se pone más raro, pero puedes ver cómo la abstracción del alt-screen está haciendo trabajo por ti.

## Fin del arco 3 — la arquitectura paga

El harness ya está estructuralmente completo. Provider, herramientas, compactación, subagentes, TUI — todas las capas tienen su costura y encajan limpiamente entre sí. Esa es la prueba de fuego de las abstracciones: al bucle del agente le da igual que una herramienta sea local o remota, que el modelo venga de Anthropic o de OpenAI, o que la UI imprima a stdout o pinte un programa Bubble Tea.

El capítulo 13 cierra el libro principal con lo que dejamos fuera a propósito y por dónde seguir. Los capítulos 14–19 son *extras* que se enchufan encima de lo que has construido — servidores MCP como herramientas, contexto del proyecto vía `AGENTS.md`, el visor de tokens, prompt caching, aprobación con diff para las escrituras y memoria persistente del agente. Son independientes entre sí; léelos cuando te interese la funcionalidad correspondiente.

Siguiente: [13 · Qué sigue](13-whats-next.md).
