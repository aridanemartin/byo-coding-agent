# 19 · Memoria del agente

El agente hoy se resetea a cero en cada sesión. El slice `messages` está vacío cuando `main` arranca; el system prompt es el que compilaste; nada de lo que el agente aprendió ayer sobrevive. El capítulo 06 hizo esa falta de estado explícita del lado de la API — cada llamada manda el historial entero. El harness *replicó* esa misma falta de estado del lado del cliente, y cada sesión se convirtió en una isla.

Para una herramienta CLI de un solo uso eso está bien. Para un agente al que vuelves todos los días, es una fricción recurrente:

- Explicas "nuestros test fixtures viven en `testdata/golden/`" tres sesiones seguidas.
- Le enseñas las preferencias de formato cada vez.
- Re-lee los mismos archivos porque no recuerda que ya los vio.
- "Seguir con el trabajo de ayer" significa re-explicar el contexto de ayer.

Este capítulo añade memoria: una capa persistente que el agente lee al arrancar y escribe a lo largo del tiempo. Misma forma que cualquier otro punto de extensión del libro — una interfaz chica, una implementación por defecto, una línea en `main.go` para intercambiar.

## La interfaz Store

Tres operaciones, deliberadamente tres:

```go
// internal/memory/store.go
type Entry struct {
    Time    time.Time
    Kind    string   // "fact", "decision", "session-summary", "preference"
    Content string
    Tags    []string
}

type Store interface {
    Save(ctx context.Context, e Entry) error
    Recall(ctx context.Context, query string, limit int) ([]Entry, error)
    Preamble(ctx context.Context) (string, error)
}

var Default Store = NoMemory{}
```

La división entre `Recall` y `Preamble` es la decisión load-bearing. No toda la memoria tiene la misma proporción valor-a-tokens:

- **Preamble** — siempre cargado en el system prompt al inicio de la sesión. Paga tokens en cada turno para siempre, pero el agente lo tiene sin pensar. Para hechos de alto valor y bajo volumen: "el usuario se llama Martin", "este proyecto usa snake_case", "en este codebase llaman X al dashboard."
- **Recall** — una tool call. Barato cuando no se invoca; caro cuando sí, porque el resultado aterriza en la conversación como un tool result. Para memoria de alto volumen y situacional: resúmenes de sesiones pasadas, decisiones sobre bugs específicos, cosas que el agente podría querer recuperar si vienen al caso.

Una interfaz más simple con solo `Save` + `Snapshot() string` funciona para memorias pequeñas — y eso es `MarkdownStore`, una implementación alternativa que también enviamos — pero no escala. Para el turno 50 de la sesión 30, el snapshot ya pesa más que la conversación que lo precede. La división de 3 métodos deja que las implementaciones decidan qué exponer eagerly vs lazily.

## SessionFiles: la implementación por defecto

La implementación por defecto es `SessionFiles`. Layout:

```
.harness/
├── sessions/
│   ├── 2026-05-13-09h47.md
│   ├── 2026-05-14-22h11.md
│   └── 2026-05-15-08h00.md
└── index.json
```

Cada archivo de sesión es markdown legible por humanos — el tipo de cosa que puedes ojear en cualquier editor, diffear en git, o `cat`-ear desde la shell:

```markdown
# 2026-05-15 08:00
tags: refactor, compaction, debug-panel
duration: 47 minutos · 23 turnos · $0.4729

## Resumen

Trabajamos en la comparación de estrategias de compactación del capítulo 07.
Descubrimos que SafeSplitPoint maneja mal bloques tool_use consecutivos del
mismo turno — necesita un test de regresión.

## Decisiones
- Renombré `compact.Verbose` a `compact.Logging`
- Añadí DedupeReads como estrategia nueva

## Hilos abiertos
- Test de regresión para el caso de tool_use consecutivos (sin escribir)
```

`index.json` es la capa de lookup rápido sobre esos archivos:

```json
{
  "sessions": [
    {
      "path": "sessions/2026-05-13-09h47.md",
      "date": "2026-05-13T09:47:00Z",
      "summary": "Configuré la integración MCP con el servidor deepwiki…",
      "tags": ["mcp", "deepwiki", "tools"]
    }
  ]
}
```

### Cómo se implementan los tres métodos

**`Save(ctx, entry)`** durante una sesión: anexa la entrada a un draft de markdown en memoria. Al cerrar la sesión (o bajo demanda), el draft se vuelca a `sessions/<date>.md` y se inserta un registro nuevo en `index.json`.

**`Recall(ctx, query, limit)`**: carga `index.json`, filtra entradas donde algún tag coincide con `query` o el summary lo contiene (substring case-insensitive), devuelve hasta `limit` entradas. El agente después elige qué archivos de sesión (si alguno) leer completos vía `read_file`. Recall no lee los cuerpos — devuelve metadata.

**`Preamble(ctx)`**: devuelve los resúmenes de las últimas 5 sesiones (configurable) concatenados, junto con sus fechas y tags. Tamaño acotado — solo contexto reciente. El número se elige para que el preamble típico se mantenga por debajo de ~1 KB, suficientemente chico para que el prompt caching (capítulo 17) lo absorba limpiamente.

