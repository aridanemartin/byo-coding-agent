package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/betta-tech/byo-coding-agent/internal/compact"
	"github.com/betta-tech/byo-coding-agent/internal/ui"
)

type command struct {
	description string
	usage       string // optional; falls back to "/name"
	run         func(args string)
}

var commands = map[string]command{}

func init() {
	commands["help"] = command{description: "show available commands", run: cmdHelp}
	commands["model"] = command{description: "show or change the model", usage: "/model [name]", run: cmdModel}
	commands["clear"] = command{description: "clear conversation history", run: cmdClear}
	commands["tools"] = command{description: "list available tools", run: cmdTools}
	commands["compact"] = command{
		description: "run compaction now (optionally with a specific strategy)",
		usage:       "/compact [sliding|summarize|none]",
		run:         cmdCompact,
	}
	commands["verbose"] = command{
		description: "toggle printing of compaction before/after",
		usage:       "/verbose [on|off]",
		run:         cmdVerbose,
	}
	commands["exit"] = command{description: "exit the harness", run: cmdExit}
}

// verbose controls whether compaction events print before/after to stdout.
// Read by agentLoop and cmdCompact; toggled by /verbose.
var verbose bool

// runCommand handles a line that starts with "/". Returns true if handled,
// false if the line should fall through to the model as a normal message.
func runCommand(line string) bool {
	if !strings.HasPrefix(line, "/") {
		return false
	}
	parts := strings.SplitN(strings.TrimPrefix(line, "/"), " ", 2)
	name := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	c, ok := commands[name]
	if !ok {
		fmt.Printf("unknown command: /%s (try /help)\n", name)
		return true
	}
	c.run(args)
	return true
}

func cmdHelp(_ string) {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Println(ui.Dimmed("available commands:"))
	for _, n := range names {
		c := commands[n]
		display := "/" + n
		if c.usage != "" {
			display = c.usage
		}
		fmt.Printf("  %-20s %s\n", display, ui.Dimmed(c.description))
	}
}

// knownModels are suggestions shown by /model with no args. Any model id can
// be set — validation happens at the next Send call.
var knownModels = []string{
	"claude-opus-4-7",
	"claude-opus-4-6",
	"claude-sonnet-4-6",
	"claude-haiku-4-5",
}

func cmdModel(args string) {
	if args == "" {
		fmt.Printf("current: %s\n", ui.Cyan(llm.Model()))
		fmt.Println(ui.Dimmed("suggestions:"))
		for _, m := range knownModels {
			fmt.Printf("  %s\n", m)
		}
		fmt.Println(ui.Dimmed("(or pass any model id — validated on next call)"))
		return
	}
	llm.SetModel(args)
	fmt.Printf("model: %s\n", ui.Cyan(args))
}

func cmdClear(_ string) {
	messages = messages[:0]
	fmt.Println(ui.Dimmed("conversation cleared"))
}

func cmdTools(_ string) {
	fmt.Println(ui.Dimmed("tools available:"))
	for _, t := range registry.Definitions() {
		fmt.Printf("  %s  %s\n", ui.Cyan(t.Name), ui.Dimmed(t.Description))
	}
}

func cmdCompact(args string) {
	strategy := compactor
	switch strings.ToLower(args) {
	case "":
		// use the configured compactor
	case "sliding":
		strategy = &compact.SlidingWindow{KeepLast: 6}
	case "summarize":
		strategy = &compact.Summarize{Provider: llm, Threshold: 0, KeepRecent: 4}
	case "none":
		strategy = compact.NoCompaction{}
	default:
		fmt.Printf("unknown strategy: %s (try sliding, summarize, or none)\n", args)
		return
	}

	before := messages
	compacted, err := strategy.Compact(context.Background(), messages)
	if err != nil {
		fmt.Printf("compaction error: %v\n", err)
		return
	}
	messages = compacted
	fmt.Printf("%s\n", ui.Dimmed(fmt.Sprintf("compacted: %d → %d messages", len(before), len(compacted))))
	if verbose && len(before) != len(compacted) {
		ui.PrintCompaction(before, compacted)
	}
}

func cmdVerbose(args string) {
	switch strings.ToLower(args) {
	case "":
		verbose = !verbose
	case "on", "true", "yes":
		verbose = true
	case "off", "false", "no":
		verbose = false
	default:
		fmt.Printf("unknown value: %s (try on/off)\n", args)
		return
	}
	state := "off"
	if verbose {
		state = "on"
	}
	fmt.Printf("verbose: %s\n", state)
}

func cmdExit(_ string) {
	os.Exit(0)
}
