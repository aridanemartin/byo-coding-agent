# 01 · The agent loop

The whole thing fits in one diagram:

```
[user input]
    │
    ▼
[append to messages]
    │
    ▼
[call model] ─────────┐
    │                 │
    ▼                 │
[has tool_use?] ──no──┴──▶ [print text, return to REPL]
    │
   yes
    │
    ▼
[execute each tool]
    │
    ▼
[append tool_results]
    │
    ▼
(loop back to "call model")
```

That's it. The model decides what to do; the harness executes; the loop continues until the model stops asking for tools. Everything else in this book — providers, compaction, subagents, the TUI — is a layer on top of this loop.

## Aside: what's a REPL?

The diagram above is the **inner** loop — one full agent turn. There's also an **outer** loop wrapping the whole thing, called a **REPL**: **R**ead–**E**val–**P**rint–**L**oop.

If you've written or played video games, you've already seen this shape. A **game loop** runs at 60 frames per second and does the same four things every frame:

```
loop forever:
    read inputs    (key presses, mouse, controller)
    update state   (physics, AI, what changed since last frame)
    render         (draw the new frame to the screen)
    sleep until next frame
```

A REPL is the same skeleton, just slowed down and event-driven. Instead of running 60 times per second on a timer, it runs once per *user input* — keyed off the human typing, not the clock:

```
loop forever:
    read input     (the user's line)
    eval           (do something with it)
    print          (show the result)
    wait for next line
```

You've used REPLs in this shape before, probably without naming them: Python's `python3` prompt, a browser's JavaScript console, `bash` itself. Same loop, same purpose, different "eval" step.

Our harness's outer loop is a REPL. The twist: "eval" means "run the agent loop on the user's message," not "run a piece of code." So the harness has two nested loops:

| Loop | Driven by | One iteration is |
|---|---|---|
| Outer (REPL) | The user's keystrokes | Read a line → run the agent on it → print → wait for next line |
| Inner (agent loop) | The model's choices | Send to model → if tool_use, execute and append → repeat until done |

Same skeleton as a game loop. The outer loop is a *game tick on user input*; the inner loop is the *update step* (with model+tools standing in for physics+AI). When you read about "the loop" in later chapters, context tells you which one — mostly it's the agent loop, since that's where the interesting state lives.

## What happens in one turn

The diagram at the top of this chapter shows the full picture, but it's easier to digest in two passes — first without tools, then with.

### Pass 1: without tools, it's just a chat client

Imagine the model has no tools. The inner loop collapses to:

1. User types a message.
2. We append it to a running list of messages.
3. Send the whole list to the model.
4. Model returns a text response.
5. Print it.
6. Wait for the next user input.

That's a working program. It chats. The user could ask "what's the capital of France?" and get "Paris." But it can't *do* anything — it has no hands. To the user, it feels like a wrapper around the API.

### Pass 2: tools turn the chat into an agent

To give the model hands, we add **tools** — named operations the harness knows how to perform, like `bash`, `read_file`, `write_file`. Each tool has a JSON schema describing its inputs. The schemas get sent alongside every model request so the model knows what's available.

Now the model has two kinds of response it can return:

| Response shape | What it means | What we do |
|---|---|---|
| Plain text | "Here's my answer." | Print it, wait for the next user message |
| A **tool call request** | "Before I answer, please run `read_file` with `path: main.go` and tell me what's in it." | Run the tool, send the result back, call the model again |

The second branch is where the **loop** comes in. When the model asks for a tool, the harness:

1. Executes the tool locally (e.g. runs `read_file`, captures the output).
2. Appends a **tool result** to the messages slice.
3. Sends the now-longer conversation back to the model.
4. Model sees the result and decides what to do next — answer, or call another tool.

