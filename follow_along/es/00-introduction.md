# 00 · Introducción

## Qué vamos a construir

Un agente de código en un terminal. Escribes una pregunta o una tarea; el agente llama a distintas herramientas (`bash`, `read_file`, `write_file`) para actuar sobre tu sistema de archivos; puede delegar acciones a diferentes subagentes; las conversaciones largas se compactan automáticamente. Alrededor de 1.000 líneas de Go.

El objetivo a construir es algo así:

<img width="885" height="332" alt="bettatech-tui" src="https://github.com/user-attachments/assets/c726f9c6-466b-4193-8f24-5a4bb9e96994" />

No es una aplicación compleja. Es un modelo (empezando con el SDK de Claude), un bucle que lo invoca, algunas herramientas que el modelo puede usar y una UI que le permite a una persona dirigirlo. Lo interesante es la *ESTRUCTURA*.

## Qué es la "ingeniería de arneses"

En la ingeniería de arneses, el modelo (LLM) es el motor. El **arnés (o harness en inglés)** es todo lo demás: el bucle que invoca al modelo, las herramientas que puede usar, cómo se moldea la conversación con el tiempo, qué tiene permitido hacer, cómo interactúas con él.

El cómo implementamos el arnés alrededor del modelo importa ya que la forma de actuar puede cambiar completamente. Claude Code, OpenCode, Aider y Cursor usan más o menos la misma familia de modelos, pero los productos a veces dan resultados muy distintos.

Este proyecto es una versión reducida y legible de ese tipo de arnés, diseñada especialmente para que trastees con ella, la expandas y la modifiques libremente.

## Por qué en estilo "constrúyelo tú mismo"

La mejor forma de aprender como funciona algo es contruyendolo tú mismo. Es por ello que, para aprender cómo aprovechar al máximo las nuevas herramientas de IA que van saliendo y entender ciertas decisiones, la mejor forma es construir tu propio arnés, tu propio cli.

Es por ello que este repositorio está inspirado en los repositorios de "Build Your Own X", ofreciendo una serie de capítulos que puedes ir siguiendo para aprender.

La intención de este arnés no es ser código productivo, sino ser un proyecto de aprendizaje y entendimiento de conceptos que, de otra forma, siempre suenan abstractos.

## Requisitos previos

- **Go 1.21+.** Usamos genéricos, la función `max` y `golang.org/x/term`.
- **Una API key de Anthropic.** Consíguela en [console.anthropic.com](https://console.anthropic.com). El tier gratuito alcanza.
- **Comodidad leyendo Go.** No tienes que escribirlo, pero si quieres aprender lo máximo, vas a escribir el código de cada capítulo antes de espiar el HEAD.
- **Un terminal de verdad.** Algunos capítulos renderizan ASCII art y TUIs; la experiencia dentro del "panel de terminal" de un IDE a veces se siente con lag.

## Cómo está estructurado el repositorio

Tres arcos generales:

| Capítulos | Arco | Qué construyes |
|---|---|---|
| 01–02 | Lo mínimo necesario | Un REPL que llama a Claude, ejecuta herramientas y pide permiso antes de las destructivas |
| 03–08 | Las abstracciones se ganan su lugar | `Provider`, comandos de barra, compactación, una mejor entrada |
| 09–12 | La arquitectura rinde frutos | Herramientas plug-and-play, paquetes `internal/`, subagentes, una TUI completa |

Para el final del capítulo 02 ya tienes algo que funciona. Para el final del capítulo 12 tienes algo que se parece a un pequeño Claude Code.

## Una nota sobre el modelo

Usamos `claude-opus-4-7` en todo el libro. Opus 4.7 está documentado como un modelo que invoca menos subagentes y que sigue los system prompts de forma más literal que Opus 4.6 — estos comportamientos aparecen en los capítulos 07 y 11. Si estás siguiendo el libro con otro modelo, los prompts pueden comportarse distinto; en general no pasa nada.

## Qué no cubre este repositorio

- Construir un modelo. Usamos el SDK de Anthropic; tratamos al modelo como una caja negra.
- Despliegue en producción, sistemas multiusuario, persistencia. El harness es solo local.
- Salida en streaming token a token. El harness espera la respuesta completa antes de renderizar.
- Políticas de permisos más complejas que "preguntar cada vez".

El capítulo 13 habla de cómo añadir todo esto.

Siguiente: [01 · El bucle del agente](01-the-agent-loop.md).
