# Follow-Along

Un recorrido capítulo por capítulo sobre cómo se armó este harness. La intención es **la historia del porqué**, no un repaso línea por línea — el repo en HEAD es la fuente de verdad para ver cómo queda el código; estos capítulos explican cómo se decidió cada capa y qué cuesta saltársela.

Léelo en orden. Cada capítulo es corto (5–10 min) y se enfoca en una sola decisión de diseño.

## Capítulos

| # | Título | De qué trata |
|---|---|---|
| [00](00-introduction.md) | Introducción | Qué es la "ingeniería de harness" y por qué un proyecto de aprendizaje encaja en este espacio |
| [01](01-the-agent-loop.md) | El bucle del agente | REPL básico + SDK de Anthropic + tres herramientas + el switch `executeTool` |
| [02](02-the-permission-gate.md) | El control de permisos | Por qué la aprobación vive en la capa del harness, el contrato "error como resultado de la herramienta" |
| [03](03-the-provider-interface.md) | La interfaz de proveedor | Extraer una abstracción de LLM para que el harness no quede casado a un solo SDK |
| [04](04-ui-polish.md) | Pulido de la UI | Banner ASCII, detección del ancho del terminal, spinner de carga |
| [05](05-slash-commands.md) | Comandos de barra | Una pequeña paleta de comandos: `/help`, `/model`, `/clear`, `/tools`, `/exit` |
| [06](06-conversation-state.md) | El estado de la conversación | La API es sin estado; el cliente lleva todo |
| [07](07-compaction-strategies.md) | Estrategias de compactación | Una interfaz para manejar conversaciones largas; sliding window vs summarization; el decorador de logging |
| [08](08-better-input.md) | Mejor entrada | De `bufio.Scanner` → readline → una caja de entrada con borde en Bubble Tea |
| [09](09-plug-and-play-tools.md) | Herramientas plug-and-play | Una interfaz `Tool`, un `Registry` y auto-registro vía `init()` |
| [10](10-project-structure.md) | Estructura del proyecto | Por qué nos mudamos a paquetes `internal/` — y qué costó |
| [11](11-subagents.md) | Subagentes | Extraer `Agent` como struct, la abstracción `Subagent`, la herramienta delegate |
| [12](12-full-tui.md) | La TUI completa | Un programa Bubble Tea de verdad, con viewport, scrollback y flujo de aprobación |
| [13](13-whats-next.md) | Qué sigue | Ejercicios, lo que omitimos a propósito, y hacia dónde llevarlo |

## Extras

Capítulos independientes que extienden el harness con integraciones concretas. Se apoyan en la arquitectura de los capítulos 01–13 pero no son parte del arco central — léelos cuando quieras la feature, sáltatelos cuando no.

| # | Título | De qué trata |
|---|---|---|
| [14](14-mcp-support.md) | Agregando soporte de MCP | Envolver servidores remotos del Model Context Protocol detrás de la interfaz `Tool` |
| [15](15-agents-md.md) | Contexto del proyecto con AGENTS.md | Cargar un archivo markdown específico del proyecto en el system prompt al arranque |
| [16](16-token-viewer.md) | El visor de tokens | Rastrear el uso y el coste de la sesión, comando `/tokens` y línea de estado en vivo |

## Cómo leer esto

Si quieres el **recorrido más rápido**: lee el capítulo 00, después hojea los capítulos 01, 09 y 11 (las tres abstracciones centrales: bucle del agente, herramientas y subagentes). Todo lo demás es pulido en capas.

Si quieres **construirlo tú mismo**: lee en orden, escribe el código de cada capítulo y compara tu trabajo con el repo en HEAD antes de pasar al siguiente.

## Una nota sobre los fragmentos de código

Los bloques de código en estos capítulos son pedagógicos — muestran la *forma* del código en la etapa correspondiente. El repo en HEAD tiene capas adicionales (la interfaz `Tool`, los paquetes `internal/`, la TUI completa) que se introducen gradualmente. No te sorprenda si un fragmento del capítulo 02 no calza línea por línea con `main.go` — para el capítulo 12 ya habíamos refactorizado la mayor parte.
