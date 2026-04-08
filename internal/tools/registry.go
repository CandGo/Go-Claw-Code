package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"runtime"
	"time"

	"github.com/CandGo/Go-Claw-Code/internal/api"
	"github.com/CandGo/Go-Claw-Code/internal/sandbox"
)

// PermissionLevel defines the required permission for a tool.
type PermissionLevel int

const (
	PermReadOnly PermissionLevel = iota
	PermWorkspaceWrite
	PermDangerFullAccess
)

// ToolSpec describes a single tool.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(input map[string]interface{}) (string, error)
	Aliases     []string
	Permission  PermissionLevel
}

// ToolRegistry manages all available tools.
type ToolRegistry struct {
	specs map[string]*ToolSpec
}

// globalRegistry is the singleton used by ToolSearch.
var globalRegistry *ToolRegistry

// globalSandbox is the sandbox used for command execution.
var globalSandbox *sandbox.Sandbox

// SetSandbox sets the global sandbox for tool execution.
func SetSandbox(sb *sandbox.Sandbox) {
	globalSandbox = sb
}

// NewToolRegistry creates a registry with all built-in tools.
func NewToolRegistry() *ToolRegistry {
	r := &ToolRegistry{specs: make(map[string]*ToolSpec)}

	// Core file/shell tools
	r.register(bashTool())
	r.register(readTool())
	r.register(writeTool())
	r.register(editTool())
	r.register(globTool())
	r.register(grepTool())

	// Web tools
	r.register(webFetchTool())
	r.register(webSearchTool())

	// Task management
	r.register(todoWriteTool())

	// Agent/skill
	r.register(agentTool())
	r.register(skillTool())

	// Notebook
	r.register(notebookEditTool())

	// Misc
	r.register(sleepTool())
	r.register(toolSearchTool())
	r.register(sendUserMessageTool())
	r.register(askUserQuestionTool())
	r.register(enterPlanModeTool())
	r.register(exitPlanModeTool())
	r.register(taskOutputTool())
	r.register(taskStopTool())
	r.register(clearScreenTool())
	r.register(statusLineTool())

	// Memory
	r.register(writeMemoryTool())

	// Cron
	r.register(cronCreateTool())
	r.register(cronDeleteTool())
	r.register(cronListTool())

	// MCP resources/prompts
	r.register(mcpResourceTool())
	r.register(mcpListResourcesTool())
	r.register(mcpListPromptsTool())
	r.register(mcpGetPromptTool())

	// Worktree
	r.register(worktreeCreateTool())
	r.register(worktreeRemoveTool())

	// Additional tools
	r.register(multiEditTool())
	r.register(todoReadTool())

	// System tools
	r.register(configTool())
	r.register(structuredOutputTool())
	r.register(replTool())
	r.register(powershellTool())

	// Team / Multi-Agent tools
	r.register(sendMessageTool())
	r.register(teamCreateTool())
	r.register(teamDeleteTool())

	// Extended LSP tool (30+ language detection)
	r.register(lspExtendedTool())

	globalRegistry = r
	return r
}

func (r *ToolRegistry) register(spec *ToolSpec) {
	r.specs[spec.Name] = spec
}

// RegisterDynamic registers a dynamically-created tool (e.g., from MCP or plugins).
func (r *ToolRegistry) RegisterDynamic(name, description string, inputSchema map[string]interface{}, handler func(input map[string]interface{}) (string, error)) {
	r.specs[name] = &ToolSpec{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler:     handler,
		Permission:  PermReadOnly,
	}
}

// planModeAllowedTools lists tools permitted during plan mode.
var planModeAllowedTools = map[string]bool{
	"read_file": true, "glob": true, "grep": true, "WebFetch": true, "WebSearch": true,
	"ToolSearch": true, "Skill": true, "Agent": true, "TodoWrite": true,
	"NotebookEdit": true, "SendUserMessage": true, "sleep": true,
	"StructuredOutput": true, "EnterPlanMode": true, "ExitPlanMode": true,
	"AskUserQuestion": true, "LSP": true, "SendMessage": true,
}

// planModeBashWhitelist prefixes allowed for bash in plan mode.
var planModeBashWhitelist = []string{
	"git status", "git diff", "git log", "git show", "git branch",
	"cat ", "head ", "tail ", "ls", "find ", "wc ", "grep ", "which ",
	"echo ", "pwd", "env", "node -e", "python -c",
}