## Dos tools enchufadas

Dos tools nuevas en `internal/tool/` se auto-registran vía `init()` (capítulo 09) y usan `memory.Default`:

```go
// internal/tool/remember.go
type RememberTool struct{}

func init() { Default.Register(&RememberTool{}) }

func (RememberTool) Definition() api.ToolDef { /* … */ }

func (RememberTool) Execute(ctx context.Context, rawInput string) (string, bool) {
    var in struct{ Content, Kind string; Tags []string }
    json.Unmarshal([]byte(rawInput), &in)
    err := memory.Default.Save(ctx, memory.Entry{
        Time: time.Now(), Kind: in.Kind, Content: in.Content, Tags: in.Tags,
    })
    if err != nil { return err.Error(), true }
    return "remembered.", false
}
```

`recall(query, limit)` sigue la misma forma, llamando a `memory.Default.Recall`. Las dos son exactamente análogas a cómo `delegate.go` usa `subagent.Default`.

## Cableado en `main.go`

Tres líneas más un defer de cierre. Misma forma que el setup de MCP (capítulo 14):

```go
mem, err := memory.NewSessionFiles(".harness")
if err != nil { /* log, fallback a NoMemory */ }
memory.Default = mem
defer mem.Close()  // vuelca el draft de la sesión en curso al disco

sysPrompt := systemPrompt + loadAgentsContext() + mustPreamble(mem)
```

`loadAgentsContext` (capítulo 15) ya concatenaba contexto de proyecto escrito por humano; ahora `mustPreamble(mem)` añade la memoria escrita por el agente. Componen: AGENTS.md es lo que *tú* le dijiste al agente sobre el proyecto, el preamble es lo que el *agente aprendió*. Ambos se anteponen limpiamente; ambos son prefijos estables (amigables con el prompt caching).

## Resumen al cerrar sesión

La pregunta de diseño grande: ¿cómo se resume una sesión?

| Opción | Cómo | Tradeoff |
|---|---|---|
| **Agente decide a media sesión** | El agente llama a `remember(...)` cada vez que algo se siente vale la pena guardar | Coincide con lo que importó *semánticamente* — pero el agente olvida hacerlo más de la mitad de las veces |
| **Auto-summary al cierre** | Al Ctrl-D, el harness manda un prompt final sintético: "resume esta sesión en un párrafo + 3-5 tags" y escribe el resultado | Consistente y automático. Cuesta ~$0.001–0.01 extra por sesión por la llamada del resumen. |

Hacemos **auto-summary al cierre** por defecto. Es el camino más confiable. Los usuarios que quieran memoria dirigida por el agente pueden deshabilitar el hook y depender de llamadas explícitas a `remember(...)`; la arquitectura soporta ambos.

El hook vive en el defer de shutdown de `main`: después de que `program.Run()` retorna, si `mem != nil` y la sesión tuvo conversación, el harness corre un prompt más contra el proveedor con el system prompt "resume la siguiente sesión de agente de programación en un párrafo seguido de 3-5 tags de una palabra" y la conversación completa como mensaje del usuario. El resultado se vuelve el cuerpo del archivo de sesión. Cuesta ~1 KB de tokens. Captura lo que el agente aprendió incluso si nunca llamó a `remember` ni una vez.

## Por qué esto es extensible

La interfaz de 3 métodos absorbe cada backend que discutimos sin cambios:

| Implementación | Save | Recall | Preamble |
|---|---|---|---|
| `NoMemory` (fallback) | no-op | vacío | "" |
| `MarkdownStore` | append a `.harness/memory.md` | grep + parse | el archivo entero |
| `SessionFiles` | dir de sesiones + index.json | filtrar índice, devolver metadata | últimas N sesiones |
| `JSONLStore` | append línea a `.harness/memory.jsonl` | scan + filter por Tags/Content | proyectar a markdown |
| `BoltStore` | put en KV | scan + filter por valor | concatenar entries `kind=preamble` |
| `BleveStore` | indexar entry como documento | ranking BM25 | top-N por Time |
| `VectorStore` | embed + insert | coseno contra embedding del query | top-K por timestamp |
| `MockStore` (tests) | append a slice en memoria | filter en memoria | concatenar todo |

Añadir cualquiera de esos es un archivo nuevo en `internal/memory/`, una línea cambiada en `main.go`. El bucle del agente nunca se entera.

Un `VectorStore` haría: en `Save`, embebe la entrada vía el proveedor y guarda `(embedding, content)`; en `Recall`, embebe el query y devuelve top-K por coseno; en `Preamble`, devuelve los K más recientes por timestamp. Mismos tres métodos. El capítulo de `Provider` (capítulo 03) hizo la abstracción; este capítulo extiende el mismo patrón a una preocupación distinta.

## Interacción con el resto del harness

