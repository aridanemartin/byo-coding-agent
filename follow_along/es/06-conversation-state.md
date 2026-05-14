# 06 · El estado de la conversación

Un capítulo corto, importante. Vamos a hacer explícito algo que ha sido cierto en silencio todo el tiempo: **el modelo no tiene memoria.** Cada llamada a la API manda la conversación entera desde cero.

## El invariante

```
POST /v1/messages → response
POST /v1/messages → response
POST /v1/messages → response
```

Estas tres llamadas son independientes. No hay session ID, no hay conversación del lado del servidor, no hay thread que la API recuerde entre ellas. Para mantener una conversación, el cliente re-manda todo en cada turno.

Eso pone la carga de la "memoria" sobre nosotros — el harness. Llevamos la conversación como un slice en memoria, y en cada turno le entregamos el slice entero al modelo.

## Dónde vive el slice y qué contiene

```go
var messages []api.Message
```

Eso es todo. La fuente de verdad para "qué se ha dicho". Cada turno escribe en él; cada llamada a la API lo lee.

Qué se agrega, en orden:

| Paso | Qué se agrega | Por qué |
|---|---|---|
| Envías una línea | `{Role: User, Content: [{Type: Text, Text: ...}]}` | Tu mensaje |
| El modelo responde | `{Role: Assistant, Content: resp.Content}` | La respuesta completa del modelo, incluyendo cualquier bloque `tool_use` |
| Las herramientas ejecutan | `{Role: User, Content: [{Type: ToolResult, ToolUseID: ..., ToolResult: ...}]}` | Un mensaje de rol user con todos los tool results de este turno |
| El bucle llama a Send otra vez | nada nuevo — Send re-lee el slice | El "bucle" en agentLoop está sobre `messages`, no sobre input nuevo |

Después de un solo turno que usa una herramienta, el slice tiene cuatro entradas (texto de rol user → assistant con tool_use → tool_result de rol user → assistant con texto final). El siguiente mensaje que envíes agrega la entrada cinco.

## El cableado entre tool_use y tool_result

Cada bloque `tool_use` tiene un `ToolUseID` único (p.ej. `toolu_01abc...`). Cada bloque `tool_result` debe referenciar ese mismo ID vía su campo `ToolUseID`. Pierde el enlace y la API devuelve 400 — ve un tool_result huérfano y se niega a procesar el turno.

```go
// En agent.loop, después de que el modelo emite un bloque tool_use con v.ID:
result, isErr := executeTool(v.ToolName, v.ToolInput)
toolResults = append(toolResults, Block{
    Type:       BlockToolResult,
    ToolUseID:  v.ToolUseID,   // ← el enlace
    ToolResult: result,
    IsError:    isErr,
})
```

Los IDs son tokens opacos — el modelo los asigna, nosotros los devolvemos como eco. Este emparejamiento es invisible la mayor parte del tiempo pero muy visible cuando la lógica de compactación del capítulo 07 accidentalmente divide la conversación entre un `tool_use` y su `tool_result`.

## El system prompt va aparte

Un punto sutil pero importante: el system prompt NO está en `messages`. Es un campo de nivel superior en la request de Anthropic:

```go
client.Messages.New(ctx, anthropic.MessageNewParams{
    System:   []anthropic.TextBlockParam{{Text: p.system}},  // ← nivel superior
    Messages: ...,                                            // ← solo user/assistant
    ...
})
```

Se guarda en el provider, se manda en cada request. El modelo no ve el system prompt como un "primer mensaje del usuario"; lo ve como sus propias instrucciones internas. (Otros proveedores — OpenAI en particular — empaquetan el system prompt dentro del array de messages con `role: "system"`. La abstracción de provider del capítulo 03 esconde esta diferencia.)

## Dos consecuencias

### `/clear` es una línea

`messages = messages[:0]` vacía el slice. La siguiente llamada a la API manda solo el system prompt — sin turnos previos. El modelo no tiene memoria de nada que pasó antes. Lo vimos en el capítulo 05 y ahora sabemos por qué funciona.

