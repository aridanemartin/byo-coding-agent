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

## Now try

1. Run the agent and ask it `list the files here`. Watch the `[tool] bash ...` print fly by.
2. Ask it `write a hello.txt with a haiku in it`. Two tool calls in one turn — observe the loop.
3. Ask it `read the file /does/not/exist`. The model gets back an error string and either reports it back to you or tries a different path. This is the "errors as tool results" contract in action.

Next: [02 · The permission gate](02-the-permission-gate.md).
