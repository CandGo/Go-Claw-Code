package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-claw/claw/internal/api"
)

// ToolSpec describes a single tool.
type ToolSpec struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	Handler     func(input map[string]interface{}) (string, error)
	Aliases     []string
}

// ToolRegistry manages all available tools.
type ToolRegistry struct {
	specs map[string]*ToolSpec
}

// globalRegistry is the singleton used by ToolSearch.
var globalRegistry *ToolRegistry

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
	}
}

// Execute runs a tool by name.
func (r *ToolRegistry) Execute(toolName string, input map[string]interface{}) (string, error) {
	spec, ok := r.specs[toolName]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", toolName)
	}
	return spec.Handler(input)
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

func bashTool() *ToolSpec {
	return &ToolSpec{
		Name:        "bash",
		Description: "Execute a bash command and return the output.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{"type": "string", "description": "The bash command to execute"},
				"timeout": map[string]interface{}{"type": "integer", "description": "Timeout in milliseconds (default 120000)"},
			},
			"required": []string{"command"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			cmd, _ := input["command"].(string)
			timeoutMs := 120000
			if t, ok := input["timeout"].(float64); ok && t > 0 {
				timeoutMs = int(t)
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
			defer cancel()

			execCmd := exec.CommandContext(ctx, "bash", "-c", cmd)
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
		Name:        "read_file",
		Description: "Read a file from the filesystem.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "Absolute file path"},
				"offset":  map[string]interface{}{"type": "integer", "description": "Start line (0-indexed)"},
				"limit":   map[string]interface{}{"type": "integer", "description": "Number of lines to read"},
			},
			"required": []string{"path"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			path, _ := input["path"].(string)
			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("failed to read %s: %w", path, err)
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
				fmt.Fprintf(&buf, "%d\t%s\n", i+1, lines[i])
			}
			return buf.String(), nil
		},
	}
}

func writeTool() *ToolSpec {
	return &ToolSpec{
		Name:        "write_file",
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
		Name:        "edit_file",
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
		Name:        "glob",
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
		Name:        "grep",
		Description: "Search file contents with a regex pattern.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{"type": "string", "description": "Regex pattern"},
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

			re, err := regexp.Compile(pattern)
			if err != nil {
				return "", fmt.Errorf("invalid regex: %w", err)
			}

			var matches []string
			err = filepath.WalkDir(searchDir, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return nil
				}
				for i, line := range strings.Split(string(data), "\n") {
					if re.MatchString(line) {
						matches = append(matches, fmt.Sprintf("%s:%d:%s", path, i+1, line))
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