### El costo crece linealmente con la longitud de la conversación

Cada turno re-manda cada turno previo a través de la tokenización. Para el turno 10 estás pagando por reprocesar los mismos 9 turnos de contexto. Para el turno 50, estás pagando *un montón*.

Hay dos maneras de manejar esto:

1. **Prompt caching.** Decirle a la API "este prefijo es estable, cachealo". Las requests subsiguientes pagan ~10% por la porción cacheada. Acá no lo usamos, pero es la respuesta de producción.
2. **Compactación.** Cuando la conversación se hace larga, resume la parte vieja y reemplázala con el resumen. El capítulo 07 está dedicado a esto.

Ambas son respuestas *del lado del cliente* a una realidad *del lado del servidor*. El modelo sigue sin tener memoria; solo mandamos menos contexto.

## Por qué la compactación del lado del servidor no es la respuesta acá

Anthropic ofrece un header beta `compact-2026-01-12` — la API resume los turnos anteriores automáticamente cuando el contexto se acerca a su límite. Está bueno. No lo usamos.

Razón: este es un harness de aprendizaje cuya abstracción de provider está pensada para generalizar. La compactación del lado del servidor es un feature solo de Anthropic. Si dependiéramos de ella, cambiar a OpenAI o a un provider mock silenciosamente perdería la compactación. La compactación del lado del cliente funciona contra cualquier backend sin cambios.

Este es un patrón recurrente en la ingeniería de harness: cuando hay un feature disponible "gratis" del provider, la pregunta es si usarlo filtra suposiciones específicas del provider al harness. A veces sí (búsqueda web), a veces no (esta).

## Tropiezos

**El "efecto del segundo sistema" sobre el contexto.** Una vez que entiendes la falta de estado, es tentador sobre-diseñar — construir un context manager fancy, un vector store, un sistema de retrieval. Aguanta. El slice de conversación entero está bien para la mayoría de los casos de uso de un agente de código. La compactación (capítulo 07) maneja el caso de long-tail.

**Modificar `messages` mientras iteras.** No lo hagas. El bucle del agente lee y agrega; eso es seguro en código de una sola goroutine. Si alguna vez lanzas goroutines que tocan `messages`, necesitas un mutex o un channel. Todavía no; el capítulo 11 nos deja cerca de necesitar uno.

**Olvidar agregar el turno del assistant.** Un bug común: manejar los bloques tool_use pero olvidarse de agregar también la respuesta del modelo a `messages`. La siguiente llamada a la API no tendría registro de lo que dijo el modelo, y la API tiraría 400 sobre los tool_results huérfanos.

> **En el repo actual.** Para el capítulo 11 el slice `messages` se movió de un global a nivel de paquete a un campo en el struct `Agent` en [`internal/agent/agent.go`](../internal/agent/agent.go) — mira el campo `messages []api.Message` y los métodos `Send`/`Messages`/`SetMessages`/`ClearMessages` que lo rodean. La historia de la falta de estado no cambia; solo está owneada por un struct en lugar de por `main`. Eso es lo que nos deja tener múltiples agentes (root + subagentes) al mismo tiempo.

## Ahora prueba

1. Después de unos cuantos turnos, vuelca `messages` a JSON (`json.MarshalIndent(messages, "", "  ")` a stdout) y lee la estructura. Confirma el patrón alternado user/assistant y los emparejamientos tool_use/tool_result.
2. Cronometra cuánto tarda la llamada a la API en el turno 1 vs el turno 20. La mayor parte del tiempo extra es procesar el prefijo más largo — siente el costo lineal.
3. Edita `messages` manualmente a mitad de conversación para borrar una respuesta vieja del assistant. Manda un mensaje nuevo. ¿Qué pasa? (Puede tirar 400 si rompiste un par tool_use/result.)

Siguiente: [07 · Estrategias de compactación](07-compaction-strategies.md).
