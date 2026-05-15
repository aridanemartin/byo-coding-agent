# Exercise 6 — Image inputs

**Goal:** let the user attach images to a turn so the model can describe screenshots, read diagrams, OCR text from a photo, or reason about a UI.

**Difficulty:** medium. **Time:** 2–3 hours. **Touches:** `internal/api/types.go`, `internal/provider/`, `commands.go`, `internal/ui/`.

## What's already in place

The provider-agnostic message types in `internal/api/types.go` only know about three block kinds: `BlockText`, `BlockToolUse`, `BlockToolResult`. Both Anthropic and OpenAI providers translate these to/from their SDKs in `internal/provider/anthropic.go` and `internal/provider/openai.go`. The UI accepts plain text from the user and renders text or tool blocks back.

There's no way today to send an image. Both backends support it — Anthropic via `ImageBlockParam` (base64 or URL source), OpenAI via the vision content shape — the harness just doesn't expose it.

## What to build

A path for the user to attach one or more images to the next message, with the providers translating them correctly.

## Suggested steps

1. **Extend the block model.** Add `BlockImage` to the `BlockType` constants and the fields it needs:

   ```go
   const BlockImage BlockType = "image"

   type Block struct {
       // ... existing fields ...

       // BlockImage
       ImageSource    string // "base64" or "url"
       ImageMediaType string // "image/png", "image/jpeg", "image/webp", "image/gif"
       ImageData      string // base64 payload OR the URL, depending on Source
   }
   ```

   Don't shoehorn this into `Text`. Keeping the fields separate makes provider translation obvious.

2. **Teach Anthropic to send it.** In `anthropic.go`, when you walk message blocks to build the SDK params, map `BlockImage` to `anthropic.NewImageBlock` (or the base64/URL constructor for your SDK version). The media type is required; reject empty/unsupported.

3. **Teach OpenAI to send it.** OpenAI's vision input uses the `content` array with `{"type": "image_url", "image_url": {"url": "..."}}` objects. For base64 inputs, encode as `data:<media-type>;base64,<payload>` and pass through the same field. Document the gotcha that not every OpenAI model supports vision — surface a friendly error if the active model doesn't.

4. **Add `/attach` and `/clear-attach` commands.** Slash command pattern is in `commands.go`. `/attach ~/Desktop/screenshot.png` reads the file, sniffs the MIME type (`http.DetectContentType` on the first 512 bytes is enough), base64-encodes it, and stages a `BlockImage` to be prepended to the *next* user message. `/clear-attach` drops staged attachments. `/attach` with no args lists what's staged.

5. **Wire the staged attachments into the send path.** When the user submits a turn, the agent loop prepends staged image blocks to the text block. After send, clear the staging area.

6. **Render the attachment in the TUI.** Don't try to draw the image — most terminals can't. Render a one-line indicator: `[image: screenshot.png · png · 248KB]`. The transcript renderer (Exercise 4 if you've done it) needs to know about this block too.

7. **Validate.** Reject files larger than ~5 MB (Anthropic's per-image cap) with a clear error. Reject unsupported MIME types up front.

## Acceptance

- `/attach demo.png` followed by `describe this image` sends both the image and text to the model in one turn, and the response references what's in the image.
- The same flow works after `/provider openai gpt-4o` (or any vision-capable OpenAI model).
- An unsupported MIME type or an oversize file prints a clear pre-send error — no SDK round-trip.
- The transcript shows `[image: …]` in place of the binary, and tools that walk message content (compaction, summarize, save) don't crash on the new block kind.

## Stretch

- Drag-and-drop in the TUI: detect when the input is a file URI / path and auto-stage it.
- A `screenshot` tool that captures the screen on macOS (`screencapture -i -c` + clipboard read) and stages the result.
- A URL form: `/attach https://example.com/foo.png` — Anthropic accepts URLs directly; OpenAI does too via `image_url.url`. Skip the base64 encode in that path.
- Image outputs: some models can return generated images. Round-trip a generated image block back into the transcript and write it to disk.