// IsPlanModeActive returns whether plan mode is currently active.
func IsPlanModeActive() bool {
	return globalPlanModeActive
}

// toolAliases maps common LLM tool name variants to registered names.
// Models like Claude use PascalCase (Read, Write, Bash), glm/deepseek may use
// snake_case variants. All map to the actual registered names.
var toolAliases = map[string]string{
	// Claude Code style → actual registered names
	"read":           "read_file",
	"write":          "write_file",
	"edit":           "edit_file",
	"bash":           "bash",
	"glob":           "glob",
	"grep":           "grep",
	// Common LLM variants → registered names
	"create_file":    "write_file",
	"run_command":    "bash",
	"execute":        "bash",
	"shell":          "bash",
	"terminal":       "bash",
	"search":         "grep",
	"search_files":   "grep",
	"find_files":     "glob",
	"list_files":     "glob",
	"find":           "glob",
	"write_memory":   "WriteMemory",
	"ask_user":       "AskUserQuestion",
	"notebook_edit":  "NotebookEdit",
	"task_output":    "TaskOutput",
	"task_stop":      "TaskStop",
	"tool_search":    "ToolSearch",
	"web_fetch":      "WebFetch",
	"web_search":     "WebSearch",
	"web_reader":     "WebFetch",
	"todo_write":     "TodoWrite",
	"todo_read":      "TodoRead",
	"plan_mode":      "EnterPlanMode",
	"exit_plan":      "ExitPlanMode",
	"send_message":   "SendUserMessage",
	// Team/Multi-Agent aliases
	"team_create":   "TeamCreate",
	"team_delete":   "TeamDelete",
	"sendmsg":       "SendMessage",
	"lsp":           "LSP",
}

// Execute runs a tool by name.
func (r *ToolRegistry) Execute(toolName string, input map[string]interface{}) (string, error) {
	spec, ok := r.specs[toolName]
	if !ok {
		// Alias mapping for non-standard tool names (e.g. glm calls read_file instead of Read)
		if alias, found := toolAliases[strings.ToLower(toolName)]; found {
			spec, ok = r.specs[alias]
			if ok {
				toolName = alias
			}
		}
	}
	if !ok {
		// Case-insensitive fallback for non-standard models (e.g. glm, deepseek)
		lower := strings.ToLower(toolName)
		for name, s := range r.specs {
			if strings.ToLower(name) == lower {
				spec = s
				ok = true
				toolName = name
				break
			}
		}
	}
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}

	// Plan mode restrictions
	if globalPlanModeActive {
		if toolName == "bash" {
			cmd, _ := input["command"].(string)
			allowed := false
			for _, prefix := range planModeBashWhitelist {
				if strings.HasPrefix(cmd, prefix) || cmd == strings.TrimSpace(prefix) {
					allowed = true
					break
				}
			}
			if !allowed {
				return "", fmt.Errorf("only read-only bash commands allowed in plan mode")
			}
		} else if !planModeAllowedTools[toolName] {
			return "", fmt.Errorf("tool %s is not available in plan mode", toolName)
		}
	}

	// Sandbox path validation for file tools
	if globalSandbox != nil && globalSandbox.IsEnabled() {
		if path := extractPath(input); path != "" {
			if err := globalSandbox.ValidatePath(path); err != nil {
				return "", err
			}
		}
	}

	return spec.Handler(input)
}

