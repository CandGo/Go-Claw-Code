# Go-Claw-Code

[English](README.md) | **中文**

[Claude Code](https://docs.anthropic.com/en/docs/claude-code) 的 Go 语言重新实现 — Anthropic 的智能编程 CLI 工具。

**96 个 Go 文件 · 3.4 万行代码 · 4 个直接依赖 · 零 AI SDK** — 直接调用 Anthropic HTTP API，支持 SSE 流式输出。

> 本项目为独立社区实现，与 Anthropic 无关，也未获其认可。

---

## 功能特性

### 38 个内置工具

| 分类 | 工具 |
|------|------|
| 文件操作 | Read（支持图片/PDF）、Write、Edit、MultiEdit、Glob、Grep |
| Shell | Bash、PowerShell、REPL |
| 网络 | WebFetch、WebSearch |
| Agent | Agent（6 种类型）、Skill、SendUserMessage、AskUserQuestion |
| 规划 | EnterPlanMode、ExitPlanMode |
| 任务管理 | TodoWrite、TodoRead、TaskOutput、TaskStop |
| 定时调度 | CronCreate、CronDelete、CronList |
| 记忆 | WriteMemory |
| MCP | MCPReadResource、MCPListResources、MCPListPrompts、MCPGetPrompt |
| 工作树 | EnterWorktree、ExitWorktree |
| 系统 | ToolSearch、NotebookEdit、Sleep、ClearScreen、StatusLine、Config、StructuredOutput |

### 39 个斜杠命令

`/help` `/status` `/model` `/permissions` `/fast` `/cost` `/compact` `/clear` `/diff` `/undo` `/commit` `/commit-push-pr` `/pr` `/review-pr` `/issue` `/branch` `/worktree` `/export` `/session` `/resume` `/config` `/memory` `/init` `/setup` `/version` `/doctor` `/context` `/todo` `/agents` `/skills` `/plugin` `/debug-tool-call` `/bughunter` `/ultraplan` `/teleport` `/vim` `/statusline` `/grep-tool` `/mcp`

### Agent 子执行

| Agent 类型 | 可用工具 | 最大迭代 |
|------------|----------|----------|
| `general-purpose` | 全部工具 | 32 |
| `Explore` | 只读工具 | 5 |
| `Plan` | 只读 + Agent + Todo | 3 |
| `Verification` | 只读 + Bash + PowerShell | 10 |
| `claw-code-guide` | 只读 + SendUserMessage | 8 |
| `statusline-setup` | Bash + Read + Write + Edit | 10 |

### 首次运行配置向导

首次启动时（未检测到凭证），交互式向导自动引导：

- 自动检测并复用已有 Claude Code 配置
- 输入 API Key
- OAuth 浏览器登录
- 配置自定义端点（智谱 GLM、DeepSeek、OpenAI 等）
- 选择模型并自动配置 Base URL

随时运行 `/setup` 重新配置。

### 流式 TUI

基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 构建：

- 实时流式 Markdown 渲染，支持语法高亮（Chroma）
- Vim 键绑定（5 种模式）
- Shift+Enter 多行输入
- 权限确认 UI
- Diff 渲染

### 权限系统

8 种权限模式，支持按工具粒度控制，以及会话级"始终允许"缓存：

`read-only` → `workspace-write` → `danger-full-access`，以及 `prompt`、`plan`、`acceptEdits`、`dontAsk`、`allow`

### Hook 系统

6 种 Hook 事件，通过 Shell 命令执行：

`PreToolUse` / `PostToolUse` / `SubagentBefore` / `SubagentAfter` / `Notification` / `Stop`

支持工具名匹配、30 秒超时、exit code 2 = 拒绝。

### 多模型支持

支持任何 Anthropic 兼容的 API 端点：

| 提供商 | Base URL | 模型 |
|--------|----------|------|
| Anthropic | `https://api.anthropic.com` | claude-sonnet-4-6, claude-opus-4-6, claude-haiku-4-5 |
| 智谱 AI | `https://open.bigmodel.cn/api/paas/v4` | glm-5.1 |
| DeepSeek | `https://api.deepseek.com` | deepseek-chat |
| OpenAI | `https://api.openai.com/v1` | gpt-4o |

### 其他特性

- **LLM 压缩** — 通过 LLM 进行对话摘要，启发式回退
- **MCP 客户端** — stdio + HTTP/SSE 服务器连接，动态工具注册
- **插件系统** — 安装、启用、禁用、卸载插件，支持 Hook
- **OAuth/PKCE** — 浏览器认证，支持 Token 刷新
- **沙箱** — Linux namespace 隔离，Windows 支持
- **会话持久化** — JSON 保存/加载
- **记忆系统** — 项目级和用户级记忆，`MEMORY.md` 索引
- **定时调度** — 循环和一次性定时提示

---

## 快速开始

### 前置条件

- Go 1.24+
- API Key（Anthropic、智谱、DeepSeek 或任何 OpenAI 兼容端点）

### 构建

```bash
git clone https://github.com/candgo1/go-claw.git
cd go-claw
go build ./cmd/go-claw-code/
```

### 运行

```bash
# 交互式 REPL（首次运行启动配置向导）
./go-claw-code

# 单次模式
./go-claw-code "列出这个项目所有 TODO 注释"

# 指定模型和权限模式
./go-claw-code --model glm-5.1 --permission-mode danger-full-access "修复这个 bug"
```

### 环境变量

| 变量 | 用途 |
|------|------|
| `ANTHROPIC_API_KEY` | API Key（也供 Claude Code 使用） |
| `ANTHROPIC_BASE_URL` | API 端点覆盖 |
| `ANTHROPIC_MODEL` | 默认模型 |
| `CLAW_API_KEY` | Go-Claw 专用 API Key（优先级更高） |
| `CLAW_BASE_URL` | Go-Claw 专用 Base URL（优先级更高） |
| `CLAW_MODEL` | Go-Claw 专用模型（优先级更高） |
| `CLAW_PERMISSION_MODE` | 权限模式覆盖 |

### 与 Claude Code 共存

Go-Claw-Code 使用独立的配置目录（`~/.go-claw/`），与 Claude Code（`~/.claude/`）完全独立。两个工具可以同时安装、并行使用。

---

## 架构

```
cmd/go-claw-code/           # 入口
cmd/tetris/                 # 俄罗斯方块演示（Bubble Tea TUI）
internal/
├── api/                    # Anthropic API（SSE 流式、重试、Token 追踪）
├── auth/                   # OAuth/PKCE + 配置向导 + 凭证存储
├── commands/               # 39 个斜杠命令
├── config/                 # 多层配置加载
├── initrepo/               # 项目初始化
├── lsp/                    # LSP 客户端集成
├── mcp/                    # MCP 协议客户端（stdio + HTTP/SSE）
├── plugins/                # 插件管理器
├── runtime/                # 对话循环、权限、Hook、压缩、会话
├── sandbox/                # 进程隔离
├── server/                 # HTTP 服务器模式
├── tools/                  # 38 个工具实现
└── tui/                    # Bubble Tea TUI
```

## 与 Rust 版 Claude Code 对比

| 特性 | Claude Code (Rust) | Go-Claw-Code |
|------|--------------------|--------------|
| 语言 | Rust | Go |
| 二进制大小 | ~80MB | ~17MB |
| 编译时间 | 数分钟 | 数秒 |
| 工具数 | 38+ | 38 |
| 斜杠命令 | 38+ | 39 |
| Agent 类型 | 6 | 6 |
| 权限模式 | 8 | 8 |
| Hook 事件 | 6 | 6 |
| 流式 TUI | yes | yes |
| MCP 客户端 | yes | yes |
| 插件系统 | no | yes |
| 多模型支持 | 仅 Claude | Claude + GLM + DeepSeek + OpenAI |
| OAuth/PKCE | yes | yes |
| 沙箱 | Linux namespaces | Linux namespaces |
| LLM 压缩 | yes | yes |
| 记忆系统 | yes | yes |
| 首次运行向导 | no | yes |
| 独立配置 | no | yes |

## 与 claude-code-go 对比

[claude-code-go](https://github.com/zwl698/claude-code-go) 是另一个 Claude Code 的 Go 实现，从 npm 包的 TypeScript source map 反编译翻译而来。

### 概览

| | **Go-Claw-Code** | **claude-code-go** |
|--|------------------|--------------------|
| 来源 | 原创实现 | 从 npm 包 source map 反编译翻译 |
| Go 文件 | 96 | 114 |
| 代码行数 | ~3.4 万 | ~3.5 万 |
| 测试文件 | **26 个**（全部通过） | 1 个 |
| CLI 框架 | flag（标准库） | Cobra |
| 文档语言 | 英文 + 中文 | 中文 |

### Provider 支持

| Provider | Go-Claw-Code | claude-code-go |
|----------|:------------:|:--------------:|
| Anthropic | yes | yes |
| 智谱 GLM | **yes** | no |
| DeepSeek | **yes** | no |
| OpenAI | **yes** | no |
| AWS Bedrock | no | yes |
| Google Vertex | no | yes |
| Azure Foundry | no | yes |

### 功能对比

| 功能 | Go-Claw-Code | claude-code-go |
|------|:------------:|:--------------:|
| 工具数 | 38 | 30+ |
| 斜杠命令 | **39** | 7 |
| Agent 类型 | 6（带工具过滤） | 仅 TaskTool |
| 插件系统 | **yes** | no |
| 记忆系统 | **yes** | no |
| 首次运行向导 | **yes** | no |
| 独立配置 | **yes**（`~/.go-claw/`） | no（共享 `~/.claude/`） |
| 多 Agent 团队 | no | yes |
| 语音 | no | yes |
| LSP 集成 | 基础 | 完整（30+ 语言） |
| MCP 客户端 | yes | yes |
| Vim 模式 | yes | yes |
| LLM 压缩 | yes | yes |
| 定时调度 | yes | yes |
| 工具错误保留 | **yes**（保留 stderr） | — |
| Windows 适配 | **yes**（CRLF、cmd.exe） | PowerShell 工具 |

### 总结

- **选择 Go-Claw-Code**：多模型支持（GLM/DeepSeek/OpenAI）、测试覆盖、丰富的斜杠命令、插件扩展、记忆系统、首次运行向导、与 Claude Code 共存的独立配置
- **选择 claude-code-go**：企业云 Provider（Bedrock/Vertex/Foundry）、多 Agent 团队、语音、完整的 LSP 支持

## License

MIT
