# 00 · Introducción

## Qué vamos a construir

Un agente de código en un terminal. Escribes una pregunta o una tarea; el agente llama herramientas (`bash`, `read_file`, `write_file`) para actuar sobre tu sistema de archivos; puede delegar investigación de solo lectura a un subagente; las conversaciones largas se compactan automáticamente. Alrededor de 1.000 líneas de Go.

El objetivo a construir se ve así cuando lo ejecutas:

<img width="885" height="332" alt="bettatech-tui" src="https://github.com/user-attachments/assets/c726f9c6-466b-4193-8f24-5a4bb9e96994" />

Aquí no hay nada exótico. Es un modelo, un bucle que lo invoca, algunas herramientas que el modelo puede usar y una UI que le permite a una persona dirigirlo. Lo interesante son las *costuras* — dónde termina una pieza y empieza la siguiente.

## Qué es la "ingeniería de harness"

El modelo es el motor. El **harness** es todo lo demás: el bucle que invoca al modelo, las herramientas que puede usar, cómo se moldea la conversación con el tiempo, qué tiene permitido hacer, cómo hablas con él.

La disciplina importa porque el mismo modelo detrás de dos harness distintos se comporta como dos productos distintos. Claude Code, OpenCode, Aider y Cursor usan más o menos la misma familia de modelos. Sus personalidades — rápidas o cuidadosas, transparentes u opacas, capaces o cautelosas — viven en sus harness. Acierta con el harness y un modelo de gama media se siente excelente; equivócate y un modelo de vanguardia se siente roto.

Este proyecto es una versión reducida y legible de ese tipo de harness, diseñada para que la trastees.

## Por qué un libro de "constrúyelo tú mismo"

Las grandes lecciones de la ingeniería de harness no son visibles en los productos terminados. Cuando ves una herramienta pulida, ya no puedes saber *por qué* su superficie de herramientas se ve como se ve — por qué tres herramientas dedicadas en lugar de un solo `bash`, por qué la aprobación es por llamada y no por sesión, por qué la compactación es del lado del cliente y no del servidor. Eso son decisiones, no hechos. La forma de interiorizar una decisión es tomarla tú mismo.

Así que cada capítulo introduce una pieza del harness, explica las alternativas que consideramos, elige una y te dice qué cuesta.

## Requisitos previos

- **Go 1.21+.** Usamos genéricos, la función `max` y `golang.org/x/term`.
- **Una API key de Anthropic.** Consíguela en [console.anthropic.com](https://console.anthropic.com). El tier gratuito alcanza.
- **Comodidad leyendo Go.** No tienes que escribirlo, pero si quieres aprender lo máximo, vas a escribir el código de cada capítulo antes de espiar el HEAD.
- **Un terminal de verdad.** Algunos capítulos renderizan ASCII art y TUIs; la experiencia dentro del "panel de terminal" de un IDE a veces se siente con lag.

## Cómo está estructurado el libro

Tres arcos generales:

| Capítulos | Arco | Qué construyes |
|---|---|---|
| 01–02 | Lo mínimo necesario | Un REPL que llama a Claude, ejecuta herramientas y pide permiso antes de las destructivas |
| 03–08 | Las abstracciones se ganan su lugar | `Provider`, comandos de barra, compactación, una mejor entrada |
| 09–12 | La arquitectura rinde frutos | Herramientas plug-and-play, paquetes `internal/`, subagentes, una TUI completa |

Para el final del capítulo 02 ya tienes algo que funciona. Para el final del capítulo 12 tienes algo que se parece a un pequeño Claude Code.

## Una nota sobre el modelo

Usamos `claude-opus-4-7` en todo el libro. Opus 4.7 está documentado como un modelo que invoca menos subagentes y que sigue los system prompts de forma más literal que Opus 4.6 — estos comportamientos aparecen en los capítulos 07 y 11. Si estás siguiendo el libro con otro modelo, los prompts pueden comportarse distinto; en general no pasa nada.

## Qué no cubre este libro

- Construir un modelo. Usamos el SDK de Anthropic; tratamos al modelo como una caja negra.
- Despliegue en producción, sistemas multiusuario, persistencia. El harness es solo local.
- Salida en streaming token a token. El harness espera la respuesta completa antes de renderizar.
- Políticas de permisos más complejas que "preguntar cada vez".

El capítulo 13 habla de cómo añadir todo esto.

Siguiente: [01 · El bucle del agente](01-the-agent-loop.md).
