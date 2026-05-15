# Ejercicio 2 — Subagentes definidos en markdown

**Objetivo:** definir subagentes como archivos markdown cargados desde disco, en vez de structs Go escritos a mano.

**Dificultad:** media. **Tiempo:** 1–2 horas. **Toca:** `internal/subagent/`, `main.go`.

## Lo que ya está en el repo

`internal/subagent/registry.go` define la interfaz `Subagent` (`Name`, `Description`, `Run`) y un registro `Default`. El único subagente concreto hoy es `Research` en `internal/subagent/research.go` — su system prompt, su subconjunto de herramientas y su presupuesto de iteraciones están todos cocidos en Go.

Eso está bien para un subagente. En el momento que quieras tres revisores, dos planners y un especialista en búsqueda de código, no vas a querer escribir un tipo Go por cada uno.

## Lo que vas a construir

Un loader que escanee `.harness/agents/*.md` al arrancar, parsee cada archivo y lo registre con `subagent.Default`. Un archivo se ve así:

```markdown
---
name: reviewer
description: Revisa un diff buscando bugs, errores sin manejar y problemas de seguridad. Devuelve una lista con viñetas.
tools: [read_file, bash]
max_turns: 6
---

Eres un revisor de código. Mira el diff o los archivos que el caller te indique.
Reporta:
- Bugs y fallos en tiempo de ejecución probables
- Errores no manejados / retornos ignorados
- Preocupaciones de seguridad (secretos, inyección, path traversal)

Sé específico. Cita números de línea. Sin preámbulo.
```

El frontmatter configura el wrapper; el cuerpo es el system prompt.

## Pasos sugeridos

1. **Añade un tipo `MarkdownSubagent`** en `internal/subagent/markdown.go` que envuelva un `agent.Agent` y saque la config de un archivo parseado. Implementa `Subagent` igual que `Research` — la mayoría de la lógica en `Run` es idéntica.

2. **Escribe el loader.** Una función `LoadDir(dir string, p provider.Provider, allTools *tool.Registry) ([]Subagent, error)` que:
   - Lea cada `.md` del directorio.
   - Separe el frontmatter (entre líneas `---`) del cuerpo.
   - Parsee YAML o un formato clave-valor mínimo — tú decides. Parseo `key: value` con stdlib es suficiente; no tienes que tirar de `yaml.v3`.
   - Construya un `tool.Registry` filtrado con solo las herramientas listadas en `tools:`.
   - Devuelva un `MarkdownSubagent` por archivo.

3. **Conéctalo en `main.go`.** Después de registrar los subagentes built-in, llama a `LoadDir` y registra cada uno devuelto. Si el directorio no existe, sálta — el arranque no puede fallar por eso.

4. **Añade un comando `/subagents reload`.** Re-escanea el directorio y reemplaza los agentes cargados desde markdown (deja los built-in en paz). Lee `commands.go` para el patrón de registro de comandos.

5. **Valida.** Rechaza archivos sin `name` o `description` con un error claro que apunte a la ruta del archivo. Un subagente sin nombre producirá una herramienta `delegate_` vacía.

## Aceptación

- Sueltas `.harness/agents/reviewer.md` en el directorio de trabajo, corres `go run .`, y `/subagents` lista `reviewer` junto a `research`.
- El modelo puede despacharlo vía `delegate_reviewer` (o la convención de nombres que uses).
- Borrar el archivo y correr `/subagents reload` lo quita del registro.
- Un archivo malformado (sin `name`) imprime un error claro al arrancar pero no peta.

## Extra

- Hot-reload con [`fsnotify`](https://github.com/fsnotify/fsnotify): los edits aparecen sin reiniciar.
- Selección de modelo por agente en el frontmatter (`model: gpt-5-codex`) — envuelve un Provider separado detrás del agente.
- Un campo `extends:` que deje que un agente herede el system prompt o las herramientas de otro.