| Capa | Relación |
|---|---|
| **AGENTS.md** (cap. 15) | Componen: AGENTS.md = contexto de proyecto escrito por humano, memoria = aprendizajes escritos por el agente. Ambos se anteponen al system prompt. AGENTS.md vive en la raíz del repo y está versionado; `.harness/` está en `.gitignore`. |
| **Compactación** (cap. 07) | Ortogonal. La compactación gestiona la memoria *de trabajo* (el slice `messages` en vivo). El memory store gestiona lo que sobrevive entre sesiones. Si la compactación destruye la mayor parte de la conversación, el resumen de sesión escrito al cierre sigue basándose en el *transcript completo* — la compactación corre en el bucle del agente, el resumen corre en shutdown sobre el historial sin compactar. |
| **Prompt caching** (cap. 17) | `Preamble` es un prefijo estable → objetivo ideal de caché. El primer turno de cada sesión paga cache-write por el preamble; los turnos siguientes lo leen del caché. Los resultados de Recall llegan *después* del breakpoint de caché, así que no lo invalidan. |
| **Subagentes** (cap. 11) | Por defecto el subset de tools del subagente de research no incluye `recall` — los subagentes no deberían embarrar la memoria del padre, y darles acceso tiende a producir búsquedas off-topic. Es fácil opt-in si quieres: añade `"recall"` al subset en `registerSubagents`. |
| **Aprobación con diff** (cap. 18) | `remember` no escribe archivos del usuario, solo `.harness/`. Sin modal de diff. Si alguna vez construyes una variante de `recall` que muta archivos (e.g., actualiza un README de proyecto con aprendizajes), enchúfala por la aprobación con diff. |

## Tropiezos

**Drift del índice.** Si un archivo de sesión se renombra/borra a mano, `index.json` no lo refleja. Recall devuelve paths que no existen. El loader verifica cada path indexado al arranque y poda los que faltan — pero si borras *mientras el harness corre*, el siguiente Recall produce paths obsoletos. Mitigación barata ya hecha.

**El resumen puede estar mal.** El resumen del modelo al cierre de sesión puede perderse lo que importaba. Si la sesión fue sobre depurar un bug complicado, el resumen puede enfatizar el fix sobre el descubrimiento. La mitigación: los archivos de sesión son markdown plano. Edítalos a mano. Re-ejecuta `recall` y el contenido corregido aflora.

**El modelo no puede recordar lo que no sabe que existe.** Si tu system prompt no menciona que existe un memory store, el modelo no va a llamar a `recall` proactivamente. La lista de `/tools` lo muestra, pero el agente tiene que elegir usarlo. O bien: (a) auto-cargar suficiente en el Preamble para que el Recall explícito raramente haga falta, o (b) decirle al modelo en el system prompt que la memoria está disponible. Hacemos las dos cosas — el system prompt menciona la tool recall en su inventario, y el Preamble auto-carga las últimas 5 sesiones.

**Privacidad.** La memoria acumula todo lo que el agente consideró valioso guardar — pistas de API, paths, nombres de usuario, decisiones. Si sincronizas `.harness/` entre máquinas, compartes screenshots, o pusheas el repo sin el `.gitignore` correcto, estás filtrando. El `.gitignore` por defecto excluye `.harness/`; no lo deshagas sin pensarlo.

**El auto-summary corre síncrono al shutdown.** Una espera extra de ~2 segundos al Ctrl-D mientras el harness pide el resumen. Sorprende la primera vez. Podría hacerse en background con un mensaje "summarizing… (Ctrl-C para saltar)"; lo mantenemos síncrono porque perder el resumen pierde la memoria entera de esa sesión.

> **En el repositorio actual.** La interfaz vive en [`internal/memory/store.go`](../../internal/memory/store.go); `SessionFiles` está en [`internal/memory/sessionfiles.go`](../../internal/memory/sessionfiles.go); las tools son [`internal/tool/remember.go`](../../internal/tool/remember.go) y [`internal/tool/recall.go`](../../internal/tool/recall.go); el hook de auto-summary vive en `main.go` cerca del shutdown del programa.

## Ahora prueba

1. Ten una sesión normal y pídele al agente que recuerde algo específico: "recuerda que nuestros test fixtures viven en `testdata/golden/`". Después de Ctrl-D, mira `.harness/sessions/<hoy>.md`. El resumen debería mencionar el hecho.
2. Arranca una sesión nueva y pregunta "¿dónde viven nuestros test fixtures?". El agente debería responder desde el preamble sin llamar a ninguna tool — el hecho fue cargado con el system prompt.
3. Después de varias sesiones, pregunta "¿en qué trabajamos la semana pasada sobre compactación?". El agente debería llamar a `recall("compactación")`, recibir una lista de sesiones que matchean, decidir cuáles `read_file`, y sintetizar una respuesta.
4. Abre `.harness/sessions/<alguna-sesión-vieja>.md` y edita el resumen a mano. Arranca una sesión nueva y pregunta sobre ese tema. El contenido editado aparece.
5. Implementa `MarkdownStore` como alternativa de un archivo y cámbialo con una línea en `main.go`. El comportamiento del agente cambia — el preamble se vuelve el archivo entero, recall es grep, las mismas tools siguen funcionando.

← [volver al índice](README.md)
