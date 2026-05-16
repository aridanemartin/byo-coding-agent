# 04 · Pulido de la UI

Para el capítulo 03 el harness es un REPL que funciona: habla con Claude, corre tres herramientas, pregunta antes de operaciones destructivas y puede cambiar de proveedor LLM en una línea. Lo que no tiene es *textura*. El prompt es un simple `>`. No hay feedback mientras el modelo piensa — solo silencio y un cursor parpadeando durante dos a cinco segundos. Cambia el tamaño de la terminal a menos de 80 columnas y la salida se descuadra.

Un capítulo corto, entonces. Vamos a agregar un banner, hacer que sobreviva a terminales angostas, y agregar un spinner de carga. Nada de esto es load-bearing; el objetivo es aprender tres técnicas pequeñas que aparecen en todas partes en trabajo de CLI.

## El banner

ASCII art usando la fuente figlet "ANSI Shadow", que la mayoría de las herramientas terminal-AI (Claude Code, OpenCode) usan como su wordmark.

```
██████╗ ███████╗████████╗████████╗ █████╗ ████████╗███████╗ ██████╗██╗  ██╗
██╔══██╗██╔════╝╚══██╔══╝╚══██╔══╝██╔══██╗╚══██╔══╝██╔════╝██╔════╝██║  ██║
██████╔╝█████╗     ██║      ██║   ███████║   ██║   █████╗  ██║     ███████║
██╔══██╗██╔══╝     ██║      ██║   ██╔══██║   ██║   ██╔══╝  ██║     ██╔══██║
██████╔╝███████╗   ██║      ██║   ██║  ██║   ██║   ███████╗╚██████╗██║  ██║
╚═════╝ ╚══════╝   ╚═╝      ╚═╝   ╚═╝  ╚═╝   ╚═╝   ╚══════╝ ╚═════╝╚═╝  ╚═╝
```

Más un subtítulo en gris tenue. Envuelve todo en `\033[1;36m` (cian negrita) y `\033[0m` (reset). Listo.

## Terminales angostas

El banner tiene 75 columnas de ancho. En cualquier cosa menos a ~78 columnas se enrolla y se ve como basura. Así que detectamos el ancho del terminal y caemos a un wordmark de texto plano.

```go
import "golang.org/x/term"

func TermWidth() int {
    w, _, err := term.GetSize(int(os.Stdout.Fd()))
    if err != nil { return 0 }
    return w
}

func PrintBanner() {
    if TermWidth() >= 78 {
        // banner grande
    } else {
        // wordmark de una línea: "  BETTATECH  ·  build your own coding agent"
    }
}
```

Tres pequeñas cosas escondidas en ese patrón, vale la pena conocerlas porque aparecen en todas partes:

1. **`golang.org/x/term`** es la forma canónica de preguntar "¿stdout es un TTY, y qué ancho tiene?". La biblioteca estándar no lo expone.
2. **`GetSize` da error en no-TTYs** (con pipe, redirigido). Tratamos err como "0 cols", lo que cae en la rama del banner chico — lo correcto para `harness > log.txt`.
3. **78 es margen** para un banner de 75 de ancho. Elegir el ancho exacto es una trampa si alguna vez agregas un solo carácter.

## El spinner

Mientras el agente espera a la API, no ves nada durante varios segundos. Eso es mala UX. Agregamos un pequeño spinner en braille que se sobreescribe en su lugar:

```go
var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

type Spinner struct {
    stop chan struct{}
    done chan struct{}
}

func StartSpinner(label string) *Spinner {
    s := &Spinner{stop: make(chan struct{}), done: make(chan struct{})}
    go func() {
        defer close(s.done)
        ticker := time.NewTicker(80 * time.Millisecond)
        defer ticker.Stop()
        i := 0
        for {
            select {
            case <-s.stop:
                fmt.Print("\r\033[K") // limpia la línea
                return
            case <-ticker.C:
                fmt.Printf("\r%c %s", spinnerFrames[i], label)
                i = (i + 1) % len(spinnerFrames)
            }
        }
    }()
    return s
}

func (s *Spinner) Stop() {
    close(s.stop)
    <-s.done   // bloquea hasta que la goroutine confirme que limpió la línea
}
```

Tres detalles, todos con su razón de existir:

1. **`\r` devuelve el cursor a la columna 0; `\033[K` limpia hasta el fin de línea.** Juntos sobreescriben el frame del spinner limpiamente. Sin el limpiado, pasar de una label larga a una corta deja basura al final.
2. **`Stop()` bloquea en `done`.** Esta es la parte que sorprende. Si `Stop()` devolviera de inmediato, la goroutine del spinner podría imprimir otro frame *después* de que ya nos hubiéramos movido a imprimir la respuesta del modelo. La sincronización garantiza que para cuando `Stop()` retorna, ya no hay más salida del spinner en vuelo.
3. **Chequeo de no-TTY** (no se muestra). Si stdout no es un terminal, el spinner simplemente devuelve un shell no-op. Spammear `\r⠋...` a un archivo de log es peor que no tener spinner.

En el capítulo 12 reemplazamos esto completamente con `bubbles/spinner` dentro de un programa de Bubble Tea. La versión braille-sobre-stdout de arriba está bien para la era del REPL.

## Dónde encaja

```go
// agentLoop
sp := startSpinner("thinking...")
resp, err := provider.Send(ctx, messages, tools)
sp.Stop()
```

El spinner corre solo durante la llamada a la API. Apenas vuelve la respuesta, se detiene, y nosotros imprimimos lo que haya bajado — texto o líneas de log `[tool]` — en una línea limpia.

## Tropiezos

**Animación matando el prompt cache.** No por el spinner en particular, pero sí por las secuencias ANSI: cualquier cosa que termine en el system prompt (timestamps, decoraciones animadas) destruye el prompt caching. Los banners están bien porque se imprimen una sola vez al inicio, no son parte del array de mensajes.

**Ancho de Unicode.** Algunos terminales no renderizan `█` y braille a 1 celda de ancho. En macOS Terminal.app, bien. En unos pocos terminales minimalistas (configuraciones tempranas de `kitty`, algunas configuraciones de `tmux`), el banner se puede enrollar. No hay un fix perfecto; lo aceptamos.

> **En el repo actual.** El código del banner (con la variante ANSI Shadow ancha y el fallback para terminales angostas) está en [`internal/ui/banner.go`](../internal/ui/banner.go). El spinner stand-alone — usado en modo REPL antes de que la TUI tomara el control — es [`internal/ui/spinner.go`](../internal/ui/spinner.go). La versión TUI (capítulo 12) usa `bubbles/spinner` en su lugar; ambos archivos sobreviven en el repo así puedes comparar los dos enfoques.

## Ahora prueba

1. Cambia el tamaño de tu terminal a 60 columnas de ancho. Reinicia el harness. Confirma que el banner de fallback entra en acción.
2. Hazle pipe a la salida: `go run . > /tmp/out.txt`. Abre el archivo. El banner debería ser el *chico* (porque `GetSize` devolvió err en el pipe no-TTY).
3. Cambia los frames del spinner a `|/-\`. Compara la sensación. Mismo principio operativo, vibe muy distinto.

Siguiente: [05 · Comandos de barra](05-slash-commands.md).
