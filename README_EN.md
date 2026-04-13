# Go-Claw-Code

**English** | [中文](README.md)

A Go reimplementation of [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — Anthropic's agentic coding CLI.

**138 Go files (103 source + 35 test) · 42K lines · 4 direct dependencies · zero AI SDKs** — raw Anthropic HTTP API with SSE streaming. **319 test cases · all passing.**

> This is an independent community implementation and is not affiliated with or endorsed by Anthropic.

---

## Features

### 43 Built-in Tools

| Category | Tools |
|----------|-------|
| File | Read (image/PDF), Write, Edit, MultiEdit, Glob, Grep, RepoMap |
| Shell | Bash, PowerShell, REPL |
| Web | WebFetch, WebSearch |
| Browser | browser_new_tab, browser_close_tab, browser_list_tabs, browser_navigate, browser_click, browser_click_at, browser_type, browser_screenshot, browser_get_content, browser_scroll, browser_back, browser_eval, browser_press_key, browser_set_files, browser_site_experience, browser_jina |
| Desktop | ComputerUse (mouse/keyboard/screenshot) |
| Agent | Agent (6 types), Skill, SendUserMessage, AskUserQuestion |
| Planning | EnterPlanMode, ExitPlanMode |
| Tasks | TodoWrite, TodoRead, TaskOutput, TaskStop |
| Scheduling | CronCreate, CronDelete, CronList |
| Memory | WriteMemory |
| MCP | MCPReadResource, MCPListResources, MCPListPrompts, MCPGetPrompt |
| Worktree | EnterWorktree, ExitWorktree |
| Multi-Agent | SendMessage, TeamCreate, TeamDelete |
| LSP | LSP (30+ languages, 8 operations) |
| Voice | Voice (SoX/ALSA/FFmpeg recording, WAV encoding) |
| System | ToolSearch, NotebookEdit, Sleep, ClearScreen, StatusLine, Config, StructuredOutput |

### 40 Slash Commands

`/help` `/status` `/model` `/permissions` `/fast` `/cost` `/compact` `/clear` `/diff` `/undo` `/commit` `/commit-push-pr` `/pr` `/review-pr` `/issue` `/branch` `/worktree` `/export` `/session` `/resume` `/config` `/memory` `/init` `/setup` `/version` `/doctor` `/context` `/todo` `/agents` `/skills` `/plugin` `/debug-tool-call` `/bughunter` `/ultraplan` `/teleport` `/vim` `/statusline` `/reflect` `/grep-tool` `/mcp`

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
- **System Prompt Caching** — Anthropic `cache_control` breakpoints to reduce token costs
- **Reflection Mode** — self-evaluation prompt after agent completes work to improve quality
- **CDP Browser Proxy** — gorilla/websocket raw CDP protocol, 14 HTTP endpoints, anti-detection port guard
- **Computer Use** — native desktop control (mouse/keyboard/screenshot), Windows/macOS/Linux
- **RepoMap** — Aider-style repository symbol map, 6-language symbol extraction
- **OpenAI Reasoning Tokens** — reasoning_content translation for o1/o3/DeepSeek-R1

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
internal/
├── api/                    # Anthropic API (SSE streaming, retry, token tracking, OpenAI reasoning)
├── auth/                   # OAuth/PKCE + setup wizard + credentials
├── browser/                # CDP browser proxy (gorilla/websocket, 14 endpoints, anti-detection)
├── commands/               # 40 slash commands
├── config/                 # Multi-layer config loading + Feature Flags
├── initrepo/               # Project initialization
├── lsp/                    # LSP client integration
├── mcp/                    # MCP protocol client (stdio + HTTP/SSE)
├── native/                 # Native desktop control (mouse/keyboard/screenshot, cross-platform)
├── plugins/                # Plugin manager
├── runtime/                # Conversation loop, permissions, hooks, compaction, sessions, prompt caching, reflection
├── sandbox/                # Process isolation
├── server/                 # HTTP server mode
├── tools/                  # 43 tool implementations (incl. browser, desktop, RepoMap)
├── tui/                    # Bubble Tea TUI
└── voice/                  # Voice recording and transcription
```

## Comparison with Rust Claude Code

| Feature | Claude Code (Rust) | Go-Claw-Code |
|---------|--------------------|--------------|
| Language | Rust | Go |
| Binary size | ~80MB | ~19MB |
| Build time | minutes | seconds |
| Tools | 38+ | 43 |
| Slash Commands | 38+ | 40 |
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
| Browser Control | no | yes (CDP Proxy, 14 endpoints) |
| Computer Use | no | yes (mouse/keyboard/screenshot) |
| Prompt Caching | yes | yes (cache_control breakpoints) |
| Reflection | no | yes |
| RepoMap | no | yes |

## Comparison with claude-code-go

[claude-code-go](https://github.com/zwl698/claude-code-go) is another Go implementation of Claude Code, translated from the npm package's TypeScript source maps.

### Overview

| | **Go-Claw-Code** | **claude-code-go** |
|--|------------------|--------------------|
| Source | Original implementation | npm package reverse-engineered from source maps |
| Go files | 138 | 114 |
| Lines of code | ~42K | ~35K |
| Test files | **35** (319 cases, all passing) | 1 |
| CLI framework | flag (stdlib) | Cobra |
| Docs language | English | Chinese |

### Provider Support

| Provider | Go-Claw-Code | claude-code-go |
|----------|:------------:|:--------------:|
| Anthropic | yes | yes |
| Zhipu GLM | **yes** | no |
| DeepSeek | **yes** | no |
| OpenAI | **yes** | no |
| AWS Bedrock | **yes** | yes |
| Google Vertex | **yes** | yes |
| Azure Foundry | **yes** | yes |

### Features

| Feature | Go-Claw-Code | claude-code-go |
|---------|:------------:|:--------------:|
| Tools | **43** | 30+ |
| Slash Commands | **40** | 7 |
| Agent Types | 6 (with tool filtering) | TaskTool only |
| Plugin System | **yes** | no |
| Memory System | **yes** | no |
| First-Run Wizard | **yes** | no |
| Independent Config | **yes** (`~/.go-claw/`) | no (shares `~/.claude/`) |
| Multi-Agent Teams | **yes** | yes |
| Voice | **yes** | yes |
| LSP Integration | **yes** (30+ languages) | full (30+ languages) |
| MCP Client | yes | yes |
| Vim Mode | yes | yes |
| LLM Compaction | yes | yes |
| Cron Scheduling | yes | yes |
| Tool Error Preservation | **yes** (stderr preserved) | — |
| Windows Adaptation | **yes** (CRLF, cmd.exe) | PowerShell tool |
| Browser Control | **yes** (CDP Proxy, 14 endpoints) | no |
| Computer Use | **yes** (mouse/keyboard/screenshot) | no |
| Prompt Caching | **yes** (cache_control breakpoints) | no |
| Reflection | **yes** | no |
| RepoMap | **yes** (6 languages) | no |

### Summary

- **Choose Go-Claw-Code** for: multi-model support (GLM/DeepSeek/OpenAI), test coverage, rich slash commands, plugin extensibility, memory system, first-run wizard, independent config coexisting with Claude Code, enterprise cloud providers (Bedrock/Vertex/Foundry), multi-agent teams, voice, LSP with 30+ languages, browser control (CDP Proxy), desktop control (Computer Use), prompt caching, reflection, RepoMap

## License

MIT
