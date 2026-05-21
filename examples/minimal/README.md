# minimal — the bare-bones coding agent

A single-file (`index.ts`, ~130 lines counting comments) TypeScript version of the
coding agent that follow_along [chapter 01](../../follow_along/en/01-the-agent-loop.md)
walks you through. The inner agent loop, the outer REPL, three tools
dispatched by a switch — that's it.

## Run

```sh
export ANTHROPIC_API_KEY=sk-ant-...
cd examples/minimal
npm install
npx tsx index.ts
```

Type at the `>` prompt. Some things to try:

- `list the files here` — exercises the `bash` tool
- `read index.ts and tell me what it does` — exercises `read_file`
- `write a hello.txt with a haiku in it` — exercises `write_file`

`ctrl-D` quits.

## What's deliberately missing

Everything else the harness has. This example is the "essence" pass; each
piece below got added later, with a chapter that explains why:

| Missing | Where it gets added | Why it's worth adding |
|---|---|---|
| Permission gate (`approve?`) | [Chapter 02](../../follow_along/en/02-the-permission-gate.md) | Stops the agent from `rm -rf`-ing your laptop |
| Provider abstraction | [Chapter 03](../../follow_along/en/03-the-provider-interface.md) | Swap Anthropic for OpenAI/Ollama/local |
| Slash commands | [Chapter 05](../../follow_along/en/05-slash-commands.md) | `/help`, `/model`, `/clear`, etc. |
| Compaction | [Chapter 07](../../follow_along/en/07-compaction-strategies.md) | Long conversations don't blow the context window |
| Plug-and-play tools | [Chapter 09](../../follow_along/en/09-plug-and-play-tools.md) | Add a tool by dropping a file, no switch edits |
| Subagents | [Chapter 11](../../follow_along/en/11-subagents.md) | Delegate read-only research without polluting context |
| Full TUI | [Chapter 12](../../follow_along/en/12-full-tui.md) | Real scrollback, status line, modal panels |
| MCP support | [Chapter 14](../../follow_along/en/14-mcp-support.md) | Remote tool servers (git, filesystem, anything) |

If you want to feel how each one earns its keep, run this minimal agent
for a few minutes, then run the full one (`go run .` from the repo root)
back-to-back.
