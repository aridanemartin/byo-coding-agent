// Bare-bones coding agent from follow_along chapter 01 — ported to TypeScript.
// Inner loop, outer REPL, three tools dispatched by a switch, nothing else.
//
// Read alongside follow_along/en/01-the-agent-loop.md to see the essence of
// the harness before TUI, compaction, MCP, or subagents get added on top.
//
// Run:
//
//   export ANTHROPIC_API_KEY=sk-ant-...
//   npx tsx index.ts
//
// Type at the > prompt. ctrl-D quits.

import Anthropic from "@anthropic-ai/sdk";
import * as readline from "readline";
import { spawnSync } from "child_process";
import * as fs from "fs";

const systemPrompt = `You are a coding assistant running in a terminal. You have three tools:
bash, read_file, write_file. Be concise.`;

const tools: Anthropic.Tool[] = [
  {
    name: "bash",
    description: "Run a shell command and return its combined stdout/stderr.",
    input_schema: {
      type: "object",
      properties: {
        command: { type: "string", description: "The command to run." },
      },
      required: ["command"],
    },
  },
  {
    name: "read_file",
    description: "Read the contents of a file at the given path.",
    input_schema: {
      type: "object",
      properties: {
        path: { type: "string", description: "Filesystem path to read." },
      },
      required: ["path"],
    },
  },
  {
    name: "write_file",
    description: "Write content to a file (creating or overwriting it).",
    input_schema: {
      type: "object",
      properties: {
        path: { type: "string", description: "Filesystem path to write." },
        content: { type: "string", description: "The bytes to write." },
      },
      required: ["path", "content"],
    },
  },
];

const client = new Anthropic();

// executeTool runs whatever the model asked for. Returns [content, isError] —
// failures travel back to the model so it can recover instead of crashing.
function executeTool(
  name: string,
  input: Record<string, string>
): [string, boolean] {
  console.log(`[tool] ${name} ${JSON.stringify(input)}`);
  switch (name) {
    case "bash": {
      const result = spawnSync("sh", ["-c", input.command], {
        encoding: "utf-8",
      });
      const out = (result.stdout ?? "") + (result.stderr ?? "");
      if (result.status !== 0) {
        return [`${out}\n[exit error: code ${result.status}]`, true];
      }
      return [out, false];
    }
    case "read_file": {
      try {
        return [fs.readFileSync(input.path, "utf-8"), false];
      } catch (err: any) {
        return [err.message, true];
      }
    }
    case "write_file": {
      try {
        fs.writeFileSync(input.path, input.content, { mode: 0o644 });
        return [`wrote ${input.path}`, false];
      } catch (err: any) {
        return [err.message, true];
      }
    }
    default:
      return [`unknown tool: ${name}`, true];
  }
}

// agentLoop calls the model, dispatches tool calls, and loops until the model
// returns plain text or a non-tool stop reason.
async function agentLoop(
  messages: Anthropic.MessageParam[]
): Promise<Anthropic.MessageParam[]> {
  while (true) {
    let resp: Anthropic.Message;
    try {
      resp = await client.messages.create({
        model: "claude-opus-4-7",
        max_tokens: 8192,
        system: systemPrompt,
        messages,
        tools,
      });
    } catch (err: any) {
      console.error(`api error: ${err.message}`);
      return messages;
    }

    messages = [...messages, { role: "assistant", content: resp.content }];

    const toolResults: Anthropic.ToolResultBlockParam[] = [];
    for (const block of resp.content) {
      if (block.type === "text") {
        console.log(block.text);
      } else if (block.type === "tool_use") {
        const [result, isError] = executeTool(
          block.name,
          block.input as Record<string, string>
        );
        toolResults.push({
          type: "tool_result",
          tool_use_id: block.id,
          content: result,
          is_error: isError,
        });
      }
    }

    if (resp.stop_reason !== "tool_use") {
      return messages;
    }

    messages = [...messages, { role: "user", content: toolResults }];
  }
}

async function main() {
  const rl = readline.createInterface({ input: process.stdin });
  let messages: Anthropic.MessageParam[] = [];

  process.stdout.write("> ");
  for await (const line of rl) {
    const input = line.trim();
    if (input !== "") {
      messages = [...messages, { role: "user", content: input }];
      messages = await agentLoop(messages);
    }
    process.stdout.write("> ");
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
