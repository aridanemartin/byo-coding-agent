# 19 · Agent memory

The agent today resets to zero every session. The `messages` slice is empty when `main` starts; the system prompt is whatever you compiled with; nothing the agent learned yesterday survives. Chapter 06 made that statelessness explicit on the API side — each call ships the whole history. The harness *mirrored* that statelessness on the client side, and every session became an island.

For a one-shot CLI tool that's fine. For an agent you come back to every day, it's a recurring friction:

- You explain "our test fixtures live in `testdata/golden/`" three sessions in a row.
- You teach formatting preferences every time.
- It re-reads the same files because it doesn't remember it already saw them.
- "Continuing yesterday's work" means re-explaining yesterday's context.

This chapter adds memory: a persistent layer the agent reads at startup and writes to over time. Same shape as every other extension point in this book — a small interface, a default implementation, one line in `main.go` to swap.

## The Store interface

Three operations, deliberately three:

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

The split between `Recall` and `Preamble` is the load-bearing decision. Not all memory has the same value-to-token ratio:

- **Preamble** — always loaded into the system prompt at session start. Pays tokens on every turn forever, but the agent has it without thinking. Use for high-value, low-volume facts: "user is Martin", "this project uses snake_case", "the codebase calls X the dashboard."
- **Recall** — a tool call. Cheap when not invoked; expensive when it is, because the result lands in the conversation as a tool result. Use for high-volume, situational memory: past session summaries, decisions on specific bugs, things the agent might want to fetch if relevant.

A simpler interface with just `Save` + `Snapshot() string` works for tiny memories — and that's `MarkdownStore`, an alternative implementation we ship — but it doesn't scale. By turn 50 of session 30, the snapshot is bigger than the conversation it precedes. The 3-method split lets implementations decide what to surface eagerly vs lazily.

## SessionFiles: the default

The default implementation is `SessionFiles`. Layout:

```
.harness/
├── sessions/
│   ├── 2026-05-13-09h47.md
│   ├── 2026-05-14-22h11.md
│   └── 2026-05-15-08h00.md
└── index.json
```

Each session file is human-readable markdown — the kind of thing you can browse in any editor, diff in git, or `cat` from a shell:

```markdown
# 2026-05-15 08:00
tags: refactor, compaction, debug-panel
duration: 47 minutes · 23 turns · $0.4729

## Summary

Worked on the compaction strategy comparison in chapter 07. Discovered
that SafeSplitPoint mis-handles consecutive tool_use blocks from the same
turn — needs a regression test.

## Decisions
- Renamed `compact.Verbose` to `compact.Logging`
- Added DedupeReads as a new strategy

## Open threads
- Regression test for consecutive-tool_use case (not yet written)
```

`index.json` is the fast-lookup layer over those files:

```json
{
  "sessions": [
    {
      "path": "sessions/2026-05-13-09h47.md",
      "date": "2026-05-13T09:47:00Z",
      "summary": "Set up MCP integration with the deepwiki server …",
      "tags": ["mcp", "deepwiki", "tools"]
    }
  ]
}
```

### How the three methods are implemented

**`Save(ctx, entry)`** during a session: append the entry to a draft markdown held in memory. On session close (or on demand), the draft gets flushed to `sessions/<date>.md` and a new record is inserted into `index.json`.

**`Recall(ctx, query, limit)`**: load `index.json`, filter to entries where any tag matches `query` or the summary contains it (case-insensitive substring), return up to `limit` entries. The agent then chooses which session files (if any) to read in full via `read_file`. Recall doesn't read the bodies — it returns metadata.

**`Preamble(ctx)`**: returns the last 5 sessions' summaries (configurable) concatenated, plus their dates and tags. Bounded size — recent context only. The number is chosen so the typical preamble stays under ~1 KB, small enough that prompt caching (chapter 17) absorbs it cleanly.

## Two tools enchufed in

Two new tools in `internal/tool/` self-register via `init()` (chapter 09) and use `memory.Default`:

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

`recall(query, limit)` follows the same shape, calling `memory.Default.Recall`. Both are exactly analogous to how `delegate.go` uses `subagent.Default`.

## Wiring in `main.go`

Three lines plus a deferred close. Same shape as MCP setup (chapter 14):

```go
mem, err := memory.NewSessionFiles(".harness")
if err != nil { /* log, fall back to NoMemory */ }
memory.Default = mem
defer mem.Close()  // flushes the in-progress session draft to disk

sysPrompt := systemPrompt + loadAgentsContext() + mustPreamble(mem)
```

`loadAgentsContext` (chapter 15) was already concatenating human-written project context; now `mustPreamble(mem)` adds agent-written memory. They compose: AGENTS.md is what *you* told the agent about the project, the preamble is what the *agent learned*. Both prepend cleanly; both are stable prefixes (prompt-caching-friendly).

## End-of-session summarization

The big design question: how does a session actually get summarized?

| Option | How | Tradeoff |
|---|---|---|
| **Agent decides mid-session** | The agent calls `remember(...)` whenever something feels worth keeping | Matches what mattered *semantically* — but the agent forgets to do it more than half the time |
| **Auto-summary at close** | On Ctrl-D, the harness sends one final synthetic prompt: "summarize this session in a paragraph + 3-5 tags" and writes the result | Consistent and automatic. Costs ~$0.001–0.01 extra per session for the summary call. |

We do **auto-summary at close** by default. It's the more reliable path. Users who want agent-driven memory can disable the hook and rely on explicit `remember(...)` calls; the architecture supports both.

