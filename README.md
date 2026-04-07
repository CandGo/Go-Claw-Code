# Go-Claw-Code

A Go reimplementation of [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — Anthropic's agentic coding CLI.

**96 Go files · 34K lines · 4 direct dependencies · zero AI SDKs** — raw Anthropic HTTP API with SSE streaming.

> This is an independent community implementation and is not affiliated with or endorsed by Anthropic.

---

## Features

### 38 Built-in Tools

| Category | Tools |
|----------|-------|
| File | Read (image/PDF), Write, Edit, MultiEdit, Glob, Grep |
| Shell | Bash, PowerShell, REPL |
| Web | WebFetch, WebSearch |
| Agent | Agent (6 types), Skill, SendUserMessage, AskUserQuestion |
| Planning | EnterPlanMode, ExitPlanMode |
| Tasks | TodoWrite, TodoRead, TaskOutput, TaskStop |
| Scheduling | CronCreate, CronDelete, CronList |
| Memory | WriteMemory |
| MCP | MCPReadResource, MCPListResources, MCPListPrompts, MCPGetPrompt |
| Worktree | EnterWorktree, ExitWorktree |
| System | ToolSearch, NotebookEdit, Sleep, ClearScreen, StatusLine, Config, StructuredOutput |

### 39 Slash Commands

`/help` `/status` `/model` `/permissions` `/fast` `/cost` `/compact` `/clear` `/diff` `/undo` `/commit` `/commit-push-pr` `/pr` `/review-pr` `/issue` `/branch` `/worktree` `/export` `/session` `/resume` `/config` `/memory` `/init` `/setup` `/version` `/doctor` `/context` `/todo` `/agents` `/skills` `/plugin` `/debug-tool-call` `/bughunter` `/ultraplan` `/teleport` `/vim` `/statusline` `/grep-tool` `/mcp`

### Agent Sub-Execution

| Agent Type | Tools | Max Iter |
|------------|-------|----------|
| `general-purpose` | All | 32 |
| `Explore` | Read-only | 5 |
| `Plan` | Read-only + Agent + Todo | 3 |
| `Verification` | Read-only + Bash + PowerShell | 10 |
| `claw-code-guide` | Read-only + SendUserMessage | 8 |
| `statusline-setup` | Bash + Read + Write + Edit | 10 |

### First-Run Setup Wizard

On first launch (no credentials detected), an interactive wizard guides you through:

- Reuse existing Claude Code config (auto-detected)
- Enter API Key
- OAuth browser login
- Custom endpoint (Zhipu GLM, DeepSeek, OpenAI, etc.)
- Model selection with auto-configured base URL

Run `/setup` at any time to reconfigure.

### Streaming TUI

Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea):

- Real-time streaming markdown with syntax highlighting (Chroma)
- Vim keybindings (5 modes)
- Multiline input with Shift+Enter
- Permission prompt UI
- Diff rendering

### Permission System

8 permission modes with per-tool granularity and session-persistent "allow always" caching:

`read-only` → `workspace-write` → `danger-full-access`, plus `prompt`, `plan`, `acceptEdits`, `dontAsk`, `allow`

### Hook System

6 hook events with shell command execution:

`PreToolUse` / `PostToolUse` / `SubagentBefore` / `SubagentAfter` / `Notification` / `Stop`

Hooks support tool pattern matching, 30s timeout, and exit code 2 = deny.

### Multi-Provider Support

Works with any Anthropic-compatible API endpoint:

| Provider | Base URL | Models |
|----------|----------|--------|
| Anthropic | `https://api.anthropic.com` | claude-sonnet-4-6, claude-opus-4-6, claude-haiku-4-5 |
| Zhipu AI | `https://open.bigmodel.cn/api/paas/v4` | glm-5.1 |
| DeepSeek | `https://api.deepseek.com` | deepseek-chat |
| OpenAI | `https://api.openai.com/v1` | gpt-4o |

### Additional Features

- **LLM Compaction** — conversation summarization via LLM with heuristic fallback
- **MCP Client** — stdio + HTTP/SSE server connections, dynamic tool registration
- **Plugin System** — install, enable, disable, uninstall plugins with hook support
- **OAuth/PKCE** — browser-based auth with token refresh
- **Sandbox** — Linux namespace isolation, Windows support
- **Session Persistence** — JSON save/load
- **Memory System** — project and user-level memory with `MEMORY.md` index
- **Cron Scheduler** — recurring and one-shot scheduled prompts

---

## Quick Start

### Prerequisites

- Go 1.24+
- An API key (Anthropic, Zhipu, DeepSeek, or any OpenAI-compatible endpoint)

### Build

```bash
git clone https://github.com/candgo1/go-claw.git
cd go-claw
go build ./cmd/go-claw-code/
```

### Run

```bash
# Interactive REPL (first run launches setup wizard)
./go-claw-code

# One-shot mode
./go-claw-code "list all TODO comments in this project"

# Specify model and permission mode
./go-claw-code --model glm-5.1 --permission-mode danger-full-access "fix the bug"
```

