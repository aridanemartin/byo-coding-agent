# Ejercicio 3 — Backend de memoria intercambiable

**Objetivo:** implementar un segundo `memory.Store` y demostrar que el resto del harness no nota la diferencia.

**Dificultad:** media. **Tiempo:** 1–2 horas. **Toca:** `internal/memory/`, `main.go`.

## Lo que ya está en el repo

`internal/memory/store.go` define la interfaz `Store` de tres métodos — `Save`, `Recall`, `Preamble`. La implementación por defecto, `SessionFiles` en `internal/memory/sessionfiles.go`, escribe un archivo markdown por sesión bajo `.harness/sessions/` y mantiene un índice JSON para búsquedas rápidas.

La interfaz es pequeña a propósito. Sustituirla es la prueba: ¿entiendes lo bastante el contrato como para cambiar el almacenamiento sin romper el bucle del agente, las herramientas `remember`/`recall` o el preamble del system prompt?

## Lo que vas a construir

Elige *uno* de estos backends y lo implementas como hermano de `SessionFiles`:

- **`InMemoryRing{Capacity int}`** — un ring buffer que guarda las últimas N entradas. Sin disco; se resetea al reiniciar. Te obliga a pensar qué significa Recall sin persistencia.
- **`SQLite{Path string}`** — una fila por Entry, con FTS5 para Recall. Te obliga a tomar decisiones reales de esquema y de lenguaje de consulta.
- **`JSONBlob{Path string}`** — un único archivo JSON reescrito en cada Save. La opción persistente más simple; te obliga a confrontar la semántica de escrituras concurrentes.

## Pasos sugeridos

1. **Crea el archivo.** `internal/memory/your_backend.go`. Mismo paquete; misma superficie de imports que `SessionFiles`.

2. **Implementa los tres métodos.** Save devuelve `nil` cuando la entrada está durable (para stores no persistentes, "durable" = en el slice). Recall hace una búsqueda best-effort — el match por substring es aceptable; la interfaz no promete búsqueda semántica. Preamble devuelve el preface siempre cargado; normalmente construido a partir de entradas `KindSessionSummary`.

3. **Escribe tests.** Un test de round-trip para backends persistentes (Save y luego Recall devuelve la entrada). Un test de capacidad para el ring buffer (el Save N+1 expulsa el más viejo). Espeja `internal/memory/sessionfiles_test.go`.

4. **Conéctalo en `main.go`.** Sustituye la línea que construye `SessionFiles` por tu nuevo backend, detrás de una variable de entorno o una flag si quieres soportar selección en runtime.

5. **Verifica que el agente no cambia.** `remember "x"`, `recall x`, reinicia, y comprueba el preamble. Ningún código existente en `internal/agent` ni `internal/tool` debería tocarse.

## Aceptación

- Las herramientas `remember` y `recall` funcionan de forma transparente contra el nuevo backend.
- Los tests existentes en `internal/agent/` e `internal/tool/` pasan sin modificación.
- Nuevos tests en `internal/memory/your_backend_test.go` cubren el comportamiento específico del backend (expulsión, persistencia, concurrencia).

## Extra

- Un adaptador `FanoutStore` que escriba a dos stores a la vez (p. ej., session files + sqlite para query). Demuestra que la interfaz compone.
- Un comando de migración (`/memory migrate sqlite`) que copie entradas de un backend a otro.
- Un benchmark: ¿qué tan lento es Recall con 10k entradas? Usa `testing.B` de Go para averiguarlo antes de optimizar.
