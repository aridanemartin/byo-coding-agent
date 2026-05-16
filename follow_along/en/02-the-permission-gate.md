# 02 · The permission gate

The agent we have so far will happily run any shell command the model produces. That's fine for `ls`. It's not fine for `rm -rf`. Before going further, we need a way to gate destructive operations.

## The decision: ask every time

There are roughly three places you can put approval logic:

| Where | What it looks like | Tradeoff |
|---|---|---|
| **Inside the tool** | `bash` itself asks "are you sure?" | Each tool has to know there's a person to prompt; couples concerns. Doesn't compose. |
| **At the harness layer** | The agent loop asks before calling the tool | One place, consistent UX, doesn't need tool cooperation. ✓ |
| **At the model layer** | Tell the model "always ask first" | Unreliable. The model is supposed to help, not gatekeep. |

We put it at the harness layer. Specifically, `executeTool` calls `confirm("approve?")` between the `[tool] …` print and the actual dispatch.

```go
func executeTool(name, rawInput string) (string, bool) {
    fmt.Printf("[tool] %s %s\n", name, rawInput)
    if !confirm("approve?") {
        return "user denied this tool call", true
    }
    // … dispatch
}
```

## The `confirm` function

Reads a line from stdin; anything other than `y` / `yes` is no.

```go
func confirm(prompt string) bool {
    fmt.Printf("%s [y/n] ", prompt)
    if !scanner.Scan() { return false }
    a := strings.ToLower(strings.TrimSpace(scanner.Text()))
    return a == "y" || a == "yes"
}
```

Two things hidden in those five lines:

1. **Default is no.** Empty input → false. Ctrl-D → false. Any unrecognized character → false. The conservative default is the safe one when you're about to run a shell command.
2. **The same `scanner` as the main REPL.** Having two scanners on stdin causes buffer races. There's one global scanner, shared by `main` and `confirm`.

## Why "user denied" is a tool result, not a hard stop

When you say no, we don't crash, don't abort the conversation, don't bypass the model. We return:

```go
return "user denied this tool call", true
```

The `true` is `is_error`. The model gets back a tool result saying the call was denied. Typical model behavior on denial:

- Try a different approach (different tool, different arguments)
- Ask you what you'd prefer
- Apologize and stop

This is the same channel as any other tool failure (file not found, bash exit error, etc.). The model doesn't need to know whether it was a deliberate denial or a system error — the contract is just "tool calls can fail; here's the message."

This is one of the most important design decisions in the harness. **The model is in a loop; failures are inputs to the next iteration, not exceptions.** Treating denials, errors, and successes uniformly is what lets the model adapt.

## What gets gated

In this implementation: **every tool call**. Every time the model wants to invoke `bash`, `read_file`, or `write_file`, you get prompted.

This is over-cautious for `read_file` and `write_file` — they're scoped to specific paths, easier to reason about than a shell command. A more nuanced design would use a `PermissionPolicy` interface with named policies:

| Policy | Behavior |
|---|---|
| `AlwaysAllow` | Auto-execute |
| `AlwaysAsk` | Prompt every time (what we have) |
| `AllowList{names}` | Auto-execute the named tools, ask for everything else |
| `AskOnce` | Ask the first time, remember for the session |

We didn't build that. We left it as an exercise. The interface fits cleanly between the agent loop and the registry:

```
agent loop → permission policy → registry.Execute
```

A `policy.Decide(name, input) → allow | deny | ask` would replace the inline `confirm` call.

## Pitfalls

**Scanner races.** Don't create a second `bufio.Scanner` for the confirm prompt. The two would steal bytes from each other unpredictably. Share one.

**Forgetting to mark the error.** Returning `"user denied"` with `isError: false` makes the model think the tool succeeded with an unhelpful output. It'll act on that confusion. Always set `isError: true` for denials.

**The hidden assumption: you're at the keyboard.** In a non-interactive context (CI, scripted tests) the prompt would hang waiting for input. We don't handle that here. A real production version would auto-deny when stdin isn't a TTY.

> **In the current repo.** `executeTool` in [`main.go`](../main.go) is the wrapper:
>
> ```go
> func executeTool(name, rawInput string) (string, bool) {
>     fmt.Printf("[tool] %s %s\n", name, rawInput)
>     if !ui.Confirm("approve?") {
>         return "user denied this tool call", true
>     }
>     return registry.Execute(ctx, name, rawInput)
> }
> ```
>
> The `confirm` function evolved from a simple `bufio.Scanner` read into a Bubble Tea state transition — chapter 12 covers how. The `is_error: true` contract didn't change.

## Now try

1. Ask the agent to do something destructive (`delete all the .log files in the current directory`). Approve. Note what it actually ran.
2. Same prompt — but deny. Watch the model handle the denial. Did it ask you what to do, or just stop?
3. Open `executeTool` and *remove* the `confirm` call temporarily. Try the destructive prompt again. Feel the difference.

## End of arc 1 — the bare minimum

You have a working agent. Two chapters in, the harness can hold a conversation with Claude, run three tools, and ask before doing anything destructive. The whole thing is ~150 lines of one-file Go and feels a lot like a toy. That's fine — it *is* a toy. What it isn't is *extensible*.

The next arc (chapters 03–08) is about earning the right to call this a *harness*: making the LLM swappable, the conversation manageable, and the input usable. The shape repeats every time — small interface, a default implementation, room for others to plug in.

Next: [03 · The provider interface](03-the-provider-interface.md).
