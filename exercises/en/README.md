# Exercises

Six extension exercises for the harness, in rough order of difficulty. Each one parallels an existing extension point (Provider, Tool, CompactionStrategy, Store, Subagent) and forces a real architectural decision — not just a paint-by-numbers swap.

Read the relevant `follow_along/` chapter first; these don't re-explain the why.

| # | Exercise | Layer | Difficulty |
|---|---|---|---|
| 1 | [Tool retry on error](01-tool-retry-on-error.md) | Agent loop | easy |
| 2 | [Markdown-defined subagents](02-markdown-subagents.md) | Subagents | medium |
| 3 | [Pluggable memory backend](03-pluggable-memory.md) | Memory | medium |
| 4 | [Pluggable transcript renderer](04-transcript-renderer.md) | Transcript | medium |
| 5 | [Streaming responses](05-streaming-responses.md) | Provider + UI | hard |
| 6 | [Image inputs](06-image-inputs.md) | API types + Provider | medium |

Each file has a goal, a recap of what's already in the repo, suggested steps with concrete file paths, and acceptance criteria. The "Stretch" section at the end is optional — pick it up if the base exercise turned out small for you.

Done one? Open a PR. There's no canonical solution; the value is in the trade-offs you make along the way.
