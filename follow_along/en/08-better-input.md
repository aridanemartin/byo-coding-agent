# 08 · Better input

`bufio.Scanner` reads a line. It does not let you move the cursor mid-line, recall history, or correct a typo three characters back. For a chat REPL where messages can be long, that's brutal.

We're going to fix it in two steps, because the journey is more useful than the destination.

## Step 1: readline

The classic Unix answer to "line editing in a terminal" is `readline`. In Go, `github.com/chzyer/readline` is a drop-in replacement for `bufio.Scanner`:

```go
rl, err := readline.NewEx(&readline.Config{
    Prompt:            "\033[1;36m❯\033[0m ",
    HistoryFile:       "~/.bettatech_harness_history",
    HistorySearchFold: true,
})
defer rl.Close()

for {
    line, err := rl.Readline()
    if errors.Is(err, io.EOF) { return }
    if errors.Is(err, readline.ErrInterrupt) { continue }   // ctrl-c clears the line
    // … handle line …
}
```

That gets you, for free:

- Arrow keys for cursor movement
- Backspace/delete mid-line
- Ctrl-A / Ctrl-E to jump to start/end
- Up/down arrows for history
- Ctrl-R for reverse search through history
- History persisted to `~/.bettatech_harness_history` across sessions
- Ctrl-D to EOF (exit), Ctrl-C to cancel current line

This is the right answer for most CLIs. We use it briefly and then replace it. Why?

## Step 2: Bubble Tea

We wanted the input to look like Claude Code or OpenCode — bordered, styled, multi-line ready, with a status indication. `readline` styling stops at "you can put ANSI codes in the prompt." It can't draw a box.

`github.com/charmbracelet/bubbletea` is the Go TUI framework that those tools use. It's overkill for "read a line." But it's the right starting point for *eventually* having a full TUI (chapter 12), and it lets us draw whatever input affordances we want.

The model-view-update pattern:

```go
type chatInputModel struct {
    ti        textinput.Model
    history   []string
    histIdx   int
    submitted string
    done      bool
}

func (m chatInputModel) Init() tea.Cmd { return textinput.Blink }

func (m chatInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.Type {
        case tea.KeyEnter:
            m.submitted = m.ti.Value()
            m.done = true
            return m, tea.Quit
        case tea.KeyCtrlD:
            return m, tea.Quit
        case tea.KeyUp, tea.KeyDown:
            // history navigation (a few lines per direction)
        }
    }
    var cmd tea.Cmd
    m.ti, cmd = m.ti.Update(msg)
    return m, cmd
}

func (m chatInputModel) View() string {
    return boxStyle.Render(m.ti.View()) + "\n" + hintStyle.Render("enter: send · ↑↓: history · ctrl-d: exit")
}
```

`ReadChatInput()` wraps that into a one-shot: spin up a `tea.NewProgram(...)`, run it, return when the model says done.

The result is a single bordered input box that gets called once per turn:

```
╭───────────────────────────────────────────────────────╮
│ ❯ your message                                        │
╰───────────────────────────────────────────────────────╯
  enter: send · ↑↓: history · ctrl-d: exit
```

After you submit, Bubble Tea exits, the box stays on screen (because we don't use alt-screen mode at this stage), and the next iteration of the REPL kicks off another `ReadChatInput`.

## Why two steps for the same goal

You could go straight from `bufio.Scanner` to Bubble Tea. We didn't, because each step teaches something different.

- **`bufio.Scanner` → readline** teaches you that ergonomics matter. The feel improves dramatically and the code change is small. This is the 80/20.
- **readline → Bubble Tea** teaches you the *next paradigm.* MVU isn't just for prettier input; it's the architecture you'll use when the whole UI becomes a TUI (chapter 12). Doing it once for input is a warmup.

If you're following along and only have time for one step, do readline. Bubble Tea earns its complexity in chapter 12.

## A subtle issue: two readers on stdin

Once you have a fancy input library, you can't *also* use `bufio.Scanner` somewhere else (for `confirm()`, say). Two readers on the same stdin buffer steal bytes from each other in unpredictable ways.

The fix in this chapter: use one input mechanism. Confirm uses the same `readline.Instance` via `SetPrompt`:

```go
func confirm(prompt string) bool {
    input.SetPrompt(prompt + " [y/n] ")
    defer input.SetPrompt(mainPrompt)
    line, err := input.Readline()
    // …
}
```

In Bubble Tea mode, same idea — confirm runs another `tea.NewProgram`. By chapter 12 the entire UI is one Bubble Tea program and confirm becomes a state transition inside that program; the "two readers" problem dissolves because there's only ever one.

## Pitfalls

**Persisting history.** When you add `HistoryFile`, you're writing your typed text to disk. If you ever paste an API key into chat (it happens), you've leaked it to `~/.bettatech_harness_history`. We accept this for a learning project. For production: don't persist history, or hash/redact specific patterns first.

**TUI in non-TTY environments.** `tea.NewProgram` fails if stdin isn't a terminal. We don't handle this gracefully — running `harness < script.txt` would crash. A real-world version would detect non-TTY and fall back to scanner-mode automatically.

**Up/down ambiguity with multi-line input.** Once you have a textarea (vs textinput), arrow keys mean "move within text," not "navigate history." The standard fix is Ctrl-P / Ctrl-N for history (Emacs convention) and reserve arrows for cursor movement. We use single-line `textinput` to sidestep this.

> **In the current repo.** The one-shot Bubble Tea version of input (with history navigation and persistent history) is in [`internal/ui/input.go`](../internal/ui/input.go) — see `chatInputModel`. By chapter 12 the input is part of a larger persistent TUI program, but `input.go` still has the standalone version and the `loadHistory` / `appendHistory` helpers. History persistence isn't wired into the chapter-12 TUI yet — that's one of the exercises in chapter 13.

## Now try

1. Compare the feel of the harness with the `bufio.Scanner` version (use git to check out an earlier state if you have history) vs the readline version vs the Bubble Tea version. Notice how much "polish" is just affordance count.
2. Read `internal/ui/input.go` and trace the up-arrow code path. The `bufferText` field stores what you were typing *before* you started navigating history, so pressing Down past the most-recent entry restores it. Surprisingly easy to miss.
3. Replace `textinput` with `textarea` (also from `bubbles`) and figure out the multi-line keybindings. Specifically: how do you bind Shift-Enter to "insert newline" while plain Enter "submits"? (This is a real-world rabbit hole — some terminals can't distinguish the two.)

## End of arc 2 — abstractions earn their keep

Six chapters of abstractions: a `Provider` interface, a slash-command palette, an explicit conversation contract, three compaction strategies, a real input box. Each chapter solved one problem and added one seam. The harness now reads like a *system* instead of a script — and you can already swap the LLM, the compaction strategy, and the input layer without touching the rest.

Arc 3 (chapters 09–12) is where those seams pay off. We turn the tool switch into a registry, move code into `internal/` packages, introduce subagents (each one is just another agent running through the same loop), and replace stdout-printing with a full Bubble Tea program. By the end the harness looks like a small Claude Code.

Next: [09 · Plug-and-play tools](09-plug-and-play-tools.md).
