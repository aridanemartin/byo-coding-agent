# 04 · UI polish

By chapter 03 the harness is a working REPL: it talks to Claude, runs three tools, asks before destructive operations, and can swap LLM provider in one line. What it doesn't have is *texture*. The prompt is a bare `>`. There's no feedback while the model is thinking — just dead silence and a blinking cursor for two to five seconds. Resize your terminal narrower than 80 columns and the output mangles.

A short chapter, then. We're going to add a banner, make it survive narrow terminals, and add a loading spinner. None of this is load-bearing; the goal is to learn three small techniques that come up everywhere in CLI work.

## The banner

ASCII art using the "ANSI Shadow" figlet font, which most terminal-AI tools (Claude Code, OpenCode) use as their wordmark.

```
██████╗ ███████╗████████╗████████╗ █████╗ ████████╗███████╗ ██████╗██╗  ██╗
██╔══██╗██╔════╝╚══██╔══╝╚══██╔══╝██╔══██╗╚══██╔══╝██╔════╝██╔════╝██║  ██║
██████╔╝█████╗     ██║      ██║   ███████║   ██║   █████╗  ██║     ███████║
██╔══██╗██╔══╝     ██║      ██║   ██╔══██║   ██║   ██╔══╝  ██║     ██╔══██║
██████╔╝███████╗   ██║      ██║   ██║  ██║   ██║   ███████╗╚██████╗██║  ██║
╚═════╝ ╚══════╝   ╚═╝      ╚═╝   ╚═╝  ╚═╝   ╚═╝   ╚══════╝ ╚═════╝╚═╝  ╚═╝
```

Plus a subtitle in dim gray. Wrap the whole thing in `\033[1;36m` (bold cyan) and `\033[0m` (reset). Done.

## Narrow terminals

The banner is 75 columns wide. At anything less than ~78 columns it wraps and looks like garbage. So we detect terminal width and fall back to a plain-text wordmark.

```go
import "golang.org/x/term"

func TermWidth() int {
    w, _, err := term.GetSize(int(os.Stdout.Fd()))
    if err != nil { return 0 }
    return w
}

func PrintBanner() {
    if TermWidth() >= 78 {
        // big banner
    } else {
        // single-line wordmark: "  BETTATECH  ·  build your own coding agent"
    }
}
```

Three small things hidden in that pattern, worth knowing because they show up everywhere:

1. **`golang.org/x/term`** is the canonical way to ask "is stdout a TTY, and how wide?" The standard library doesn't expose it.
2. **`GetSize` errors on non-TTYs** (piped, redirected). We treat err as "0 cols", which falls into the small-banner branch — the right thing for `harness > log.txt`.
3. **78 is breathing room** for a 75-wide banner. Picking the exact width is a tripping hazard if you ever add a single character.

## The spinner

While the agent is waiting on the API, you see nothing for several seconds. That's bad UX. We add a small braille spinner that overwrites itself in place:

```go
var spinnerFrames = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

type Spinner struct {
    stop chan struct{}
    done chan struct{}
}

func StartSpinner(label string) *Spinner {
    s := &Spinner{stop: make(chan struct{}), done: make(chan struct{})}
    go func() {
        defer close(s.done)
        ticker := time.NewTicker(80 * time.Millisecond)
        defer ticker.Stop()
        i := 0
        for {
            select {
            case <-s.stop:
                fmt.Print("\r\033[K") // clear the line
                return
            case <-ticker.C:
                fmt.Printf("\r%c %s", spinnerFrames[i], label)
                i = (i + 1) % len(spinnerFrames)
            }
        }
    }()
    return s
}

func (s *Spinner) Stop() {
    close(s.stop)
    <-s.done   // block until the goroutine confirms it cleared the line
}
```

Three details, all of which exist for reasons:

1. **`\r` returns the cursor to column 0; `\033[K` clears to end of line.** Together they overwrite the spinner frame cleanly. Without the clear, going from a long label to a short one leaves trailing garbage.
2. **`Stop()` blocks on `done`.** This is the part that surprises people. If `Stop()` returned immediately, the spinner's goroutine might print another frame *after* we'd already moved on to printing the model's response. The synchronization guarantees that by the time `Stop()` returns, no more spinner output is in flight.
3. **Non-TTY check** (not shown). If stdout isn't a terminal, the spinner just returns a no-op shell. Spamming `\r⠋...` into a log file is worse than no spinner.

In chapter 12 we replace this entirely with `bubbles/spinner` inside a Bubble Tea program. The braille-on-stdout version above is fine for the REPL era.

## Where this fits

```go
// agentLoop
sp := startSpinner("thinking...")
resp, err := provider.Send(ctx, messages, tools)
sp.Stop()
```

The spinner runs only during the API call. As soon as the response is back, it's stopped, and we print whatever came down — text or `[tool]` log lines — on a clean line.

## Pitfalls

**Animation killing the prompt cache.** Not for spinner specifically, but for ANSI sequences: anything that ends up in the system prompt (timestamps, animated decorations) destroys prompt caching. Banners are fine because they're one-shot at startup, not part of the messages array.

**Unicode width.** Some terminals don't render `█` and braille at 1-cell width. On macOS Terminal.app, fine. On a few minimalist terminals (early `kitty` setups, some `tmux` configurations), the banner can wrap. There's no perfect fix; we accept it.

> **In the current repo.** Banner code (with both the wide ANSI Shadow variant and the narrow-terminal fallback) is in [`internal/ui/banner.go`](../internal/ui/banner.go). The stand-alone spinner — used in REPL mode before the TUI took over — is [`internal/ui/spinner.go`](../internal/ui/spinner.go). The TUI version (chapter 12) uses `bubbles/spinner` instead; both files survive in the repo so you can compare the two approaches.

## Now try

1. Resize your terminal to 60 columns wide. Restart the harness. Confirm the fallback banner kicks in.
2. Pipe the output: `go run . > /tmp/out.txt`. Open the file. The banner should be the *small* one (because `GetSize` returned err on the non-TTY pipe).
3. Change the spinner frames to `|/-\`. Compare the feel. Same operating principle, very different vibe.

Next: [05 · Slash commands](05-slash-commands.md).
