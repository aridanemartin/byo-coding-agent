# Ejercicio 6 — Entradas con imágenes

**Objetivo:** dejar que el usuario adjunte imágenes a un turno para que el modelo pueda describir capturas, leer diagramas, hacer OCR a una foto, o razonar sobre una UI.

**Dificultad:** media. **Tiempo:** 2–3 horas. **Toca:** `internal/api/types.go`, `internal/provider/`, `commands.go`, `internal/ui/`.

## Lo que ya está en el repo

Los tipos de mensaje agnósticos al provider en `internal/api/types.go` solo conocen tres tipos de bloque: `BlockText`, `BlockToolUse`, `BlockToolResult`. Tanto el provider Anthropic como el OpenAI los traducen a/desde sus SDKs en `internal/provider/anthropic.go` e `internal/provider/openai.go`. La UI acepta texto plano del usuario y renderiza bloques de texto o de herramienta de vuelta.

Hoy no hay forma de enviar una imagen. Los dos backends lo soportan — Anthropic vía `ImageBlockParam` (fuente base64 o URL), OpenAI vía la forma de contenido de visión — el harness simplemente no lo expone.

## Lo que vas a construir

Un camino para que el usuario adjunte una o más imágenes al siguiente mensaje, con los providers traduciéndolas correctamente.

## Pasos sugeridos

1. **Extiende el modelo de bloque.** Añade `BlockImage` a las constantes `BlockType` y los campos que necesita:

   ```go
   const BlockImage BlockType = "image"

   type Block struct {
       // ... campos existentes ...

       // BlockImage
       ImageSource    string // "base64" o "url"
       ImageMediaType string // "image/png", "image/jpeg", "image/webp", "image/gif"
       ImageData      string // payload base64 O la URL, según Source
   }
   ```

   No metas esto a la fuerza en `Text`. Mantener los campos separados hace que la traducción en los providers sea obvia.

2. **Enseña a Anthropic a enviarla.** En `anthropic.go`, cuando recorres los bloques del mensaje para construir los params del SDK, mapea `BlockImage` a `anthropic.NewImageBlock` (o al constructor base64/URL según tu versión del SDK). El media type es obligatorio; rechaza vacío/no soportado.

3. **Enseña a OpenAI a enviarla.** La entrada de visión de OpenAI usa el array `content` con objetos `{"type": "image_url", "image_url": {"url": "..."}}`. Para entradas base64, codifica como `data:<media-type>;base64,<payload>` y pásalo por el mismo campo. Documenta el truco de que no todos los modelos OpenAI soportan visión — devuelve un error amigable si el modelo activo no.

4. **Añade los comandos `/attach` y `/clear-attach`.** El patrón de slash command está en `commands.go`. `/attach ~/Desktop/screenshot.png` lee el archivo, detecta el MIME type (`http.DetectContentType` con los primeros 512 bytes basta), lo codifica en base64 y prepara un `BlockImage` para anteponerlo al *siguiente* mensaje del usuario. `/clear-attach` descarta lo preparado. `/attach` sin argumentos lista lo que hay preparado.

5. **Conecta lo preparado en el envío.** Cuando el usuario envía un turno, el bucle del agente antepone los bloques de imagen preparados al bloque de texto. Tras el envío, limpia el área de preparación.

6. **Renderiza el adjunto en la TUI.** No intentes dibujar la imagen — la mayoría de terminales no pueden. Renderiza un indicador de una línea: `[imagen: screenshot.png · png · 248KB]`. El renderizador de transcripción (Ejercicio 4 si lo hiciste) también necesita conocer este bloque.

7. **Valida.** Rechaza archivos de más de ~5 MB (el cap por imagen de Anthropic) con un error claro. Rechaza MIME types no soportados de entrada.

## Aceptación

- `/attach demo.png` seguido de `describe esta imagen` envía tanto la imagen como el texto al modelo en un único turno, y la respuesta hace referencia a lo que hay en la imagen.
- El mismo flujo funciona tras `/provider openai gpt-4o` (o cualquier modelo OpenAI con visión).
- Un MIME no soportado o un archivo demasiado grande imprime un error claro antes del envío — sin viaje al SDK.
- La transcripción muestra `[imagen: …]` en lugar del binario, y las herramientas que recorren el contenido del mensaje (compactación, summarize, save) no petan con el nuevo tipo de bloque.

## Extra

- Drag-and-drop en la TUI: detecta cuando la entrada es un URI/path de archivo y autopreparar.
- Una herramienta `screenshot` que capture la pantalla en macOS (`screencapture -i -c` + lectura del clipboard) y prepare el resultado.
- Formato URL: `/attach https://example.com/foo.png` — Anthropic acepta URLs directamente; OpenAI también vía `image_url.url`. Sálta el encode a base64 en esa ruta.
- Imágenes de salida: algunos modelos pueden devolver imágenes generadas. Round-trippéa un bloque de imagen generada de vuelta a la transcripción y escríbela al disco.
