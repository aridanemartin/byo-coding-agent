# 12 · The full TUI

In chapter 08 we used Bubble Tea for the input box only — a one-shot program that ran, took a line, and exited. The REPL kept looping and printing to stdout.

Now we go all the way: **one Bubble Tea program owns the whole UI.** A viewport for scrollback, a bordered input box, an approval prompt that takes over when needed, a spinner above the input while the agent is working, and a status indicator showing which subagents are running.

This is the chapter where the harness starts to look like Claude Code or OpenCode.

## The architectural shift

Before: REPL loop → input → agent runs synchronously, prints to stdout → next input.

After: Bubble Tea program → input box → you submit → agent runs **in a goroutine**, posts events to the program → program updates the viewport → done event → next input.

The whole thing is a state machine inside the model:

```go
type modelState int
const (
    stateIdle modelState = iota
    stateRunning
    stateAwaitingApproval
)
```

Transitions:

| From | Event | To |
|---|---|---|
| `stateIdle` | you submit a line | `stateRunning` |
| `stateRunning` | agent calls `Confirm` | `stateAwaitingApproval` |
| `stateAwaitingApproval` | you pick y/n | `stateRunning` |
| `stateRunning` | agent returns | `stateIdle` |

## The trick that made it tractable

The agent loop, the slash commands, and the tools all use plain `fmt.Println` to print. Rewriting all 40-ish print sites to push tea.Msg events would be tedious.

Instead, we **redirect `os.Stdout` to a pipe** and forward each line into the program as an `AppendMsg`:

```go
// main.go
originalStdout := os.Stdout
r, w, _ := os.Pipe()
os.Stdout = w

program := ui.NewProgram(runner)

go func() {
    scanner := bufio.NewScanner(r)
    for scanner.Scan() {
        program.Send(ui.AppendMsg(scanner.Text() + "\n"))
    }
}()
```

Every existing `fmt.Println` writes to the pipe. The forwarder reads each line and posts it into the program. The program's `Update` handler appends to its scrollback buffer:

```go
case AppendMsg:
    m.output.WriteString(string(msg))
    m.viewport.SetContent(m.output.String())
    if m.followBottom { m.viewport.GotoBottom() }
```

Zero refactor of print sites. Tools, commands, agent logs — all flow into the viewport as-is.

There's a subtle catch: Bubble Tea also writes to stdout for its own rendering. If we redirect *that*, we get an infinite loop. The fix is `tea.WithOutput(originalStdout)` — tell Bubble Tea to write to the *original* stdout, not the redirected one.