// extractPath extracts a file path from tool input.
func extractPath(input map[string]interface{}) string {
	for _, key := range []string{"path", "file_path", "directory"} {
		if s, ok := input[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

// AvailableTools returns tool definitions for the API.
func (r *ToolRegistry) AvailableTools() []api.ToolDefinition {
	defs := make([]api.ToolDefinition, 0, len(r.specs))
	for _, spec := range r.specs {
		defs = append(defs, api.ToolDefinition{
			Name:        spec.Name,
			Description: spec.Description,
			InputSchema: spec.InputSchema,
		})
	}
	return defs
}

// ToolFilter controls which tools are available based on agent type.
type ToolFilter struct {
	allowed map[string]bool
}

// Agent tool filters matching the Rust version's 6 agent types.

// ReadOnlyFilter returns a filter for Explore-type agents (read-only tools only).
func ReadOnlyFilter() *ToolFilter {
	names := []string{
		"read_file", "glob", "grep", "WebFetch", "WebSearch",
		"ToolSearch", "Skill", "NotebookEdit",
		"SendUserMessage", "sleep", "StructuredOutput",
	}
	f := &ToolFilter{allowed: make(map[string]bool)}
	for _, n := range names {
		f.allowed[n] = true
	}
	return f
}

// ReadOnlyWithAgentFilter returns a filter for Plan-type agents.
func ReadOnlyWithAgentFilter() *ToolFilter {
	f := ReadOnlyFilter()
	f.allowed["Agent"] = true
	f.allowed["TodoWrite"] = true
	return f
}

// VerificationFilter returns a filter for Verification agents: bash + read-only, no write/edit.
func VerificationFilter() *ToolFilter {
	f := ReadOnlyFilter()
	f.allowed["bash"] = true
	f.allowed["PowerShell"] = true
	f.allowed["TodoWrite"] = true
	return f
}

// ClawGuideFilter returns a filter for claw-guide agents: read-only + SendUserMessage.
func ClawGuideFilter() *ToolFilter {
	f := ReadOnlyFilter()
	f.allowed["SendUserMessage"] = true
	return f
}

// StatuslineSetupFilter returns a filter for statusline-setup agents: bash + read + write + edit.
func StatuslineSetupFilter() *ToolFilter {
	names := []string{
		"bash", "read_file", "write_file", "edit_file",
		"glob", "grep", "ToolSearch",
	}
	f := &ToolFilter{allowed: make(map[string]bool)}
	for _, n := range names {
		f.allowed[n] = true
	}
	return f
}

// AllToolsFilter returns a filter that allows everything (nil = no filtering).
func AllToolsFilter() *ToolFilter {
	return nil
}

// FilterForAgentType returns the appropriate tool filter for the given agent type.
func FilterForAgentType(agentType string) *ToolFilter {
	switch agentType {
	case "Explore":
		return ReadOnlyFilter()
	case "Plan":
		return ReadOnlyWithAgentFilter()
	case "Verification":
		return VerificationFilter()
	case "claw-guide":
		return ClawGuideFilter()
	case "statusline-setup":
		return StatuslineSetupFilter()
	default: // "general-purpose" and unknown
		return AllToolsFilter()
	}
}

// MaxIterationsForAgent returns the default max iterations for an agent type.
func MaxIterationsForAgent(agentType string) int {
	switch agentType {
	case "Explore":
		return 5
	case "Plan":
		return 3
	case "Verification":
		return 10
	case "claw-guide":
		return 8
	case "statusline-setup":
		return 10
	default:
		return 32
	}
}

// FilterTools returns tool definitions filtered by the given filter.
func (r *ToolRegistry) FilterTools(filter *ToolFilter) []api.ToolDefinition {
	if filter == nil {
		return r.AvailableTools()
	}
	defs := make([]api.ToolDefinition, 0, len(r.specs))
	for _, spec := range r.specs {
		if filter.allowed[spec.Name] {
			defs = append(defs, api.ToolDefinition{
				Name:        spec.Name,
				Description: spec.Description,
				InputSchema: spec.InputSchema,
			})
		}
	}
	return defs
}

// FilteredRegistry returns a new registry containing only filtered tools.
func (r *ToolRegistry) FilteredRegistry(filter *ToolFilter) *ToolRegistry {
	if filter == nil {
		return r
	}
	newR := &ToolRegistry{specs: make(map[string]*ToolSpec)}
	for name, spec := range r.specs {
		if filter.allowed[name] {
			newR.specs[name] = spec
		}
	}
	return newR
}

// GetSpec returns the tool spec for a given name.
func (r *ToolRegistry) GetSpec(name string) (*ToolSpec, bool) {
	s, ok := r.specs[name]
	return s, ok
}

// bashTool creates the bash tool spec.
func bashTool() *ToolSpec {
	return &ToolSpec{
		Name:       "bash",
		Permission: PermDangerFullAccess,
		Description: "Execute a bash command and return the output.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command":     map[string]interface{}{"type": "string", "description": "The bash command to execute"},
				"timeout":     map[string]interface{}{"type": "integer", "description": "Timeout in milliseconds (default 120000)"},
				"description": map[string]interface{}{"type": "string", "description": "What this command does"},
				"run_in_background": map[string]interface{}{"type": "boolean", "description": "Run in background"},
				"dangerouslyDisableSandbox": map[string]interface{}{"type": "boolean", "description": "Disable sandbox for this command"},
			},
			"required": []string{"command"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			cmd, _ := input["command"].(string)
			timeoutMs := 120000
			if t, ok := input["timeout"].(float64); ok && t > 0 {
				timeoutMs = int(t)
			}
			runBg := false
			if b, ok := input["run_in_background"].(bool); ok {
				runBg = b
			}
			sandboxOff := false
			if b, ok := input["dangerouslyDisableSandbox"].(bool); ok {
				sandboxOff = b
			}

			// Sandbox command validation
			if globalSandbox != nil && globalSandbox.IsEnabled() && !sandboxOff {
				if err := globalSandbox.ValidateCommand(cmd); err != nil {
					return "", err
				}
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
			defer cancel()

			var execCmd *exec.Cmd
				if runtime.GOOS == "windows" {
					execCmd = exec.CommandContext(ctx, "cmd.exe", "/c", cmd)
				} else {
					execCmd = exec.CommandContext(ctx, "bash", "-c", cmd)
				}
				execCmd.Stdin = strings.NewReader("")
			if runBg {
				execCmd.Start()
				return fmt.Sprintf("started in background (pid %d)", execCmd.Process.Pid), nil
			}
			out, err := execCmd.CombinedOutput()
			if ctx.Err() == context.DeadlineExceeded {
				return string(out) + "\n[timeout]", nil
			}
			return string(out), err
		},
	}
}

func readTool() *ToolSpec {
	return &ToolSpec{
		Name:       "read_file",
		Permission: PermReadOnly,
		Description: "Read a file from the filesystem.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":   map[string]interface{}{"type": "string", "description": "Absolute file path"},
				"offset": map[string]interface{}{"type": "integer", "description": "Start line (0-indexed)"},
				"limit":  map[string]interface{}{"type": "integer", "description": "Number of lines to read"},
			},
			"required": []string{"path"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			path, _ := input["path"].(string)
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				entries, err := os.ReadDir(path)
				if err != nil {
					return "", fmt.Errorf("failed to list directory %s: %w", path, err)
				}
				var lines []string
				for _, e := range entries {
					prefix := " "
					if e.IsDir() { prefix = "d" }
					lines = append(lines, prefix + " " + e.Name())
				}
				return strings.Join(lines, "\n"), nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("failed to read %s: %w", path, err)
			}

			// Image detection
			ext := strings.ToLower(filepath.Ext(path))
			imageExts := map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".bmp": true, ".svg": true}
			if imageExts[ext] {
				mime := "image/png"
				switch ext {
				case ".jpg", ".jpeg":
					mime = "image/jpeg"
				case ".gif":
					mime = "image/gif"
				case ".webp":
					mime = "image/webp"
				case ".bmp":
					mime = "image/bmp"
				case ".svg":
					mime = "image/svg+xml"
				}
				b64 := base64.StdEncoding.EncodeToString(data)
				return fmt.Sprintf("[Image: %s (%d bytes, %s)]\ndata:%s;base64,%s", filepath.Base(path), len(data), mime, mime, b64), nil
			}

			// PDF detection
			if ext == ".pdf" {
				b64 := base64.StdEncoding.EncodeToString(data)
				return fmt.Sprintf("[PDF: %s (%d bytes)]\ndata:application/pdf;base64,%s", filepath.Base(path), len(data), b64), nil
			}

			lines := strings.Split(string(data), "\n")
			offset := 0
			if o, ok := input["offset"].(float64); ok && o > 0 {
				offset = int(o)
			}
			limit := len(lines)
			if l, ok := input["limit"].(float64); ok && l > 0 {
				limit = int(l)
			}
			end := offset + limit
			if end > len(lines) {
				end = len(lines)
			}
			var buf strings.Builder
			for i := offset; i < end; i++ {
				fmt.Fprintf(&buf, "%d	%s\n", i+1, lines[i])
			}
			return buf.String(), nil
		},
	}
}

