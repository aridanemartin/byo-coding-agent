# 17 · Prompt caching

El harness manda cada byte de cada conversación a la API en cada turno. El capítulo 06 lo dejó explícito — la API es sin estado, el cliente lleva el historial. El visor de tokens del capítulo 16 hizo el costo visible. Este capítulo trata de recuperar parte de ese dinero.

## Qué es

Prompt caching es una optimización del lado del servidor: le pides al proveedor que recuerde el estado *ya procesado* de un prefijo estable de tu petición, para que las peticiones siguientes con el mismo prefijo se salten el trabajo de releerlo.

Mecánicamente, colocas un "breakpoint" en algún punto de tu petición. El proveedor hace hash de todo lo anterior y, en un cache hit, reusa el estado de atención cacheado. Todo lo que va *después* del breakpoint se sigue procesando desde cero.

Es el mismo patrón que el caché HTTP, aplicado a la capa del LLM.

## La economía

Tres categorías de tokens de entrada una vez activado el caching:

| Categoría | Tarifa Anthropic (Opus 4.7) | Qué significa |
|---|---|---|
| Input nuevo | $15,00 / M | Procesado desde cero |
| Cache write | $18,75 / M | Input nuevo + 25% por guardar el estado |
| Cache read | $1,50 / M | 10% del input — el hit que reusa |

El punto de equilibrio son dos lecturas — si un prefijo se reutiliza al menos dos veces dentro del TTL, ya estás ahorrando. Para un agente, donde un solo mensaje del usuario suele generar entre cinco y diez llamadas al modelo (cada tool result rebota de vuelta), un prefijo estable se reutiliza decenas de veces. El ahorro puede llegar a 70–80% de la factura de input.

OpenAI hace esto automáticamente, sin breakpoints — los prefijos comunes se detectan y se cachean por ~5–10 minutos por defecto. Menos flexibilidad, mejor ergonomía.

## Por qué los agentes ganan más que el chat

Tres fuerzas se acumulan para los agentes:

1. **Las definiciones de herramientas son enormes.** Un puñado de servidores MCP + herramientas locales son fácilmente 2–4 KB de JSON Schema por llamada. Ese schema no cambia entre turno y turno — objetivo perfecto para cachear.
2. **El bucle del agente multiplica llamadas.** Una sola petición del usuario puede disparar 10 llamadas al modelo (leer tres archivos, correr dos comandos bash, resumir). Cada vuelta del bucle manda el *mismo* system prompt + tools, más un historial ligeramente más largo. El primer ~80% de cada llamada es idéntico al primer 80% de la anterior.
3. **El system prompt crece.** Cuando `AGENTS.md` se carga (capítulo 15), el system prompt se hincha. Sin caching, pagas esos 5 KB de contexto del proyecto en *cada* llamada.

## Qué invalida el caché

El caché está indexado por una coincidencia exacta de bytes hasta el breakpoint. Cualquiera de estas cosas lo borra silenciosamente:

- Editar el system prompt.
- Editar `AGENTS.md` entre sesiones.
- Añadir o quitar una herramienta (un servidor MCP que se conecta y registra algo nuevo).
- Cambiar el id del modelo a mitad de sesión (`/model` o `/provider`).
- **La compactación** reescribiendo el historial temprano de la conversación (capítulo 07).
- Cualquier timestamp o nonce aleatorio que metas sin querer en el prefijo.

La interacción con la compactación es la peor para este harness: cada vez que `SlidingWindow` o `Summarize` corre, el frente del slice de mensajes cambia, el breakpoint pasa sobre bytes distintos, y vuelves a pagar el sobrecoste de cache write.

## Implementación en este harness

Tres capas, en orden de impacto:

### 1. El proveedor

En `internal/provider/anthropic.go`, marca el último bloque del system como un breakpoint de caché:

```go
System: []anthropic.TextBlockParam{{
    Text:         p.system,
    CacheControl: anthropic.NewCacheControlEphemeralParam(),
}},
```

Esto cachea todo hasta el final del system prompt, incluyendo las definiciones de herramientas que se envían junto a él. Con ese único cambio, cada turno después del primero lee el system prompt + tools desde el caché en lugar de reprocesarlos.

Para un caching más profundo — cachear también partes del historial de mensajes — Anthropic soporta hasta cuatro breakpoints, uno en cada bloque progresivamente más tardío. Cada breakpoint define una "capa" que puede cachearse independientemente:

