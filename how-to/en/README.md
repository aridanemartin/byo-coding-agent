# How-to guides

Recipe-style references for the most common extension tasks. These are short and assume you've read the corresponding `follow_along/` chapter — they tell you *how*, not *why*.

| Guide | When you need it |
|---|---|
| [Add a new tool](add-a-tool.md) | The model needs a new capability — `web_fetch`, `grep`, `git_log`, anything you can implement in Go |
| [Add a new provider](add-a-provider.md) | You want to swap Anthropic for OpenAI, Bedrock, Ollama, or a mock |
| [Add a permission policy](add-a-permission-policy.md) | You want something other than "ask every time" — auto-allow read-only tools, ask once per tool, deny by path |

Each guide is a worked example you can copy. Want a recipe that isn't here? Open a PR.
