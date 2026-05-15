# Exercise 4 — Pluggable transcript renderer

**Goal:** turn the single `RenderTranscript` function into a `Renderer` interface with multiple implementations, and add an `/export` command.

**Difficulty:** medium. **Time:** 1–2 hours. **Touches:** `internal/api/`, `commands.go`.

## What's already in place

`internal/api/types.go` has a `RenderTranscript([]Message) string` function used by the summarising compaction strategy and the compaction-log decorator. It's a single hardcoded format — fine for those internal uses, but useless if you want to export a finished session as something a human or another tool can read.

The harness has four other extension points that all follow the same shape: small interface + Default implementation + room for others to plug in. Transcripts are the obvious missing one.

## What to build

A `Renderer` interface, two or three implementations, and a slash command:

```go
type Renderer interface {
    Render(io.Writer, []Message) error
}
```

- `TerminalRenderer{}` — the current text format, factored out of `RenderTranscript`.
- `MarkdownRenderer{}` — H2 per turn (`## user`, `## assistant`), fenced blocks for tool input/output, inline-code for tool names.
- `JSONRenderer{}` — round-trippable; tests can load it back.
- (Optional) `HTMLRenderer{}` — share Goldmark with `cmd/sitegen`.

## Suggested steps

1. **Promote `RenderTranscript` to a method.** Move the existing logic into `TerminalRenderer.Render` in a new `internal/api/render.go`. Keep `RenderTranscript` as a thin wrapper so callers don't break:

   ```go
   func RenderTranscript(msgs []Message) string {
       var b strings.Builder
       _ = (TerminalRenderer{}).Render(&b, msgs)
       return b.String()
   }
   ```

2. **Implement markdown and JSON.** Both are pure functions over `[]Message`. The hardest part is deciding how to format `tool_use` and `tool_result` blocks — pick something readable, not necessarily faithful to the wire format.

3. **Add the slash command.** Following the pattern in `commands.go`, register `/export <format> [path]`:

   - `/export markdown` → prints to stdout
   - `/export markdown ./session.md` → writes to file
   - Default format is markdown if omitted

4. **Test round-trip for JSON.** A small test that serialises a known transcript and deserialises it back to an equivalent `[]Message`.

## Acceptance

- `/export markdown ./out.md` produces a readable markdown file with one section per turn and code-fenced tool blocks.
- `/export json ./out.json` round-trips: a loader reads the JSON back into `[]Message` and the result equals the original.
- `compact.Summarize` and `compact.WithLogging` (which both call `RenderTranscript`) still work — the wrapper preserved their behaviour.
- Existing tests pass.

## Stretch

- An `/import <path>` command that reads a JSON export back into the active session.
- A streaming renderer that writes incrementally during the agent loop (useful for long sessions where you want progressive output).
- An HTML renderer that produces a standalone file with the gruvbox code theme matching the website.