### Environment Variables

| Variable | Purpose |
|----------|---------|
| `ANTHROPIC_API_KEY` | API key (also used by Claude Code) |
| `ANTHROPIC_BASE_URL` | API endpoint override |
| `ANTHROPIC_MODEL` | Default model |
| `CLAW_API_KEY` | Go-Claw-specific API key (takes priority) |
| `CLAW_BASE_URL` | Go-Claw-specific base URL (takes priority) |
| `CLAW_MODEL` | Go-Claw-specific model (takes priority) |
| `CLAW_PERMISSION_MODE` | Permission mode override |

### Coexisting with Claude Code

Go-Claw-Code uses its own config directory (`~/.go-claw/`) — fully independent from Claude Code (`~/.claude/`). Both tools can be installed and used simultaneously.

---

## Architecture

```
cmd/go-claw-code/           # Entry point
cmd/tetris/                 # Tetris demo (Bubble Tea TUI)
internal/
├── api/                    # Anthropic API (SSE streaming, retry, token tracking)
├── auth/                   # OAuth/PKCE + setup wizard + credentials
├── commands/               # 39 slash commands
├── config/                 # Multi-layer config loading
├── initrepo/               # Project initialization
├── lsp/                    # LSP client integration
├── mcp/                    # MCP protocol client (stdio + HTTP/SSE)
├── plugins/                # Plugin manager
├── runtime/                # Conversation loop, permissions, hooks, compaction, sessions
├── sandbox/                # Process isolation
├── server/                 # HTTP server mode
├── tools/                  # 38 tool implementations
└── tui/                    # Bubble Tea TUI
```

## Comparison with Rust Claude Code

| Feature | Claude Code (Rust) | Go-Claw-Code |
|---------|--------------------|--------------|
| Language | Rust | Go |
| Binary size | ~80MB | ~17MB |
| Build time | minutes | seconds |
| Tools | 38+ | 38 |
| Slash Commands | 38+ | 39 |
| Agent Types | 6 | 6 |
| Permission Modes | 8 | 8 |
| Hook Events | 6 | 6 |
| Streaming TUI | yes | yes |
| MCP Client | yes | yes |
| Plugin System | no | yes |
| Multi-Provider | Claude only | Claude + GLM + DeepSeek + OpenAI |
| OAuth/PKCE | yes | yes |
| Sandbox | Linux namespaces | Linux namespaces |
| LLM Compaction | yes | yes |
| Memory System | yes | yes |
| First-Run Wizard | no | yes |
| Independent Config | no | yes |

## Comparison with claude-code-go

[claude-code-go](https://github.com/zwl698/claude-code-go) is another Go implementation of Claude Code, translated from the npm package's TypeScript source maps.

### Overview

| | **Go-Claw-Code** | **claude-code-go** |
|--|------------------|--------------------|
| Source | Original implementation | npm package reverse-engineered from source maps |
| Go files | 96 | 114 |
| Lines of code | ~34K | ~35K |
| Test files | **26** (all passing) | 1 |
| CLI framework | flag (stdlib) | Cobra |
| Docs language | English | Chinese |

### Provider Support

| Provider | Go-Claw-Code | claude-code-go |
|----------|:------------:|:--------------:|
| Anthropic | yes | yes |
| Zhipu GLM | **yes** | no |
| DeepSeek | **yes** | no |
| OpenAI | **yes** | no |
| AWS Bedrock | no | yes |
| Google Vertex | no | yes |
| Azure Foundry | no | yes |

### Features

| Feature | Go-Claw-Code | claude-code-go |
|---------|:------------:|:--------------:|
| Tools | 38 | 30+ |
| Slash Commands | **39** | 7 |
| Agent Types | 6 (with tool filtering) | TaskTool only |
| Plugin System | **yes** | no |
| Memory System | **yes** | no |
| First-Run Wizard | **yes** | no |
| Independent Config | **yes** (`~/.go-claw/`) | no (shares `~/.claude/`) |
| Multi-Agent Teams | no | yes |
| Voice | no | yes |
| LSP Integration | basic | full (30+ languages) |
| MCP Client | yes | yes |
| Vim Mode | yes | yes |
| LLM Compaction | yes | yes |
| Cron Scheduling | yes | yes |
| Tool Error Preservation | **yes** (stderr preserved) | — |
| Windows Adaptation | **yes** (CRLF, cmd.exe) | PowerShell tool |

### Summary

- **Choose Go-Claw-Code** for: multi-model support (GLM/DeepSeek/OpenAI), test coverage, rich slash commands, plugin extensibility, memory system, first-run wizard, independent config coexisting with Claude Code
- **Choose claude-code-go** for: enterprise cloud providers (Bedrock/Vertex/Foundry), multi-agent teams, voice, comprehensive LSP support

## License

MIT
