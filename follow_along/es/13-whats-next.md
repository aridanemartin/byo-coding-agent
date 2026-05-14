# 13 · Qué sigue

Tienes un agente de código que funciona. Llama a Claude, corre herramientas, pregunta antes de operaciones destructivas, delega investigación a subagentes, compacta conversaciones largas, y tiene una TUI que no da vergüenza.

Este capítulo trata de lo que omitimos a propósito — las capas que convertirían esto de un proyecto de aprendizaje en algo que usarías de verdad día a día — más ejercicios que encajan naturalmente en la arquitectura existente.

## Qué falta

**Streaming.** El modelo devuelve una respuesta completa antes de que rendericemos nada. Los agentes de código reales hacen streaming de tokens conforme llegan — el texto aparece caracter por caracter, y los bloques `tool_use` se renderizan mientras se construyen. El SDK de Anthropic soporta streaming vía `client.Messages.NewStreaming(...)`; cambiarías `Provider.Send` para que devuelva un channel de eventos parciales, y `agent.loop` los reenviaría como `AppendMsg`s a la TUI. La parte más difícil: hacer que la TUI haga append-in-place (dentro de una línea) en lugar de siempre agregar líneas enteras. El `bubbles/textarea` es un punto de partida para esto.

**Tests.** Nada está automatizado. La interfaz `Provider` fue diseñada específicamente para ser mockeable; el bucle del agente debería ser testeable end-to-end con un `MockProvider` que registra llamadas y devuelve respuestas predefinidas. Las estrategias de compactación son fáciles de unit-testear sobre slices sintéticos de mensajes. Los comandos de barra no tienen tests para nada. Esta es la brecha más grande respecto a un proyecto "real".

**Prompt caching.** Cada turno re-manda la historia completa a precio completo. El prompt cache de Anthropic cortaría eso a ~10% para el prefijo cacheado. La mecánica está bien documentada; el cambio en el harness es chico (agrega `cache_control: {type: "ephemeral"}` al último bloque de system en cada request). Qué cuesta: invalidación de cache. Cualquier cambio al system prompt, a la lista de herramientas, o a cualquier mensaje anterior invalida el cache para ese prefijo. La compactación en particular lo destruye. La contabilidad no es gratis.

**Entrada multi-línea.** La entrada de Bubble Tea es de una sola línea. `bubbles/textarea` desbloquearía Shift-Enter para newlines, pero en algunos terminales no puedes distinguir Shift-Enter de Enter, lo que hace poco confiable "Enter envía, Shift-Enter newline". La mayoría de las herramientas de producción usan Alt-Enter o Ctrl-J como el binding de newline. Elige una convención y documéntala.

**Políticas de permisos más interesantes que "preguntar cada vez".** Ahora mismo cada tool call recibe prompt. Una interfaz `PermissionPolicy` — `AlwaysAllow`, `AlwaysAsk`, `AllowList{names}`, `AskOnce` — encajaría entre el bucle del agente y el registry de herramientas. El esbozo está en el capítulo 02. Este es el follow-up pedagógicamente más limpio; enseña el mismo patrón plug-and-play una vez más, sobre la preocupación más "real" de todas (seguridad).

**Soporte de MCP.** El Model Context Protocol es el estándar para "agentes hablando con servidores de herramientas externos". Agregar soporte de MCP significa convertir `Tool` en algo que pueda estar respaldado por un servidor remoto, no solo un struct Go local. El SDK de Anthropic tiene helpers de MCP; la forma arquitectónica es que las herramientas se vuelven una tupla de `(funciones Go locales, servidores MCP remotos)` y el registry las compone.

**Persistencia.** La conversación vive en memoria; salir la pierde. Un comando `/save` que escriba `messages` como JSON, más un `/load` que lo lea de vuelta, es ~30 líneas. Una versión más sofisticada guarda estado por proyecto (archivo: `.bettatech_harness_session.json` en el cwd).

**Conteo de tokens.** No hay forma de saber qué tan grande es la conversación en tokens. `client.Messages.CountTokens(...)` existe; exponerlo como un comando `/tokens` o en el status bar son unas pocas líneas y útil al ajustar los umbrales de compactación.

## Ejercicios que encajan en la arquitectura existente

Estos están más o menos ordenados de más fácil a más involucrado.

### 1. Agrega una herramienta `web_fetch`

Toma una URL, devuelve el body de la respuesta. Usa `net/http` con un timeout sensato. Suelta un archivo en `internal/tool/`, regístrala con `init()`, listo. ~30 líneas.

### 2. Agrega un mecanismo de aliases para los comandos de barra

Haz que `/quit` sea un alias para `/exit`. Decide si agregar una entrada nueva, o aliases de primera clase vía un campo `aliases []string` en el struct `command`. La decisión es en sí el ejercicio.

### 3. Escribe un `MockProvider`

Implementa `Provider`. Guarda una lista de respuestas predefinidas y las devuelve en orden. Se usa para testear el bucle del agente sin una API key. Bonus: rastrea con qué se llamó a `Send` para que puedas hacer asserts sobre los mensajes y las herramientas.

### 4. Agrega una estrategia de compactación `TokenBudget`

Tira los mensajes más viejos hasta que el conteo de tokens estimado esté bajo un umbral configurable. Empieza con conteo de bytes como proxy de token (barato, sin llamada a la API). Después, cambia a `client.Messages.CountTokens(...)` para precisión.

