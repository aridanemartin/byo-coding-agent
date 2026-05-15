# Exercise 1 — Tool retry on error

**Goal:** when a tool call fails, give the model one or two structured retries before surfacing the failure to the user.

**Difficulty:** easy. **Time:** 30–60 minutes. **Touches:** `internal/agent/agent.go`.

## What's already in place

The agent loop in `internal/agent/agent.go` already routes tool errors back to the model: the tool's output is wrapped in an `api.Block` with `IsError: true`, and the model gets to decide what to do next. That's flexible — but there's nothing stopping the model from calling the same broken tool with the same broken input in a tight loop.

You can read the relevant lines around `toolResults` and `IsError` in `internal/agent/agent.go:107–137`.

## What to build

A small `RepairPolicy` that:

- counts consecutive failures of the same tool (same name, ideally same input hash),
- on retry, injects a short meta-note into the tool result like *"this is retry 2/3; if you can't recover, ask the user"*,
- after `MaxRetries`, aborts the tool dispatch and surfaces a clear error to the user.

## Suggested steps

1. **Define the policy.** Somewhere in `internal/agent/` (or a new `internal/repair/` package):

   ```go
   type RepairPolicy struct {
       MaxRetries int
       Note       func(toolName string, attempt, max int) string // returns the meta-note
   }
   ```

2. **Track state on the agent.** Add a `repairCounts map[string]int` field on `Agent`. Reset entries when a tool succeeds or when the model moves on to a different tool/input.

3. **Wire it into the loop.** In the tool-dispatch block where `IsError` is set, consult the policy. Either append the note to the tool result content, or bump the count and abort.

4. **Make it pluggable in main.go.** Mirror the other extension points:

   ```go
   a.Repair = &agent.RepairPolicy{MaxRetries: 3}
   ```

   A nil policy means "current behaviour, unlimited retries."

## Acceptance

- A `bash` command that fails returns to the model with a `retry 1/3` note.
- The same command failing again gets `retry 2/3`, then aborts at 3 with a single user-facing error message.
- The cap is configurable from `main.go`.
- A `MockProvider`-based test (see `internal/provider/mock.go`) confirms the abort happens at the configured retry count without an API call.

## Stretch

- Report repair statistics in the `/debug` panel (count of repairs by tool name).
- Different policies per tool: `bash` gets 1 retry, `read_file` gets 3.
- Hash the tool input so *"same tool, different argument"* doesn't count as a retry.
