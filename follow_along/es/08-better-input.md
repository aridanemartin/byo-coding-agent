# 08 · Mejor entrada

`bufio.Scanner` lee una línea. No te deja mover el cursor a mitad de línea, recuperar historial, ni corregir un typo tres caracteres atrás. Para un REPL de chat donde los mensajes pueden ser largos, eso es brutal.

Vamos a arreglarlo en dos pasos, porque el viaje es más útil que el destino.

## Paso 1: readline

La respuesta clásica de Unix a "edición de líneas en un terminal" es `readline`. En Go, `github.com/chzyer/readline` es un reemplazo drop-in de `bufio.Scanner`:

```go
rl, err := readline.NewEx(&readline.Config{
    Prompt:            "\033[1;36m❯\033[0m ",
    HistoryFile:       "~/.bettatech_harness_history",
    HistorySearchFold: true,
})
defer rl.Close()

for {
    line, err := rl.Readline()
    if errors.Is(err, io.EOF) { return }
    if errors.Is(err, readline.ErrInterrupt) { continue }   // ctrl-c limpia la línea
    // … manejar la línea …
}
```

Eso te da, gratis:

- Flechas para mover el cursor
- Backspace/delete a mitad de línea
- Ctrl-A / Ctrl-E para saltar al inicio/fin
- Flechas arriba/abajo para historial
- Ctrl-R para búsqueda inversa en el historial
- Historial persistido en `~/.bettatech_harness_history` entre sesiones
- Ctrl-D para EOF (salir), Ctrl-C para cancelar la línea actual

Esta es la respuesta correcta para la mayoría de los CLIs. La usamos brevemente y la reemplazamos. ¿Por qué?

## Paso 2: Bubble Tea

Queríamos que la entrada se viera como Claude Code u OpenCode — con borde, estilizada, lista para multi-línea, con una indicación de estado. El estilo de `readline` se queda en "puedes meter códigos ANSI en el prompt". No puede dibujar una caja.

`github.com/charmbracelet/bubbletea` es el framework TUI de Go que esas herramientas usan. Es overkill para "leer una línea". Pero es el punto de partida correcto para *eventualmente* tener una TUI completa (capítulo 12), y nos deja dibujar las affordances de entrada que queramos.

El patrón model-view-update:

```go
type chatInputModel struct {
    ti        textinput.Model
    history   []string
    histIdx   int
    submitted string
    done      bool
}

func (m chatInputModel) Init() tea.Cmd { return textinput.Blink }

func (m chatInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.Type {
        case tea.KeyEnter:
            m.submitted = m.ti.Value()
            m.done = true
            return m, tea.Quit
        case tea.KeyCtrlD:
            return m, tea.Quit
        case tea.KeyUp, tea.KeyDown:
            // navegación de historial (unas pocas líneas por dirección)
        }
    }
    var cmd tea.Cmd
    m.ti, cmd = m.ti.Update(msg)
    return m, cmd
}

func (m chatInputModel) View() string {
    return boxStyle.Render(m.ti.View()) + "\n" + hintStyle.Render("enter: send · ↑↓: history · ctrl-d: exit")
}
```

`ReadChatInput()` envuelve eso en un one-shot: levanta un `tea.NewProgram(...)`, lo corre, retorna cuando el model dice que terminó.

El resultado es una sola caja de entrada con borde que se llama una vez por turno:

```
╭───────────────────────────────────────────────────────╮
│ ❯ your message                                        │
╰───────────────────────────────────────────────────────╯
  enter: send · ↑↓: history · ctrl-d: exit
```

Después de enviar, Bubble Tea sale, la caja se queda en pantalla (porque no usamos modo alt-screen en esta etapa), y la siguiente iteración del REPL dispara otro `ReadChatInput`.

## Por qué dos pasos para la misma meta

Podrías ir directo de `bufio.Scanner` a Bubble Tea. Nosotros no, porque cada paso enseña algo distinto.

- **`bufio.Scanner` → readline** te enseña que la ergonomía importa. La sensación mejora dramáticamente y el cambio de código es chico. Este es el 80/20.
- **readline → Bubble Tea** te enseña el *siguiente paradigma*. MVU no es solo para una entrada más linda; es la arquitectura que vas a usar cuando toda la UI se vuelva una TUI (capítulo 12). Hacerlo una vez para la entrada es calentamiento.