func writeTool() *ToolSpec {
	return &ToolSpec{
		Name:       "write_file",
		Permission: PermWorkspaceWrite,
		Description: "Write content to a file, creating it if needed.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "Absolute file path"},
				"content": map[string]interface{}{"type": "string", "description": "File content to write"},
			},
			"required": []string{"path", "content"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			path, _ := input["path"].(string)
			content, _ := input["content"].(string)
			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return "", err
			}
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return "", err
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(content), path), nil
		},
	}
}

func editTool() *ToolSpec {
	return &ToolSpec{
		Name:       "edit_file",
		Permission: PermWorkspaceWrite,
		Description: "Replace a string in a file.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":        map[string]interface{}{"type": "string"},
				"old_string":  map[string]interface{}{"type": "string"},
				"new_string":  map[string]interface{}{"type": "string"},
				"replace_all": map[string]interface{}{"type": "boolean"},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			path, _ := input["path"].(string)
			oldStr, _ := input["old_string"].(string)
			newStr, _ := input["new_string"].(string)
			replaceAll := false
			if r, ok := input["replace_all"].(bool); ok {
				replaceAll = r
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			content := string(data)

			count := strings.Count(content, oldStr)
			if count == 0 {
				return "", fmt.Errorf("old_string not found in %s", path)
			}
			if count > 1 && !replaceAll {
				return "", fmt.Errorf("old_string found %d times in %s; use replace_all", count, path)
			}

			if replaceAll {
				content = strings.ReplaceAll(content, oldStr, newStr)
			} else {
				content = strings.Replace(content, oldStr, newStr, 1)
			}

			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				return "", err
			}
			return fmt.Sprintf("replaced %d occurrence(s) in %s", count, path), nil
		},
	}
}

