# 05 · Slash commands

The REPL we have so far does one thing: take a line of input, send it to the model. There's no way to ask "what model am I using?", clear the conversation, switch backends, or even exit cleanly without Ctrl-D.

Time for a command palette. Every line starting with `/` gets intercepted before it reaches the model.

## The dispatcher

The pattern is a tiny registry — a map from name to handler — plus a routing function in the REPL:

```go
type command struct {
    description string
    usage       string
    run         func(args string)
}

var commands = map[string]command{}

func init() {
    commands["help"]    = command{description: "show available commands",        run: cmdHelp}
    commands["model"]   = command{description: "show or change the model",       run: cmdModel}
    commands["clear"]   = command{description: "clear conversation history",     run: cmdClear}
    commands["tools"]   = command{description: "list available tools",           run: cmdTools}
    commands["exit"]    = command{description: "exit the harness",               run: cmdExit}
}

func runCommand(line string) bool {
    if !strings.HasPrefix(line, "/") { return false }
    parts := strings.SplitN(strings.TrimPrefix(line, "/"), " ", 2)
    name := parts[0]
    args := ""
    if len(parts) > 1 { args = strings.TrimSpace(parts[1]) }
    c, ok := commands[name]
    if !ok {
        fmt.Printf("unknown command: /%s (try /help)\n", name)
        return true
    }
    c.run(args)
    return true
}
```

The REPL then becomes:

```go
userInput := strings.TrimSpace(scanner.Text())
if userInput == "" { continue }
if runCommand(userInput) { continue }   // ← new line
// otherwise, send to model
```

Three things to notice:

1. **`runCommand` returns `bool`** — whether the line was handled. The REPL's job is to decide what to do with that line; commands and "send to model" are two cases.
2. **Unknown commands print an error but return `true`.** Otherwise typing `/asdf` would be sent to the model, which is confusing — was it a typo, or did the model see a slash-prefixed message?
3. **State for commands lives at package scope.** `provider`, `messages`, `compactor` (later) are all package-level. Commands mutate them directly. Single-goroutine REPL means no locking.

## Promoting state to globals

Before this chapter, `messages` was a local variable in `main`. To let commands mutate it, we promote it (and `provider`) to package scope:

```go
var (
    provider Provider
    messages []Message
)
```

The agentLoop loses its `messages` parameter and now mutates the global. The REPL loses its return-and-reassign pattern.

This is one of those choices where "good Go style" disagrees with "what's easy to teach." Strict architectures pass state through structs and methods. For a small REPL with one goroutine and no tests, globals are simpler and clearer. We'll revisit when we add subagents (chapter 11) and need multiple `Agent` instances.

## `/model`: parameterized commands

The interesting command is `/model`. With no args, it shows the current model and lists suggestions. With an arg, it sets the model:

```go
var knownModels = []string{
    "claude-opus-4-7",
    "claude-opus-4-6",
    "claude-sonnet-4-6",
    "claude-haiku-4-5",
}

func cmdModel(args string) {
    if args == "" {
        fmt.Printf("current: %s\n", provider.Model())
        fmt.Println("suggestions:")
        for _, m := range knownModels { fmt.Printf("  %s\n", m) }
        return
    }
    provider.SetModel(args)
    fmt.Printf("model: %s\n", args)
}
```

Two design notes:

1. **We don't validate the model id.** `/model claude-foo` succeeds; the *next* API call fails with a 404. That's fine — the error propagates through the same path as any other API error.
2. **`Provider.Model()` / `Provider.SetModel(name)` are why those exist on the interface** (chapter 03). One concession to provider-specific concerns, but every LLM provider has a notion of model, so it generalizes.

## `/clear`: how trivial state ops can be

The `messages` slice is the *entire* conversation history. The model is stateless. So:

```go
func cmdClear(_ string) {
    messages = messages[:0]
    fmt.Println("conversation cleared")
}
```

One line. The model has no memory; clearing our local slice IS clearing the conversation. We'll explore why this works in chapter 06.

## `/help`: discovering what's available

A common BYO mistake is over-engineering help. Just list the commands, alphabetized, with descriptions:

```go
func cmdHelp(_ string) {
    names := make([]string, 0, len(commands))
    for n := range commands { names = append(names, n) }
    sort.Strings(names)
    for _, n := range names {
        c := commands[n]
        display := "/" + n
        if c.usage != "" { display = c.usage }
        fmt.Printf("  %-22s %s\n", display, c.description)
    }
}
```

The `usage` field is for commands that take args. `/model` displays as `/model [name]`; `/clear` displays as just `/clear`. Tiny detail; matters a lot when you have ten commands.

## Pitfalls

**Mutating slice headers vs underlying storage.** `messages = messages[:0]` keeps the underlying array (good for memory reuse) and resets the length. `messages = nil` is also fine. `messages = []Message{}` is fine but allocates. `len(messages) = 0` is not a thing.

**`SplitN` instead of `Split`.** We use `strings.SplitN(s, " ", 2)` so that args containing spaces (`/model claude-opus-4-7`) survive as one string. Plain `Split` would split on every space and break commands like `/some-command arg with spaces`.

**Where do commands live in package terms?** We left them in `main.go` (well, `commands.go` in the same package as `main`) intentionally. Commands touch *every* extension point — provider, messages, tools, compaction. Putting them in their own package would require either passing all state through, or making everything global *and* exported. Better to keep the integration layer at the top.

> **In the current repo.** All commands live in [`commands.go`](../commands.go). The registry pattern (a `map[string]command`, a `runCommand` dispatcher, one `cmdX` function per command) is unchanged from this chapter. By chapter 11 we've added `/compact`, `/verbose`, `/subagents`; each one is one new entry in `init()` and one new `cmdX` function. The shape scales linearly.

## Now try

1. Add a `/quit` command as an alias for `/exit`. The naive approach is duplicating the entry; a cleaner approach is making aliases first-class. Which feels right?
2. Try `/clear` after several turns. Then ask the model "what did we just discuss?" Verify it has no memory.
3. Stub a `/save` command that writes the current `messages` slice to a JSON file. Stub a `/load` that reads it back. This is one path to conversation persistence; chapter 13 mentions others.

Next: [06 · Conversation state](06-conversation-state.md).
