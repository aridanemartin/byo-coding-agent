# 15 · Contexto del proyecto con AGENTS.md

El system prompt que escribimos en el capítulo 01 le dice al agente *cómo comportarse* — sé conciso, prefiere el subagente de research, los errores son tool results. No dice nada sobre *esta base de código*. El agente no sabe para qué sirve `internal/tool/`, no sabe que `delegate.go` vive fuera de `internal/` a propósito, no conoce tus convenciones.

Podrías pegar ese contexto en cada conversación. La convención hacia la que la gente ha convergido en su lugar es **`AGENTS.md`** — un archivo markdown en la raíz del proyecto que las herramientas de IA leen automáticamente y le pasan al modelo como contexto del proyecto. Claude Code llama a su variante `CLAUDE.md`; la versión multi-herramienta es `AGENTS.md`.

Este capítulo conecta eso al harness.

## Para qué sirve AGENTS.md

Cosas específicas del proyecto que el agente siempre debería conocer pero que no quieres repetir en cada turno:

- Estructura del repositorio y de qué se encarga cada paquete
- Comandos de build / test / lint
- Convenciones ("los errores son tool results, no errores de Go")
- Trampas conocidas ("la herramienta delegate vive en main para evitar un ciclo de importación")
- Lo que este proyecto *no* es (para que el agente no proponga funcionalidades que no encajan)

Cualquier cosa genérica — "sé conciso", "prefiere X sobre Y" — pertenece al system prompt que viene con el harness. Cualquier cosa con forma de proyecto pertenece a `AGENTS.md`. La división sigue la misma línea que `harness vs. modelo`: el harness es dueño del comportamiento reutilizable, el proyecto es dueño de su propio contexto.

## Dónde encaja

El harness ya tiene un system prompt. Hay dos formas razonables de añadir contenido de `AGENTS.md`:

| Enfoque | Cómo | Tradeoff |
|---|---|---|
| **Anexar al system prompt** | Leer `AGENTS.md`, concatenar a `systemPrompt` antes de `provider.NewAnthropicProvider(...)` | Se trata como instrucciones, se envía cada turno, se lleva bien con el prompt caching, sobrevive a `/clear`. ✓ |
| **Inyectar como primer mensaje del usuario** | Empujar `{Role: User, Content: "Project context:\n\n" + contents}` a `rootAgent.SetMessages(...)` antes de que arranque el REPL | Aparece en el scrollback, el agente puede citarlo, pero lo descarta la compactación y solo se reinyecta si lo vuelves a hacer. |

Usamos el enfoque de system prompt. Misma elección que el resto de las herramientas (Claude Code, Cursor, Aider), por las mismas razones: el contenido son *instrucciones sobre la base de código*, no parte de la conversación.

## La implementación

Unas seis líneas en `main.go`:

```go
func loadAgentsContext() string {
    data, err := os.ReadFile("AGENTS.md")
    if err != nil {
        return ""
    }
    return "\n\n# Project context (from AGENTS.md)\n\n" + string(data)
}
```

Y en `main()`, antes de construir el provider:

```go
sysPrompt := systemPrompt + loadAgentsContext()
llm := provider.NewAnthropicProvider(anthropic.ModelClaudeOpus4_7, 8192, sysPrompt)
```

Eso es todo. El provider de Anthropic guarda `sysPrompt` y lo envía como el campo `system` de nivel superior en cada solicitud (capítulo 06). El modelo ve un solo prompt fusionado; no sabe que el prompt son dos archivos pegados.

El texto envolvente (`# Project context (from AGENTS.md)`) es una señal suave al modelo de que la segunda mitad es contexto, no instrucciones. Sin él, el modelo trata cada línea por igual, lo cual está bien pero es ligeramente peor — a veces repite "este codebase dice…" en lugar de simplemente actuar sobre ello.

## Cuándo se lee el archivo

`loadAgentsContext()` se ejecuta **una vez, al arranque**, antes de `provider.NewAnthropicProvider`. Dos consecuencias:

