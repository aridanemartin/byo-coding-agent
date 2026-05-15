# Exercise 3 — Pluggable memory backend

**Goal:** implement a second `memory.Store` and prove the existing harness doesn't know the difference.

**Difficulty:** medium. **Time:** 1–2 hours. **Touches:** `internal/memory/`, `main.go`.

## What's already in place

`internal/memory/store.go` defines the three-method `Store` interface — `Save`, `Recall`, `Preamble`. The default implementation, `SessionFiles` in `internal/memory/sessionfiles.go`, writes one markdown file per session under `.harness/sessions/` and keeps a JSON index for fast lookups.

The interface is small on purpose. Substituting it is the test: do you understand the contract well enough to swap the storage without breaking the agent loop, the `remember`/`recall` tools, or the system-prompt preamble?

## What to build

Pick *one* of these backends and implement it as a sibling of `SessionFiles`:

- **`InMemoryRing{Capacity int}`** — a ring buffer that keeps the N most recent entries. No disk; resets on restart. Forces you to think about what Recall means without persistence.
- **`SQLite{Path string}`** — one row per Entry, with FTS5 for Recall. Forces a real schema decision and a query language choice.
- **`JSONBlob{Path string}`** — a single JSON file rewritten on each Save. The simplest persistent option; forces you to confront concurrent-write semantics.

## Suggested steps

1. **Create the file.** `internal/memory/your_backend.go`. Same package; same import surface as `SessionFiles`.

2. **Implement the three methods.** Save returns `nil` once the entry is durable (for non-persistent stores, "durable" = in the slice). Recall does a best-effort search — substring match is acceptable; the interface doesn't promise semantic search. Preamble returns the always-loaded preface; usually built from `KindSessionSummary` entries.

3. **Write tests.** A round-trip test for persistent backends (Save then Recall returns the entry). A capacity test for the ring buffer (the N+1-th Save evicts the oldest). Mirror `internal/memory/sessionfiles_test.go`.

4. **Wire it in `main.go`.** Replace the line that constructs `SessionFiles` with your new backend, behind an env var or a flag if you want to support runtime selection.

5. **Verify the agent doesn't change.** `remember "x"`, `recall x`, restart, and check the preamble. None of the existing code in `internal/agent` or `internal/tool` should need touching.

## Acceptance

- The `remember` and `recall` tools work transparently against the new backend.
- Existing tests in `internal/agent/` and `internal/tool/` pass without modification.
- New tests in `internal/memory/your_backend_test.go` cover the backend's specific behaviour (eviction, persistence, concurrency).

## Stretch

- A `FanoutStore` adapter that writes to two stores at once (e.g., session files + sqlite for query). Demonstrates that the interface composes.
- A migration command (`/memory migrate sqlite`) that copies entries from one backend to another.
- A bench: how slow is Recall when you have 10k entries? Use Go's `testing.B` to find out before you optimise.
