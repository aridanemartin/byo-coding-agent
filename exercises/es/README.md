# Ejercicios

Seis ejercicios de extensión para el harness, en orden aproximado de dificultad. Cada uno se apoya en uno de los puntos de extensión existentes (Provider, Tool, CompactionStrategy, Store, Subagente) y te obliga a tomar una decisión arquitectural real — no es un cambio mecánico.

Lee el capítulo correspondiente en `follow_along/` primero; estos archivos no vuelven a explicar el *por qué*.

| # | Ejercicio | Capa | Dificultad |
|---|---|---|---|
| 1 | [Reintento de herramienta tras error](01-tool-retry-on-error.md) | Bucle del agente | fácil |
| 2 | [Subagentes definidos en markdown](02-markdown-subagents.md) | Subagentes | media |
| 3 | [Backend de memoria intercambiable](03-pluggable-memory.md) | Memoria | media |
| 4 | [Renderizador de transcripción intercambiable](04-transcript-renderer.md) | Transcripción | media |
| 5 | [Respuestas en streaming](05-streaming-responses.md) | Provider + UI | difícil |
| 6 | [Entradas con imágenes](06-image-inputs.md) | Tipos API + Provider | media |

Cada archivo tiene un objetivo, un recordatorio de lo que ya está en el repo, pasos sugeridos con rutas concretas y criterios de aceptación. La sección "Extra" al final es opcional — tómala si el ejercicio base se te quedó corto.

¿Terminaste uno? Abre un PR. No hay solución canónica; el valor está en las decisiones que tomes en el camino.