func globTool() *ToolSpec {
	return &ToolSpec{
		Name:       "glob",
		Permission: PermReadOnly,
		Description: "Find files matching a glob pattern.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{"type": "string", "description": "Glob pattern"},
				"path":    map[string]interface{}{"type": "string", "description": "Directory to search"},
			},
			"required": []string{"pattern"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			pattern, _ := input["pattern"].(string)
			searchDir, _ := input["path"].(string)
			if searchDir == "" {
				searchDir = "."
			}
			matches, err := filepath.Glob(filepath.Join(searchDir, pattern))
			if err != nil {
				return "", err
			}
			return strings.Join(matches, "\n"), nil
		},
	}
}

func grepTool() *ToolSpec {
	return &ToolSpec{
		Name:       "grep",
		Permission: PermReadOnly,
		Description: "Search file contents with a regex pattern.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern":    map[string]interface{}{"type": "string", "description": "Regex pattern"},
				"path":       map[string]interface{}{"type": "string", "description": "Directory to search"},
				"glob":       map[string]interface{}{"type": "string", "description": "File glob filter"},
				"output_mode": map[string]interface{}{"type": "string", "enum": []string{"content", "files_with_matches", "count"}},
				"-i":         map[string]interface{}{"type": "boolean", "description": "Case insensitive"},
				"head_limit":  map[string]interface{}{"type": "integer", "description": "Max results"},
				"-B":         map[string]interface{}{"type": "integer", "description": "Lines before match"},
				"-A":         map[string]interface{}{"type": "integer", "description": "Lines after match"},
				"-C":         map[string]interface{}{"type": "integer", "description": "Context lines around match"},
				"-n":         map[string]interface{}{"type": "boolean", "description": "Show line numbers"},
				"type":       map[string]interface{}{"type": "string", "description": "File type filter"},
				"multiline":  map[string]interface{}{"type": "boolean", "description": "Enable multiline matching"},
			},
			"required": []string{"pattern"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			pattern, _ := input["pattern"].(string)
			searchDir, _ := input["path"].(string)
			caseInsensitive := false
			if v, ok := input["-i"].(bool); ok {
				caseInsensitive = v
			}
			headLimit := 250
			if v, ok := input["head_limit"].(float64); ok && v > 0 {
				headLimit = int(v)
			}
			if searchDir == "" {
				searchDir = "."
			}

			flags := ""
			if caseInsensitive {
				flags = "(?i)"
			}
			re, err := regexp.Compile(flags + pattern)
			if err != nil {
				return "", fmt.Errorf("invalid regex: %w", err)
			}

			globPattern, _ := input["glob"].(string)
			outputMode, _ := input["output_mode"].(string)
			if outputMode == "" {
				outputMode = "files_with_matches"
			}

			var matches []string
			err = filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if globPattern != "" {
					matched, _ := filepath.Match(globPattern, d.Name())
					if !matched {
						return nil
					}
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				for i, line := range strings.Split(string(data), "\n") {
					if re.MatchString(line) {
						if outputMode == "content" {
							matches = append(matches, fmt.Sprintf("%s:%d:%s", path, i+1, line))
						} else if outputMode == "files_with_matches" {
							matches = append(matches, path)
							return nil
						} else if outputMode == "count" {
							matches = append(matches, path)
							return nil
						}
						if len(matches) >= headLimit {
							return nil
						}
					}
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			return strings.Join(matches, "\n"), nil
		},
	}
}
