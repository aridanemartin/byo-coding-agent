# 18 · Diff approval for writes

Chapter 02 introduced the permission gate: every tool call gets a `approve? [y/n]`. That's fine for `bash ls`. It's terrifying for `write_file` — what you're approving is "the agent wants to write *something*," and you can't see what until you say yes and read the file after the fact.

This chapter is about closing that gap. Before any `write_file` actually touches disk, the harness shows you the *exact* diff the agent is proposing, in a modal you can scroll through. One y/n on the whole thing — no hunk-by-hunk approval, no editor — just enough information to make the y/n meaningful.

## The insight that makes it possible

The model never sees a diff. It sends a `tool_use` block:

```json
{
  "type": "tool_use",
  "name": "write_file",
  "input": { "path": "main.go", "content": "package main\n\nfunc main() { …" }
}
```

That block arrives at our harness *before* anything runs. The agent loop has every argument the model wants to use, in full, sitting in memory. The path. The full proposed content. We just have to read the *current* file on disk and compute the difference. The diff is a UX layer between the model's intention and the disk's bytes — invisible to the model, hugely useful to the user.

This is one of the load-bearing decisions of harness engineering generally: **all the model's actions are mediated by us.** Anything we can compute from the tool's arguments — a diff preview, a dry-run summary, a token-cost estimate — we can show to the user *before* the action happens. Diff approval is the most useful instance, but the pattern generalizes (see "Where else this works" below).

## Three pieces, one flow

### 1. Build the diff

`internal/agent/diff.go` has one exported job: take the raw JSON the model sent for a `write_file` call and produce a unified-diff string the user can read.

```go
func buildWriteDiff(rawInput string) string {
    var in struct{ Path, Content string }
    json.Unmarshal([]byte(rawInput), &in)

    existing, err := os.ReadFile(in.Path)
    if err != nil {
        return synthesizeNewFileDiff(in.Path, in.Content)
    }
    diff := difflib.UnifiedDiff{
        A:        difflib.SplitLines(string(existing)),
        B:        difflib.SplitLines(in.Content),
        FromFile: in.Path + " (current)",
        ToFile:   in.Path + " (proposed)",
        Context:  3,
    }
    text, _ := difflib.GetUnifiedDiffString(diff)
    return text
}
```

Three cases get special handling:

- **File exists, content differs.** Standard unified diff, three lines of context, `+`/`-` markers per line.
- **File doesn't exist yet.** Synthesized prelude (`--- /dev/null` / `+++ path (new file)`) so the whole body renders as additions. Without this, `go-difflib` would just emit the raw content with no markers and the modal would look like plain text, not a diff.
- **Identical content.** The model occasionally asks to "write" a file that's byte-for-byte what's already there. Returns a "(no changes)" marker — the modal still opens with a clear message instead of falling back to the generic `approve?` prompt and confusing the user about what they just denied.

### 2. Plumb it through the approval channel

`Agent.Confirm` used to be `func(prompt string) bool`. The change is one parameter:

```go
Confirm func(prompt, detail string) bool
```

`detail` is optional long-form content. For every tool except `write_file`, the agent passes `""` and the old flow is preserved. For `write_file`, the agent calls `buildWriteDiff(rawInput)` and passes the result:

```go
prompt, detail := "approve?", ""
if name == "write_file" {
    if d := buildWriteDiff(rawInput); d != "" {
        detail = d
        prompt = "approve write to " + path + "?"
    }
}
if a.Confirm != nil && !a.Confirm(prompt, detail) {
    return "user denied this tool call", true
}
```

That's a controlled signature change — there's one caller in the codebase (the TUI wiring in `main.go`), so we update it and move on. Backward compat would be an interface with a default-empty optional, but for an internal API one caller deep it's not worth the abstraction.

### 3. Render the modal

The TUI's `ApprovalRequest` message gains a `Detail string` field. When it's non-empty, the handler:

1. Sets the harness state to `stateAwaitingApproval` (same as before).
2. Calls `layout()` to resize the existing `debugView` viewport to modal dimensions.
3. Pipes the diff through `HighlightPayload(detail, width)` and sets it as the viewport content.

`HighlightPayload` was already detecting JSON (chapter 16). We taught it to recognize unified diffs too — a leading `--- ` is enough — and pass it to Chroma's `diff` lexer. From there, `+` lines come out green, `-` lines red, `@@` headers in another color, all for free.

`View()` checks for the approval-with-detail state and routes to `viewApprovalModal()`, structurally identical to the debug detail modal from chapter 12: centered, full-screen, title + separator + scrollable body + hint line. The only differences are a yellow border (instead of cyan) to make it unmistakably a "you need to decide" state, and a different hint line (`y / n` instead of `esc / tab`).