### 5. Agrega una abstracción `PermissionPolicy`

Interfaz con `AlwaysAllow`, `AlwaysAsk`, `AllowList{names}`, `AskOnce`. Encájala entre el bucle del agente y el registry (reemplaza la llamada inline a `confirm`). Una línea en `main.go` para elegir una política. Replica el patrón Provider / Compactor exactamente.

### 6. Agrega un segundo subagente

Un subagente `CodeReview` con herramientas `read_file` + `bash` (para correr linters), con un system prompt enfocado en review. Regístralo en `main.registerSubagents`. Confirma que el modelo elige el correcto basándose en la descripción.

### 7. Streaming

El grande. Cambia `Provider.Send` para que devuelva un channel de respuestas parciales (o usa un callback). Actualiza `agent.loop` para reenviar los eventos parciales como texto. Actualiza la TUI para hacer append dentro del párrafo actual en lugar de siempre agregar líneas enteras.

### 8. Cablea el historial de entrada a la TUI nueva

El viejo `internal/ui/input.go` todavía tiene `loadHistory` / `appendHistory`. El programa Bubble Tea en el capítulo 12 no los usa. Agrega un campo `[]string` al model del harness, maneja las teclas Up/Down para navegación, persiste a `~/.bettatech_harness_history` al enviar. La entrada del capítulo 08 tenía todo esto — pórtalo.

## Yendo más allá: qué se vuelve Claude Code

Algunas features que distinguen un producto pulido de este harness:

- **Contexto persistente entre sesiones.** Cada proyecto recibe un directorio `.claude`; los mensajes y aprendizajes persisten.
- **Skill packs.** Archivos en una ubicación conocida desde la que el agente puede cargar contexto bajo demanda. (Archivos `SKILL.md`; cargados solo cuando son relevantes a la tarea.)
- **Comandos de barra que puedes definir tú.** Un directorio `.claude/commands/`; cada archivo es un template de prompt; `/<filename>` lo invoca.
- **Hooks.** Callbacks pre-tool y post-tool que configuras; te dejan hacer linting, auditar, o bloquear tool calls sin modificar el código del harness.
- **Diffs inline.** Cuando el agente edita un archivo, mostrar un diff en la UI; dejarte aprobar/rechazar cambios bloque por bloque.
- **Sandboxing.** `bash` corre en un contenedor o shell restringido, no en el host directamente. (Para agentes que operan sobre infraestructura compartida.)
- **Tracking de costos.** El uso de tokens del agente y el costo en dólares siempre visible.

Cada una de esas es un capítulo aparte. Ninguna está en este libro. Si implementas una y quieres mandar un pull request, por favor hazlo.

## Tres niveles, la misma disciplina

Los capítulos anteriores construyeron un harness desde cero. Eso es un nivel. Dos más se asientan encima — la misma disciplina aplicada a mayor altura:

| Nivel | Qué tocas | Dónde está el apalancamiento |
|---|---|---|
| **Construir** | El código Go: bucle del agente, proveedor, registro de herramientas, compactación | Forma el modelo mental. ~1% del tiempo. |
| **Extender** | Código nuevo conectado a abstracciones existentes — wrappers MCP (cap. 14), reporteros de tokens (cap. 16), un subagente nuevo | El puente. ~10% del tiempo. |
| **Configurar** | Archivos que el harness lee y prompts que escribes — `AGENTS.md` (cap. 15), `mcp.json`, un flujo SDD, paletas de comandos de barra | Donde sucede el trabajo real. ~89% del tiempo. |

Un Claude Code mal configurado con un `AGENTS.md` de 50 KB lleno de contradicciones se siente exactamente igual de roto que un harness mal construido. Acertar con cualquiera de los dos es la misma habilidad: un número pequeño de decisiones ortogonales, tomadas con deliberación, con conciencia de lo que cuestan. La razón por la que este libro pone el énfasis en construir es que el modelo mental solo se forma cuando puedes ver cada costura. Una vez que puedes, cada archivo de configuración se lee distinto.

Si entraste pensando que "la configuración no es ingeniería de verdad", del capítulo 14 en adelante debería haber quedado zanjado: `mcp.json` y `AGENTS.md` reconfiguran el comportamiento del agente tanto como cualquier cosa dentro de `internal/`. La diferencia entre niveles es tu radio de explosión, no la clase de razonamiento que aplicas.

## Una nota de cierre sobre la ingeniería de harness

Acabas de construir un harness — aunque chico — para un LLM. Las decisiones arquitectónicas no fueron accidentes: cada interfaz, cada capa, cada decorator fue una respuesta deliberada a un problema real. Cuando leas otros harness (el código de Claude Code, OpenCode, Aider, Continue), vas a ver las mismas formas. Distintos colores, distintos idioms, pero el mismo esqueleto: un bucle del agente, una superficie de herramientas, un control de permisos, un manejador de contexto, una UI.

La próxima vez que te sientes a usar una herramienta de IA para programar, vas a poder leer sus rarezas como decisiones de harness: "ah, preguntan por sesión, no por llamada", "mantienen los tool results fuera de la UI visible", "usan una ventana deslizante en vez de summarization". También vas a saber qué falta o qué harías distinto.

Esa es la victoria. El modelo no es el producto. El harness sí.

← [volver al índice](README.md)