> **What a tool actually looks like in the repo.** Here's the live `read_file` tool — [`internal/tool/readfile.go`](../internal/tool/readfile.go) in this codebase:
>
> ```go
> type ReadFileTool struct{}
>
> func (ReadFileTool) Definition() api.ToolDef {
>     return api.ToolDef{
>         Name:        "read_file",
>         Description: "Read the contents of a file at the given path.",
>         InputSchema: map[string]any{
>             "path": map[string]any{
>                 "type":        "string",
>                 "description": "Path to the file to read.",
>             },
>         },
>         Required: []string{"path"},
>     }
> }
>
> func (ReadFileTool) Execute(_ context.Context, rawInput string) (string, bool) {
>     var in struct{ Path string `json:"path"` }
>     json.Unmarshal([]byte(rawInput), &in)
>     data, err := os.ReadFile(in.Path)
>     if err != nil { return err.Error(), true }
>     return string(data), false
> }
> ```
>
> A struct implementing two methods. `Definition` returns the schema the model sees. `Execute` does the work and returns `(result string, isError bool)`. Chapter 09 covers why tools end up in this shape; the rest of this chapter shows a simpler precursor where all three tools are dispatched by a switch statement.

That last step is recursive in spirit but iterative in code. A single user message might trigger one model call ("Paris.") or twenty (read three files, run two bash commands, then finally synthesize an answer). The model picks; the harness obeys. We keep looping as long as the model keeps asking for tools, and we stop the moment it returns plain text.

### Putting it together

So the inner loop has exactly two exits:

- The model returns text → print it, return to the REPL, wait for the next user message.
- The model returns a tool call → run the tool, append the result, ask again.

That's the entire conceptual picture. Everything that follows in this chapter is wire-level detail: what the request and response actually look like, which tools we expose, and how to structure the Go code.

## The contract with the model

A single call to the Anthropic Messages API has this shape:

- **Input:** a `system` prompt, an array of `messages`, and an optional array of `tools` (each with a JSON schema for its input).
- **Output:** a response with `content` blocks (text and/or `tool_use`) and a `stop_reason`.

The `stop_reason` is what drives the loop:

| Stop reason | What it means | What we do |
|---|---|---|
| `end_turn` | Model finished | Print text, return to REPL |
| `tool_use` | Model wants to call tools | Run them, append results, call again |

There are other stop reasons (`max_tokens`, `refusal`, etc.) — we handle them by treating anything that isn't `tool_use` as "we're done with this turn."

## Choosing the tool surface

We could have given the model **one bash tool** and called it a day — `bash` can read files, write files, do everything. Or we could have given it dozens of specialized tools.

We chose three:

- `bash` — for everything we don't have a dedicated tool for
- `read_file` — explicit, gives the harness a hook to do staleness checks later if we want
- `write_file` — same, plus easy to surface in the UI as "the model is writing this file"

**The reason we promoted file ops to dedicated tools** isn't that they're necessary — it's that they're **gateable**. A `read_file` tool gives the harness an action-specific seam to log, audit, or restrict. Bash gives us only an opaque command string. Approval (next chapter) is meaningful per-tool; it isn't if you only have bash.

This is the first time the harness/model split matters: the model doesn't care whether you give it one tool or three. The shape of your tool surface is a harness decision.

## The basic loop in Go

The skeleton, roughly:

```go
func main() {
    client := anthropic.NewClient()
    var messages []anthropic.MessageParam

    scanner := bufio.NewScanner(os.Stdin)
    for {
        fmt.Print("> ")
        if !scanner.Scan() { return }
        userInput := scanner.Text()
        if userInput == "" { continue }

        messages = append(messages, anthropic.NewUserMessage(
            anthropic.NewTextBlock(userInput),
        ))
        messages = agentLoop(messages)
    }
}

func agentLoop(messages []anthropic.MessageParam) []anthropic.MessageParam {
    for {
        resp, _ := client.Messages.New(ctx, anthropic.MessageNewParams{
            Model:     anthropic.ModelClaudeOpus4_7,
            MaxTokens: 8192,
            System:    []anthropic.TextBlockParam{{Text: systemPrompt}},
            Messages:  messages,
            Tools:     tools,
        })
        messages = append(messages, resp.ToParam()) // assistant turn

        var toolResults []anthropic.ContentBlockParamUnion
        for _, block := range resp.Content {
            switch v := block.AsAny().(type) {
            case anthropic.TextBlock:
                fmt.Println(v.Text)
            case anthropic.ToolUseBlock:
                result, isErr := executeTool(v.Name, v.JSON.Input.Raw())
                toolResults = append(toolResults,
                    anthropic.NewToolResultBlock(v.ID, result, isErr))
            }
        }

        if resp.StopReason != anthropic.StopReasonToolUse {
            return messages
        }
        messages = append(messages, anthropic.NewUserMessage(toolResults...))
    }
}
```