```
Capa 1: system prompt + tools             (cachea una vez por sesión)
Capa 2: primeros N mensajes de usuario    (cachea por "apertura estable")
Capa 3: último turno del assistant         (cachea por turno)
```

El harness actualmente no expone breakpoints multi-capa; para la victoria de una línea, el breakpoint a nivel de system es suficiente.

### 2. El tracking de uso

Nada que hacer. `api.Usage.CacheCreationTokens` y `CacheReadTokens` ya existen (capítulo 16), `AnthropicProvider.Send` ya los rellena desde `resp.Usage`, y `/tokens` ya los imprime cuando no son cero. La tabla de precios en `anthropic.go` ya tiene las tarifas correctas. Los caños están conectados — activar el caching enciende esa columna del visor de tokens.

### 3. La interacción con la compactación

Esta es la trampa. Por defecto, el bucle del agente llama a `Compactor.Compact` cada turno. Incluso con `NoCompaction`, una sola invocación de `/compact` invalida el caché para el resto de la sesión.

Tres mitigaciones razonables:

- **No compactar por debajo del umbral.** La mayoría de las estrategias ya hacen esto — `SlidingWindow{KeepLast: 10}` es un no-op para los primeros 10 turnos. El caché sobrevive hasta que la compactación se dispara de verdad.
- **Cachear solo el prefijo que la compactación no va a tocar.** Pon el breakpoint al *final del system prompt*, no dentro del historial de mensajes. System + tools nunca cambian; todo lo que va después es terreno libre para la compactación.
- **Aceptar el tradeoff.** Cuando la compactación se dispara, el siguiente turno paga el sobrecoste de cache write. El turno siguiente, vuelves a las lecturas baratas.

La posición por defecto en este harness es la opción 2 — pones el breakpoint a nivel de system y dejas que la compactación corra libre.

## La variante de OpenAI

El proveedor de OpenAI obtiene caching automático sin cambios en la API. Chat Completions cachea implícitamente: un prefijo visto recientemente se reutiliza, y el campo `usage.prompt_tokens_details.cached_tokens` de la respuesta reporta cuántos tokens se sirvieron desde el caché.

El `OpenAIProvider` en este harness ya lee ese campo y lo mapea a `api.Usage.CacheReadTokens`. Así que cambiar a `/provider openai` y mirar `/tokens` ya muestra cache reads distintos de cero en conversaciones largas — sin más trabajo.

La filosofía de diseño de OpenAI: menos control manual, menos cosas que puedes romper.

## Tropiezos

**Olvidar que las herramientas son parte del prefijo.** Las definiciones de herramientas se envían en cada llamada como parte de la petición, y se cachean junto al system prompt. Cambiar descripciones u orden de las herramientas entre llamadas invalida el caché. Nuestro registry ordena las definiciones de herramientas por nombre (capítulo 09) para mantener estable la serialización en bytes — eso no fue casualidad.

**Sorpresas con el TTL.** El caché efímero de Anthropic vive ~5 minutos. Si dejas el harness abierto diez minutos y vuelves, el primer turno paga el sobrecoste de write otra vez. Para una sesión interactiva de programación está bien; para un job batch de larga duración puede importar.

**El cache write del primer turno parece caro.** Un usuario que abre el harness, manda un mensaje y se va, va a pagar *más* que sin caching (el 25% extra del write sin lecturas posteriores que lo amorticen). El punto de equilibrio son dos cache reads. Para sesiones interactivas casi siempre se cumple; para invocaciones one-shot de CLI no.

**Apilar breakpoints.** Cada breakpoint es una entrada de caché separada. Con cuatro breakpoints apilados, potencialmente pagas cuatro sobrecostes de write en el primer turno. Solo añade breakpoints en límites que también sean estables para muchas llamadas.

## Ahora prueba

1. Añade la línea `CacheControl: anthropic.NewCacheControlEphemeralParam()` al bloque system en `anthropic.go`. Ejecuta el harness, haz una pregunta que use varias herramientas, luego `/tokens`. El primer turno muestra cache creation tokens; los turnos siguientes muestran cache read tokens.
2. Haz una edición minúscula a `AGENTS.md` y reinicia. Escribe cualquier prompt. Observa que el cache write se vuelve a disparar — el system prompt ya no es idéntico al que estaba cacheado.
3. Ejecuta un `/compact summarize` a mitad de sesión y mira cómo el siguiente turno paga el sobrecoste de write otra vez. Confirma que el turno posterior vuelve a lecturas.

← [volver al índice](README.md)