The hook lives in `main`'s shutdown defer: after `program.Run()` returns, if `mem != nil` and the session had any conversation, the harness runs one more prompt against the provider with the system prompt "summarize the following coding-agent session in one paragraph followed by 3-5 single-word tags" and the full conversation as the user message. The result becomes the session file's body. Costs ~1 KB of tokens. Catches up with what the agent learned even if it never called `remember` once.

## Why this is extensible

The 3-method interface absorbs every backend we discussed without changes:

| Implementation | Save | Recall | Preamble |
|---|---|---|---|
| `NoMemory` (default fallback) | no-op | empty | "" |
| `MarkdownStore` | append to `.harness/memory.md` | grep + parse | the whole file |
| `SessionFiles` | sessions dir + index.json | filter index, return metadata | last N sessions |
| `JSONLStore` | append line to `.harness/memory.jsonl` | scan + filter by Tags/Content | proyect to markdown |
| `BoltStore` | put in KV | scan + filter by value | concatenate `kind=preamble` entries |
| `BleveStore` | index entry as document | BM25 ranking | top-N by Time |
| `VectorStore` | embed + insert | cosine vs query embedding | top-K by timestamp |
| `MockStore` (tests) | append to in-memory slice | filter in memory | concatenate all |

Adding any of those is one new file in `internal/memory/`, one line changed in `main.go`. The agent loop never sees it.

A `VectorStore` would: in `Save`, embed the entry via the provider and store `(embedding, content)`; in `Recall`, embed the query and return top-K by cosine; in `Preamble`, return the K most recent by timestamp. Same three methods. The chapter on `Provider` (chapter 03) made the abstraction; this chapter extends the same pattern to a different concern.

## Interaction with the rest of the harness

| Layer | Relationship |
|---|---|
| **AGENTS.md** (cap. 15) | Composes: AGENTS.md = human-authored project context, memory = agent-authored learnings. Both prepended to system prompt. AGENTS.md is in the repo root and version-controlled; `.harness/` is `.gitignore`d. |
| **Compaction** (cap. 07) | Orthogonal. Compaction manages *working* memory (the live `messages` slice). The memory store manages what survives across sessions. If compaction nukes most of the conversation, the session summary written at close is still based on the *full* original transcript — Compaction runs in the agent loop, the summary runs at shutdown over the un-compacted history. |
| **Prompt caching** (cap. 17) | `Preamble` is a stable prefix → ideal cache target. First turn of each session pays cache-write for the preamble; subsequent turns read it. Recall results land *after* the cache breakpoint, so they don't invalidate. |
| **Subagents** (cap. 11) | By default the research subagent's tool subset doesn't include `recall` — subagents shouldn't muddy the parent's memory, and giving them access tends to produce off-topic searches. Easy to opt-in if you want it: add `"recall"` to the subset in `registerSubagents`. |
| **Diff approval** (cap. 18) | `remember` doesn't write user files, only `.harness/`. No diff modal. If you ever build a `recall` variant that mutates files (e.g., updates a per-project README with learnings), wire it through diff approval. |

## Pitfalls

**Index drift.** If a session file is renamed/deleted by hand, `index.json` won't reflect it. Recall returns paths that don't exist. The loader verifies every indexed path on startup and prunes missing ones — but if you delete *while the harness is running*, the next Recall produces stale paths. Cheap mitigation already done.

**The summary can be wrong.** The end-of-session model summary might miss what mattered. If a session was about debugging a tricky bug, the summary might emphasize the fix over the discovery. The mitigation: session files are plain markdown. Edit them by hand. Re-run `recall` and the corrected content surfaces.

**The model can't recall what it doesn't know exists.** If your system prompt doesn't mention that a memory store exists, the model won't call `recall` proactively. The `/tools` list shows it, but the agent has to choose to use it. Either: (a) auto-load enough into the Preamble that explicit Recall is rarely needed, or (b) tell the model in the system prompt that memory is available. We do both — the system prompt mentions the recall tool in its inventory, and the Preamble auto-loads the last 5 sessions.

**Privacy.** Memory accumulates everything the agent thought worth saving — API hints, paths, user names, decisions. If you sync `.harness/` between machines, share screenshots, or push the repo without the right `.gitignore`, you're leaking. The default `.gitignore` excludes `.harness/`; don't undo that without thinking.

**The auto-summary runs synchronously at shutdown.** A 2-second extra wait on Ctrl-D as the harness asks for a summary. Surprising the first time. Could be backgrounded with a "summarizing… (Ctrl-C to skip)" prompt; we keep it synchronous because losing the summary loses the entire session's memory.

> **In the current repo.** The interface lives in [`internal/memory/store.go`](../../internal/memory/store.go); `SessionFiles` is in [`internal/memory/sessionfiles.go`](../../internal/memory/sessionfiles.go); tools are [`internal/tool/remember.go`](../../internal/tool/remember.go) and [`internal/tool/recall.go`](../../internal/tool/recall.go); the auto-summary hook lives in `main.go` near the program shutdown.

## Now try

1. Have a normal session and ask the agent to remember something specific: "remember that our test fixtures live in `testdata/golden/`". After Ctrl-D, look at `.harness/sessions/<today>.md`. The summary should mention the fact.
2. Start a new session and ask "where do our test fixtures live?". The agent should answer from the preamble without calling any tool — the fact was loaded with the system prompt.
3. After a handful of sessions, ask "what did we work on last week about compaction?". The agent should call `recall("compaction")`, get a list of matching sessions, decide which to `read_file`, and synthesize an answer.
4. Open `.harness/sessions/<some-old-session>.md` and edit the summary by hand. Start a new session and ask about that topic. The edited content shows up.
5. Implement `MarkdownStore` as a one-file alternative and swap it in with one line in `main.go`. The agent's behavior changes — preamble becomes the whole file, recall is grep, all the same tools work.

← [back to TOC](README.md)