1. **Edita `AGENTS.md`, reinicia el harness.** Sin recarga en caliente, igual que `mcp.json` (capítulo 14). Agregar un comando de barra `/reload-context` es una extensión natural — llama a `provider.SetSystemPrompt(...)` después de releer el archivo. Dejamos el setter fuera por simplicidad.
2. **El archivo se lee desde el directorio de trabajo** — el proyecto contra el que estás ejecutando el harness, no el repositorio del harness. Mismo patrón `os.ReadFile("AGENTS.md")` que `mcp.json`. Si quieres el comportamiento de Claude Code de "subir por el árbol de directorios", son unas 10 líneas: empieza en CWD, sube por los padres hasta encontrar uno o llegar a `/`.

## Dos interpretaciones de dónde vive el archivo

El mismo nombre de archivo cumple dos roles dependiendo del proyecto en el que estés:

| Rol | Vive en | Quién lo lee |
|---|---|---|
| **Lado consumidor** | `tu-proyecto/AGENTS.md` (sea lo que sea en lo que estés trabajando con el harness) | El `loadAgentsContext` del harness. |
| **Lado autor** | `byo-coding-agent/AGENTS.md` (raíz del propio repositorio del harness) | Cualquier agente externo (Claude Code, Cursor, este harness) que alguien ejecute sobre el harness. |

Estos no se contradicen — son la misma convención aplicada a repositorios distintos. El harness incluye su propio `AGENTS.md` en la raíz del repositorio, así que cualquiera que clone el repositorio para trabajar sobre el harness mismo recibe contexto del proyecto gratis, sin importar qué agente use.

## Por qué esto es más potente de lo que parece

Tres cosas se vuelven posibles una vez que `AGENTS.md` está en el system prompt:

- **Las convenciones del proyecto se vuelven aplicables.** "Los errores se devuelven como tool results, no errores de Go" escrito en `AGENTS.md` significa que el modelo se va a corregir solo cuando escriba un `return err`. No tienes que recordárselo.
- **Incorporar agentes nuevos es un archivo.** ¿Cambias de este harness a Claude Code en el mismo repositorio? El agente recibe el mismo contexto. Sin reentrenamiento del modelo, sin configuración por herramienta.
- **Memoria del proyecto.** Las decisiones que tomas durante una refactorización ("movimos X a Y porque Z") se pueden anexar a `AGENTS.md` para que la siguiente sesión las herede. Es la misma idea que los "archivos de memoria" de Claude Code, pero basada en archivos y revisable en git.

## Tropiezos

**Tamaño.** Cada byte de `AGENTS.md` se paga en tokens de entrada en cada llamada a la API. Un `AGENTS.md` de 50 KB va a dominar tus tokens y a ralentizar el caching. Mantenlo por debajo de unos pocos miles de tokens (quizá 5–10 KB). Si necesitas más, enlaza a documentos que el agente pueda `read_file` bajo demanda.

**Secretos.** Resiste la tentación de poner credenciales, claves de API o rutas específicas del entorno en `AGENTS.md`. El archivo se commitea y termina en cada solicitud. Usa el patrón de variables de entorno del capítulo 14 en su lugar.

**Desviación.** `AGENTS.md` se pudre como cualquier otra documentación. Trátalo como código: revisa cambios, poda secciones desactualizadas, borra instrucciones que ya no aplican. Un `AGENTS.md` desactualizado es peor que ninguno — el modelo va a seguir reglas obsoletas con confianza.

**Invalidación de caché.** Si activas prompt caching (tema diferido del capítulo 13), editar `AGENTS.md` invalida el prefijo cacheado en cada solicitud posterior al reinicio. Eso está bien — la reescritura es poco frecuente. Pero no añadas un timestamp; perderías el caching por completo.

> **En el repositorio actual.** El loader es la función `loadAgentsContext` en [`main.go`](../main.go), llamada en línea antes de [`provider.NewAnthropicProvider`](../internal/provider/anthropic.go). El propio [`AGENTS.md`](../AGENTS.md) del repositorio está en la raíz del proyecto.

## Ahora prueba

1. Coloca un `AGENTS.md` de una línea en el repositorio (`This project uses tabs, not spaces.`) y arranca el harness. Pide "escríbeme un archivo Go de hola mundo". Observa cómo el agente cumple con la regla que no se le dijo en la conversación.
2. Borra `AGENTS.md` y reinicia. Nota la diferencia en cómo el agente habla sobre la base de código — mismas preguntas, respuestas más vacías.
3. Añade un comando de barra `/context` que imprima lo que devolvió `loadAgentsContext()`, así puedes verificar qué está viendo realmente el modelo.

← [volver al índice](README.md)