When the user presses y/n, the answer goes back through the reply channel, the state returns to `stateRunning`, and `layout()` runs again to restore the normal panel/viewport split before the next render.

## Edge case: what counts as "write_file"

The detection in `agent.executeTool` is a string compare: `if name == "write_file"`. That's the local tool's name. **It won't trigger for MCP-backed write tools** like `filesystem_write_file` (if you wire the [filesystem MCP server](14-mcp-support.md)) or any other tool from an external server that happens to write files.

That's a deliberate scope choice: the harness can build a diff for `write_file` because it knows its argument schema (`path` + `content`). For MCP tools we'd have to inspect their schemas at runtime, parse `path`-shaped arguments, and hope the server's semantics actually match "overwrite a file." Doable but bigger.

The conservative version we shipped: only the local `write_file` gets the diff treatment; MCP write tools still go through the plain `approve?` prompt with no preview. Users who want preview for MCP writes can either prefer the local tool or wait for a generalization that introspects schemas.

## Where else this pattern works

Diff approval is one instance of a general pattern: **synthesize a preview from the tool's arguments and show it before execution.** Other tools where this would work in this harness:

| Tool | Preview |
|---|---|
| `bash` | Run with the shell's `-n` flag (syntax-check only), or `--dry-run` style flag if the command has it (`rm -i`, `rsync -n`). |
| `delegate_research` | List the curated tool subset the subagent will have access to + the system prompt it'll run under. |
| Any MCP tool | The tool's JSON-schema description + the arguments. Less actionable than a diff, more actionable than nothing. |

The pattern is always the same: in `Agent.executeTool`, after the model emits the `tool_use` block but before we dispatch, we have an opportunity to materialize what's about to happen. The richer the preview, the more meaningful the user's y/n.

## Pitfalls

**The diff captures the file as it was when the model decided.** If you edit the file in another terminal between the model's `tool_use` and your y/n, the proposed content gets applied to your edits, not to what the diff shows. There's no live update — the diff is a snapshot at approval time. For an interactive coding session this is almost always fine; for batch/automation, be aware.

**Binary files render as gibberish.** `go-difflib` is line-oriented and assumes text. Asking the model to write a PNG would surface a diff of byte-encoded mess. We don't try to detect binary content; in practice the model very rarely asks to write binaries from inside this harness.

**Large files balloon the diff payload.** A 50 KB file with one changed line still produces a small diff (just the changed hunk + 3 lines context). A 50 KB rewrite produces a 50 KB diff. The modal scrolls, but the user has to read more. No automatic summarization; we leave it visible.

**Encoding mismatches.** If the file on disk is UTF-16 or any non-UTF-8 encoding, `os.ReadFile` returns the bytes verbatim and `difflib` line-splits on `\n` literally. The diff will look strange but won't crash. The `write_file` tool itself writes UTF-8 unconditionally — if the model is "editing" a UTF-16 file, accepting the write *will* clobber its encoding.

**The model never learns about the diff.** Whether you approve or deny, the model gets back the standard tool result string — either the success/content from `write_file` or `"user denied this tool call"`. It never sees the diff itself. This is intentional (chapter 06's statelessness boundary) but worth knowing: explaining to the model "I rejected this because of X" requires you to type that in the next turn.

> **In the current repo.** The diff helper is [`internal/agent/diff.go`](../../internal/agent/diff.go). The Confirm signature lives in [`internal/agent/agent.go`](../../internal/agent/agent.go). The modal renderer and message-routing changes are in [`internal/ui/program.go`](../../internal/ui/program.go) — search for `viewApprovalModal` and `approvalDetail`. Chroma's diff lexer is wired in [`internal/ui/highlight.go`](../../internal/ui/highlight.go) via the `detectLanguage` helper.

## Now try

1. Ask the agent to create a small file (`write a haiku.txt with a haiku`). Watch the modal show `--- /dev/null` / `+++` and all-green additions. Approve. Compare the resulting file to the diff.
2. Ask it to modify the same file (`rewrite haiku.txt with a different haiku`). The modal now shows the unified diff against the existing content — `-` lines for what's leaving, `+` lines for what's arriving, context lines unchanged.
3. Deny a write (press `n`). The agent gets back `"user denied this tool call"` — read its next text response to see how it reacts. Usually it'll ask you what you'd prefer.
4. Try a refactor: ask the agent to rename a function across one file. Notice that the modal makes it obvious whether the renaming is correct *before* it touches disk — that's the whole point.

← [back to TOC](README.md)