The whole REPL is the outer loop; the agent loop is the inner loop. They're nested intentionally: the REPL is a conversation, each turn of the conversation is potentially multiple model+tool round-trips.

## The `executeTool` switch

The tool dispatcher is a switch on the tool name. Each case decodes the JSON input, does the work, returns a string + an error flag:

```go
func executeTool(name, rawInput string) (string, bool) {
    fmt.Printf("[tool] %s %s\n", name, rawInput)
    switch name {
    case "bash":
        var in struct{ Command string `json:"command"` }
        json.Unmarshal([]byte(rawInput), &in)
        out, err := exec.Command("sh", "-c", in.Command).CombinedOutput()
        if err != nil {
            return fmt.Sprintf("%s\n[exit error: %v]", out, err), true
        }
        return string(out), false
    case "read_file":
        // similar
    case "write_file":
        // similar
    default:
        return fmt.Sprintf("unknown tool: %s", name), true
    }
}
```

Three things worth pointing out:

1. **The function never returns a Go `error`.** Failures become *strings the model reads*. If `read_file` fails because the path doesn't exist, the tool result is `"no such file or directory"` with `is_error: true`. The model sees that, apologizes or tries a different path, and continues. If we returned a Go error and crashed the loop, the model would have no way to recover.

2. **There's a print at the top.** `fmt.Printf("[tool] %s %s\n", name, rawInput)` — pure observability. Lets you watch the agent's actions as they happen. Not load-bearing.

3. **The default case is defensive.** Models occasionally hallucinate tool names. Returning an error result (instead of panicking) lets the model self-correct.

## Pitfalls we hit

**Forgetting `resp.ToParam()`.** The model's response has to be appended back to `messages` before the next loop iteration — otherwise the model has no idea what it said last turn. The SDK's `.ToParam()` converts the response into the right shape. Easy to skip the first time you write this.

**Tool result IDs.** Every `tool_use` block has an `id`; every `tool_result` you send back must reference that id via `tool_use_id`. If they don't match, the API returns a 400 about an orphaned tool result. The SDK's `NewToolResultBlock(id, content, isErr)` builds the block for you.

**Loop termination.** If you check the wrong field (e.g., `stop_reason == "end_turn"` instead of `!= "tool_use"`), you'll either loop forever or never loop at all. The reliable check is "did the response contain any `tool_use` blocks?" — equivalent to `stop_reason == "tool_use"`.

> **In the current repo.** The agent loop lives in [`internal/agent/agent.go`](../internal/agent/agent.go) as the `(*Agent).loop` method (chapter 11 covers why it became a method on a struct). The `executeTool` wrapper at the harness layer is in [`main.go`](../main.go). The single-switch dispatch shown above evolved into a `tool.Registry` — chapter 09 covers that refactor.

## Now try

1. Run the agent and ask it `list the files here`. Watch the `[tool] bash ...` print fly by.
2. Ask it `write a hello.txt with a haiku in it`. Two tool calls in one turn — observe the loop.
3. Ask it `read the file /does/not/exist`. The model gets back an error string and either reports it back to you or tries a different path. This is the "errors as tool results" contract in action.

Next: [02 · The permission gate](02-the-permission-gate.md).