Actually, in this codebase we use `tea.WithAltScreen()` instead — Bubble Tea takes over the terminal in "alternate screen" mode and writes there directly. Same effect (Bubble Tea's output bypasses the pipe), different mechanism.

## The model and its parts

```go
type harness struct {
    runner AgentRunner    // closure that dispatches commands or runs the agent

    viewport viewport.Model  // scrollback
    input    textinput.Model // bordered input
    spinner  spinner.Model   // animated braille while running

    state          modelState
    approvalPrompt string
    approvalReply  chan bool

    output *strings.Builder  // ← must be a pointer (next pitfall)
    followBottom bool         // auto-scroll if we're at the bottom
}
```

The View() composes the parts:

```go
func (m harness) View() string {
    return lipgloss.JoinVertical(
        lipgloss.Left,
        m.viewport.View(),
        m.inputArea(),
    )
}
```

`inputArea` is where the state-machine peeks through. It returns either the y/n approval box or the normal input box, with an optional spinner line above when running. Always reserves 5 lines so the layout doesn't jitter on state transitions.

## The approval flow

This is the most interesting state transition. When the agent calls `Confirm("approve?")`, we need to block until you pick. But we can't *actually* block in Bubble Tea's Update — that would freeze the entire UI.

The solution: a channel.

```go
// In main.go, when setting up the root agent:
rootAgent.Confirm = func(prompt string) bool {
    reply := make(chan bool, 1)
    program.Send(ui.ApprovalRequest{Prompt: prompt, Reply: reply})
    return <-reply
}
```

The agent's goroutine sends an `ApprovalRequest` to the program and blocks on the reply channel. The program's Update handles `ApprovalRequest` by flipping state to `stateAwaitingApproval` and stashing the channel. When you press y or n, the program writes to the channel — unblocking the agent's goroutine — and flips state back to `stateRunning`.

```go
case ApprovalRequest:
    m.state = stateAwaitingApproval
    m.approvalPrompt = msg.Prompt
    m.approvalReply = msg.Reply
    return m, nil
```

The agent never touches Bubble Tea directly. It calls a function (`Confirm`) that happens to be implemented in terms of channel + Bubble Tea. Clean separation; the agent is reusable in non-TUI contexts.

## The spinner above input

`bubbles/spinner` provides an animated braille indicator. Three rules govern when it animates:

1. **Tick is issued when state transitions to running.** Inside the Enter-key handler: `return m, tea.Batch(m.runOnce(text), m.spinner.Tick)`.
2. **The TickMsg handler self-perpetuates while running.** Each tick re-renders and issues the next tick. When state isn't running, we return nil — the chain stops.
3. **The view shows the spinner line only when running.** Other times the line is blank but reserved, keeping the layout stable.

The status line also includes any active subagents inline: `⠹ thinking... · research`. No separate status bar; the spinner line is enough.

## Cycle break: `agent` no longer imports `ui`

Adding `ui → subagent` (for `subagent.Active()`) created `agent → ui → subagent → agent`. The fix was to drop `agent`'s direct UI imports — remove the `ui.StartSpinner` call (status bar replaces it), inline a plain-text compaction diff instead of calling `ui.PrintCompaction`.

This is actually a cleaner design: the agent is pure logic with no UI knowledge. It prints lines to stdout via `fmt.Println`, the TUI captures those lines via the pipe trick. The agent doesn't know there *is* a TUI.

## The `strings.Builder` panic

While building this, the program panicked with:

```
panic: strings: illegal use of non-zero Builder copied by value
```

What happened: Bubble Tea passes the model by value through `Update`. `strings.Builder` runs a `copyCheck` on every write that detects when it's been copied — that's the whole point of `Builder`'s safety mechanism. The model's `output strings.Builder` field was being copied on every Update, triggering the panic on the next WriteString.

Fix: use `*strings.Builder`. The pointer survives the value-copy intact.

```go
type harness struct {
    // …
    output *strings.Builder   // ← pointer, not value
}
```

**General rule for Bubble Tea models:** anything in a model that can't be safely copied — `sync.Mutex`, `strings.Builder`, file handles, anything with `noCopy` — has to live behind a pointer. The "return a new model from Update" pattern looks immutable but is really "copy + mutate + return."

## What you get

Concretely:

- **Real scrollback.** PgUp/PgDn/Home/End to scroll the viewport.
- **Auto-follow when at bottom.** Scroll up, viewport stays where you put it. Scroll back down, it tracks new output again.
- **Live subagent indicator.** During a run, the spinner line shows `⠹ thinking... · research` when a research subagent is in flight.
- **Inline approval.** The y/n prompt replaces the input box with a yellow-bordered version. Single keypress; no Enter needed.
- **Terminal restored on exit.** Alt-screen mode means Ctrl-D returns you to your shell prompt with the previous terminal state intact.

## Pitfalls

**Don't try to print from inside Update.** The model's Update runs on Bubble Tea's main loop. If you call `fmt.Println` from there, you're writing to the redirected pipe, which sends an `AppendMsg` back to Update. Recursive, eventually blocks. Use `tea.Cmd` for any side effects.

**The viewport's `AtBottom()` tracking.** We use `m.followBottom = m.viewport.AtBottom()` after forwarding events to the viewport. This is what makes "auto-scroll but stop following when you scrolled up" work. Easy to forget; results in either always-jumping-to-bottom or never-following.

**Sample subagents that never finish.** Because the agent goroutine can block on a confirm channel, you need a way out if you're stuck. We don't have one — Ctrl-D quits the whole program. Production would also handle Ctrl-C as "abort current operation" by sending a cancel signal through context.

> **In the current repo.** The pieces from this chapter:
>
> - The whole TUI: [`internal/ui/program.go`](../internal/ui/program.go). The model is `harness`; the messages are `AppendMsg` / `ApprovalRequest` / `agentDoneMsg`; the state machine is the three constants at the top.
> - The stdout pipe trick and how the agent's Confirm function gets wired to send `ApprovalRequest`: [`main.go`](../main.go). Read the `main()` function top-to-bottom — pipe setup, program construction, goroutine forwarder, `program.Run()`.
> - `ui.SuppressSpinner = true` in `main.go` disables the legacy chapter-04 spinner (which would corrupt the TUI by writing `\r` escapes into the pipe).

## Now try

1. While the agent is running, scroll up with PgUp. Notice the auto-follow stops. Scroll back to bottom (End). Notice it resumes.
2. Force a long-running task and observe the spinner. Then trigger a subagent — watch the spinner line update to include the subagent name.
3. Try replacing `tea.WithAltScreen()` with no option (so Bubble Tea renders inline). The cursor handling gets weirder, but you can see how the alt-screen abstraction is doing work for you.

## End of arc 3 — architecture pays off

The harness is now structurally complete. Provider, tools, compaction, subagents, TUI — every layer has its own seam, every layer composes. That's the test of the abstractions: nothing in the agent loop knows whether a tool is local or remote, whether the model is Anthropic or OpenAI, whether the UI is stdout or a Bubble Tea program.

Chapter 13 wraps up the core book with what we deliberately skipped and where to take it. Chapters 14–19 are *extras* that drop in on top of the architecture you've built — MCP servers as tools, project context via `AGENTS.md`, the token viewer, prompt caching, diff approval for writes, and persistent agent memory. Each is self-contained — read them when you want the feature.

Next: [13 · What's next](13-whats-next.md).
