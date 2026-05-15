# Ejercicio 1 — Reintento de herramienta tras error

**Objetivo:** cuando una llamada a una herramienta falla, dale al modelo uno o dos reintentos estructurados antes de devolver el error al usuario.

**Dificultad:** fácil. **Tiempo:** 30–60 minutos. **Toca:** `internal/agent/agent.go`.

## Lo que ya está en el repo

El bucle del agente en `internal/agent/agent.go` ya enruta los errores de herramienta de vuelta al modelo: la salida de la herramienta se envuelve en un `api.Block` con `IsError: true`, y el modelo decide qué hacer a continuación. Eso es flexible — pero nada impide que el modelo llame a la misma herramienta rota con la misma entrada rota en bucle cerrado.

Puedes leer las líneas relevantes alrededor de `toolResults` e `IsError` en `internal/agent/agent.go:107–137`.

## Lo que vas a construir

Una `RepairPolicy` pequeña que:

- cuente los fallos consecutivos de la misma herramienta (mismo nombre, idealmente mismo hash de entrada),
- en cada reintento, inyecte una meta-nota corta en el resultado de la herramienta como *"este es el reintento 2/3; si no puedes recuperarte, pregunta al usuario"*,
- tras `MaxRetries`, aborte el despacho de la herramienta y muestre un error claro al usuario.

## Pasos sugeridos

1. **Define la política.** En `internal/agent/` (o en un nuevo paquete `internal/repair/`):

   ```go
   type RepairPolicy struct {
       MaxRetries int
       Note       func(toolName string, attempt, max int) string // devuelve la meta-nota
   }
   ```

2. **Lleva el estado en el agente.** Añade un campo `repairCounts map[string]int` en `Agent`. Resetea las entradas cuando una herramienta tenga éxito o cuando el modelo cambie a otra herramienta o entrada.

3. **Conéctalo en el bucle.** En el bloque de despacho de herramientas donde se pone `IsError`, consulta la política. O añades la nota al contenido del resultado, o incrementas el contador y abortas.

4. **Hazlo enchufable en main.go.** Espeja los otros puntos de extensión:

   ```go
   a.Repair = &agent.RepairPolicy{MaxRetries: 3}
   ```

   Una política `nil` significa "comportamiento actual, reintentos ilimitados".

## Aceptación

- Un comando `bash` que falla vuelve al modelo con una nota `retry 1/3`.
- El mismo comando fallando otra vez recibe `retry 2/3`, y aborta en 3 con un único mensaje de error visible al usuario.
- El límite es configurable desde `main.go`.
- Un test basado en `MockProvider` (mira `internal/provider/mock.go`) confirma que el abort ocurre en el reintento configurado, sin llamadas reales a la API.

## Extra

- Reporta estadísticas de reparación en el panel `/debug` (cuenta de reparaciones por nombre de herramienta).
- Políticas distintas por herramienta: `bash` con 1 reintento, `read_file` con 3.
- Hashea la entrada de la herramienta para que *"misma herramienta, argumento distinto"* no cuente como reintento.
