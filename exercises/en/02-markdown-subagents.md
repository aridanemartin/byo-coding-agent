# Exercise 2 — Markdown-defined subagents

**Goal:** define subagents as markdown files loaded from disk, instead of hand-rolled Go structs.

**Difficulty:** medium. **Time:** 1–2 hours. **Touches:** `internal/subagent/`, `main.go`.

## What's already in place

`internal/subagent/registry.go` defines the `Subagent` interface (`Name`, `Description`, `Run`) and a `Default` registry. The one concrete subagent today is `Research` in `internal/subagent/research.go` — its system prompt, tool subset, and iteration budget are all baked into Go.

That's fine for one subagent. The moment you want three reviewers, two planners, and a code-search specialist, you don't want to write a new Go type for each.

## What to build

A loader that scans `.harness/agents/*.md` at startup, parses each file, and registers the result with `subagent.Default`. A file looks like:

```markdown
---
name: reviewer
description: Review a diff for bugs, unhandled errors, and security issues. Returns a bulleted list.
tools: [read_file, bash]
max_turns: 6
---

You are a code reviewer. Look at the diff or files the caller points you at.
Report:
- Bugs and likely runtime failures
- Unhandled errors / ignored returns
- Security concerns (secrets, injection, path traversal)

Be specific. Quote line numbers. No preamble.
```

The frontmatter configures the wrapper; the body is the system prompt.

## Suggested steps

1. **Add a `MarkdownSubagent` type** in `internal/subagent/markdown.go` that wraps an `agent.Agent` and pulls config from a parsed file. It implements `Subagent` the same way `Research` does — most of the logic in `Run` is identical.

2. **Write the loader.** A function `LoadDir(dir string, p provider.Provider, allTools *tool.Registry) ([]Subagent, error)` that:
   - Reads every `.md` file in `dir`.
   - Splits frontmatter (between `---` lines) from body.
   - Parses YAML or a tiny key-value format — your call. Pure-stdlib parsing of `key: value` is fine; you don't have to pull in `yaml.v3`.
   - Builds a filtered `tool.Registry` containing only the tools listed in `tools:`.
   - Returns one `MarkdownSubagent` per file.

3. **Wire it into `main.go`.** After registering the built-in subagents, call `LoadDir` and register each returned subagent. Skip if the directory doesn't exist — boot must not fail.

4. **Add a `/subagents reload` command.** Re-scan the directory and replace the markdown-loaded agents (leave built-ins alone). Read `commands.go` for the command-registration pattern.

5. **Validate.** Reject files missing `name` or `description` with a clear error pointing to the file path. A subagent without a name will produce an empty `delegate_` tool.

## Acceptance

- Drop `.harness/agents/reviewer.md` in the working directory, run `go run .`, and `/subagents` lists `reviewer` alongside `research`.
- The model can dispatch it via `delegate_reviewer` (or whatever the tool naming convention is).
- Deleting the file and running `/subagents reload` removes it from the registry.
- A malformed file (missing `name`) prints a clear startup error but doesn't crash.

## Stretch

- Hot-reload via [`fsnotify`](https://github.com/fsnotify/fsnotify): edits show up without a restart.
- Per-agent model selection in frontmatter (`model: gpt-5-codex`) — wraps a separate Provider behind the agent.
- An `extends:` field that lets one agent inherit another's system prompt or toolset.