Si estás siguiendo el libro y solo tienes tiempo para un paso, hazlo con readline. Bubble Tea se gana su complejidad en el capítulo 12.

## Un problema sutil: dos lectores en stdin

Una vez que tienes una librería de entrada fancy, no puedes *también* usar `bufio.Scanner` en otro lado (para `confirm()`, digamos). Dos lectores sobre el mismo buffer de stdin se roban bytes uno al otro de formas impredecibles.

El fix en este capítulo: usar un solo mecanismo de entrada. Confirm usa el mismo `readline.Instance` vía `SetPrompt`:

```go
func confirm(prompt string) bool {
    input.SetPrompt(prompt + " [y/n] ")
    defer input.SetPrompt(mainPrompt)
    line, err := input.Readline()
    // …
}
```

En modo Bubble Tea, misma idea — confirm corre otro `tea.NewProgram`. Para el capítulo 12 la UI entera es un solo programa Bubble Tea y confirm se vuelve una transición de estado dentro de ese programa; el problema de "dos lectores" se disuelve porque solo hay uno.

## Tropiezos

**Persistir historial.** Cuando agregas `HistoryFile`, estás escribiendo el texto que tipeaste al disco. Si alguna vez pegas una API key en el chat (pasa), la filtraste a `~/.bettatech_harness_history`. Lo aceptamos para un proyecto de aprendizaje. Para producción: no persistas historial, o haz hash/redacción de patrones específicos primero.

**TUI en entornos no-TTY.** `tea.NewProgram` falla si stdin no es un terminal. No manejamos esto con elegancia — correr `harness < script.txt` crashearía. Una versión del mundo real detectaría no-TTY y caería a modo scanner automáticamente.

**Ambigüedad arriba/abajo con entrada multi-línea.** Una vez que tienes un textarea (vs textinput), las flechas significan "mover dentro del texto", no "navegar historial". El fix estándar es Ctrl-P / Ctrl-N para historial (convención de Emacs) y reservar las flechas para movimiento de cursor. Nosotros usamos `textinput` de una sola línea para esquivar esto.

> **En el repo actual.** La versión Bubble Tea one-shot de la entrada (con navegación de historial e historial persistente) está en [`internal/ui/input.go`](../internal/ui/input.go) — mira `chatInputModel`. Para el capítulo 12 la entrada es parte de un programa TUI persistente más grande, pero `input.go` todavía tiene la versión standalone y los helpers `loadHistory` / `appendHistory`. La persistencia del historial todavía no está cableada en la TUI del capítulo 12 — ese es uno de los ejercicios del capítulo 13.

## Ahora prueba

1. Compara la sensación del harness con la versión `bufio.Scanner` (usa git para checkout de un estado anterior si tienes historial) vs la versión readline vs la versión Bubble Tea. Nota cuánto del "pulido" es solo conteo de affordances.
2. Lee `internal/ui/input.go` y rastrea el camino de código de la flecha arriba. El campo `bufferText` guarda lo que estabas tipeando *antes* de empezar a navegar el historial, así que presionar Abajo pasando la entrada más reciente lo restaura. Sorprendentemente fácil de pasar por alto.
3. Reemplaza `textinput` con `textarea` (también de `bubbles`) y descubre los keybindings multi-línea. Específicamente: ¿cómo enlazas Shift-Enter a "insertar newline" mientras que Enter pelado "envía"? (Este es un agujero de conejo del mundo real — algunos terminales no pueden distinguir los dos.)

## Fin del arco 2 — las abstracciones se ganan su sitio

Seis capítulos de abstracciones: una interfaz `Provider`, una paleta de slash commands, un contrato explícito de conversación, tres estrategias de compactación, una caja de entrada de verdad. Cada capítulo resolvió un problema y añadió una costura. Ahora el harness se lee como un *sistema* en lugar de como un script — y ya puedes cambiar el LLM, la estrategia de compactación y la capa de entrada sin tocar el resto.

El arco 3 (capítulos 09–12) es donde esas costuras pagan. Convertimos el switch de herramientas en un registro, movemos el código a paquetes `internal/`, introducimos subagentes (cada uno es solo otro agente corriendo el mismo bucle) y reemplazamos los prints a stdout con un programa Bubble Tea completo. Para cuando termines, el harness se parece a un Claude Code pequeño.

Siguiente: [09 · Herramientas plug-and-play](09-plug-and-play-tools.md).
