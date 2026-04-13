# Go-Claw-Code

**中文** | [English](README_EN.md)

[Claude Code](https://docs.anthropic.com/en/docs/claude-code) 的 Go 语言重新实现 — Anthropic 的智能编程 CLI 工具。

**138 个 Go 文件（103 源文件 + 35 测试文件）· 4.2 万行代码 · 4 个直接依赖 · 零 AI SDK** — 直接调用 Anthropic HTTP API，支持 SSE 流式输出。**319 个测试用例 · 全部通过。**

> 本项目为独立社区实现，与 Anthropic 无关，也未获其认可。

---

## 功能特性

### 43 个内置工具

| 分类 | 工具 |
|------|------|
| 文件操作 | Read（支持图片/PDF）、Write、Edit、MultiEdit、Glob、Grep、RepoMap |
| Shell | Bash、PowerShell、REPL |
| 网络 | WebFetch、WebSearch |
| 浏览器 | browser_new_tab、browser_close_tab、browser_list_tabs、browser_navigate、browser_click、browser_click_at、browser_type、browser_screenshot、browser_get_content、browser_scroll、browser_back、browser_eval、browser_press_key、browser_set_files、browser_site_experience、browser_jina |
| 桌面控制 | ComputerUse（鼠标/键盘/截屏） |
| Agent | Agent（6 种类型）、Skill、SendUserMessage、AskUserQuestion |
| 规划 | EnterPlanMode、ExitPlanMode |
| 任务管理 | TodoWrite、TodoRead、TaskOutput、TaskStop |
| 定时调度 | CronCreate、CronDelete、CronList |
| 记忆 | WriteMemory |
| MCP | MCPReadResource、MCPListResources、MCPListPrompts、MCPGetPrompt |
| 工作树 | EnterWorktree、ExitWorktree |
| 多 Agent | SendMessage、TeamCreate、TeamDelete |
| LSP | LSP（30+ 语言、8 种操作） |
| 语音 | Voice（SoX/ALSA/FFmpeg 录音、WAV 编码） |
| 系统 | ToolSearch、NotebookEdit、Sleep、ClearScreen、StatusLine、Config、StructuredOutput |

### 40 个斜杠命令

`/help` `/status` `/model` `/permissions` `/fast` `/cost` `/compact` `/clear` `/diff` `/undo` `/commit` `/commit-push-pr` `/pr` `/review-pr` `/issue` `/branch` `/worktree` `/export` `/session` `/resume` `/config` `/memory` `/init` `/setup` `/version` `/doctor` `/context` `/todo` `/agents` `/skills` `/plugin` `/debug-tool-call` `/bughunter` `/ultraplan` `/teleport` `/vim` `/statusline` `/reflect` `/grep-tool` `/mcp`

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
- **System Prompt 缓存** — Anthropic `cache_control` 断点，减少重复 token 开销
- **Reflection 模式** — Agent 完成任务后自动注入自评提示，提升回答质量
- **CDP 浏览器代理** — gorilla/websocket 原生 CDP 协议，14 个 HTTP 端点，支持反风控端口拦截
- **Computer Use** — 原生桌面控制（鼠标/键盘/截屏），Windows/macOS/Linux 三平台适配
- **RepoMap** — Aider 风格的仓库符号地图，6 种语言符号提取
- **OpenAI 推理 token** — 支持 o1/o3/DeepSeek-R1 的 reasoning_content 翻译

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
internal/
├── api/                    # Anthropic API（SSE 流式、重试、Token 追踪、OpenAI 推理 token）
├── auth/                   # OAuth/PKCE + 配置向导 + 凭证存储
├── browser/                # CDP 浏览器代理（gorilla/websocket、14 个端点、反风控）
├── commands/               # 40 个斜杠命令
├── config/                 # 多层配置加载 + Feature Flags
├── initrepo/               # 项目初始化
├── lsp/                    # LSP 客户端集成
├── mcp/                    # MCP 协议客户端（stdio + HTTP/SSE）
├── native/                 # 原生桌面控制（鼠标/键盘/截屏，三平台适配）
├── plugins/                # 插件管理器
├── runtime/                # 对话循环、权限、Hook、压缩、会话、Prompt 缓存、Reflection
├── sandbox/                # 进程隔离
├── server/                 # HTTP 服务器模式
├── tools/                  # 43 个工具实现（含浏览器、桌面控制、RepoMap）
├── tui/                    # Bubble Tea TUI
└── voice/                  # 语音录音与转写
```

## 与 Rust 版 Claude Code 对比

| 特性 | Claude Code (Rust) | Go-Claw-Code |
|------|--------------------|--------------|
| 语言 | Rust | Go |
| 二进制大小 | ~80MB | ~19MB |
| 编译时间 | 数分钟 | 数秒 |
| 工具数 | 38+ | 43 |
| 斜杠命令 | 38+ | 40 |
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
| 浏览器控制 | no | yes（CDP Proxy 14 端点） |
| 桌面控制 | no | yes（Computer Use） |
| Prompt 缓存 | yes | yes（cache_control 断点） |
| Reflection | no | yes |
| RepoMap | no | yes |

## 与 claude-code-go 对比

[claude-code-go](https://github.com/zwl698/claude-code-go) 是另一个 Claude Code 的 Go 实现，从 npm 包的 TypeScript source map 反编译翻译而来。

### 概览

| | **Go-Claw-Code** | **claude-code-go** |
|--|------------------|--------------------|
| 来源 | 原创实现 | 从 npm 包 source map 反编译翻译 |
| Go 文件 | 138 | 114 |
| 代码行数 | ~4.2 万 | ~3.5 万 |
| 测试文件 | **35 个**（319 用例，全部通过） | 1 个 |
| CLI 框架 | flag（标准库） | Cobra |
| 文档语言 | 英文 + 中文 | 中文 |

### Provider 支持

| Provider | Go-Claw-Code | claude-code-go |
|----------|:------------:|:--------------:|
| Anthropic | yes | yes |
| 智谱 GLM | **yes** | no |
| DeepSeek | **yes** | no |
| OpenAI | **yes** | no |
| AWS Bedrock | **yes** | yes |
| Google Vertex | **yes** | yes |
| Azure Foundry | **yes** | yes |

### 功能对比

| 功能 | Go-Claw-Code | claude-code-go |
|------|:------------:|:--------------:|
| 工具数 | **43** | 30+ |
| 斜杠命令 | **40** | 7 |
| Agent 类型 | 6（带工具过滤） | 仅 TaskTool |
| 插件系统 | **yes** | no |
| 记忆系统 | **yes** | no |
| 首次运行向导 | **yes** | no |
| 独立配置 | **yes**（`~/.go-claw/`） | no（共享 `~/.claude/`） |
| 多 Agent 团队 | **yes** | yes |
| 语音 | **yes** | yes |
| LSP 集成 | **yes**（30+ 语言） | 完整（30+ 语言） |
| MCP 客户端 | yes | yes |
| Vim 模式 | yes | yes |
| LLM 压缩 | yes | yes |
| 定时调度 | yes | yes |
| 工具错误保留 | **yes**（保留 stderr） | — |
| Windows 适配 | **yes**（CRLF、cmd.exe） | PowerShell 工具 |
| 浏览器控制 | **yes**（CDP Proxy 14 端点） | no |
| 桌面控制 | **yes**（Computer Use） | no |
| Prompt 缓存 | **yes**（cache_control 断点） | no |
| Reflection | **yes** | no |
| RepoMap | **yes**（6 种语言） | no |

### 总结

- **选择 Go-Claw-Code**：多模型支持（GLM/DeepSeek/OpenAI）、测试覆盖、丰富的斜杠命令、插件扩展、记忆系统、首次运行向导、与 Claude Code 共存的独立配置、企业云 Provider（Bedrock/Vertex/Foundry）、多 Agent 团队、语音、LSP 30+ 语言支持、浏览器控制（CDP Proxy）、桌面控制（Computer Use）、Prompt 缓存、Reflection、RepoMap

## License

MIT
