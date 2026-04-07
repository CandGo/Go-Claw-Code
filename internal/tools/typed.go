package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Typed input/output structs for all tools, matching the Rust implementation.

// --- Bash ---

// BashInput is the typed input for the bash tool.
type BashInput struct {
	Command              string  `json:"command"`
	Timeout              int     `json:"timeout,omitempty"`
	Description          string  `json:"description,omitempty"`
	RunInBackground      bool    `json:"run_in_background,omitempty"`
	DangerouslyDisableSandbox bool `json:"dangerouslyDisableSandbox,omitempty"`
}

// BashOutput is the typed output for the bash tool.
type BashOutput struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exit_code"`
	BackgroundTaskID string `json:"background_task_id,omitempty"`
	Command   string `json:"command"`
	IsError   bool   `json:"is_error,omitempty"`
}

// --- ReadFile ---
// Types moved to fileops.go (ReadFileOutput, TextFilePayload)

// ReadFileInput is the typed input for the read_file tool.
type ReadFileInput struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// --- WriteFile ---
// Types moved to fileops.go (WriteFileOutput)

// WriteFileInput is the typed input for the write_file tool.
type WriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// --- EditFile ---
// Types moved to fileops.go (EditFileOutput)

// EditFileInput is the typed input for the edit_file tool.
type EditFileInput struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

// --- Grep ---

// GrepInput is the typed input for the grep tool.
type GrepInput struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path,omitempty"`
	Glob        string `json:"glob,omitempty"`
	OutputMode  string `json:"output_mode,omitempty"` // files_with_matches, content, count
	ContextBefore int  `json:"context_before,omitempty"` // -B
	ContextAfter  int  `json:"context_after,omitempty"`  // -A
	HeadLimit   int    `json:"head_limit,omitempty"`
	CaseInsensitive bool `json:"case_insensitive,omitempty"`
}

// GrepOutput is the typed output for the grep tool.
type GrepOutput struct {
	Matches     []GrepMatch `json:"matches,omitempty"`
	Files       []string    `json:"files,omitempty"`
	TotalFiles  int         `json:"total_files"`
	TotalMatches int        `json:"total_matches"`
	CountOutput map[string]int `json:"count_output,omitempty"`
}

// GrepMatch represents a single grep match.
type GrepMatch struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// --- Glob ---

// GlobInput is the typed input for the glob tool.
type GlobInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

// GlobOutput is the typed output for the glob tool.
type GlobOutput struct {
	Files []string `json:"files"`
	Count int      `json:"count"`
}

// --- Agent ---

// AgentInput is the typed input for the agent tool.
type AgentInput struct {
	Description  string `json:"description"`
	Prompt       string `json:"prompt"`
	SubagentType string `json:"subagent_type,omitempty"`
	Name         string `json:"name,omitempty"`
	Model        string `json:"model,omitempty"`
	Isolation    string `json:"isolation,omitempty"`
}

// AgentOutput is the typed output for the agent tool.
type AgentOutput struct {
	AgentID     string `json:"agentId"`
	Output      string `json:"output"`
	OutputFile  string `json:"outputFile,omitempty"`
	Error       string `json:"error,omitempty"`
	Model       string `json:"model,omitempty"`
	Iterations  int    `json:"iterations,omitempty"`
}

// --- TodoWrite ---
// Uses TodoItem from todo.go

// --- Config ---

// ConfigInput is the typed input for the config tool.
type ConfigInput struct {
	Setting string      `json:"setting"`
	Value   ConfigValue `json:"value,omitempty"`
}

// ConfigValue is a polymorphic value that can be string, bool, or number.
type ConfigValue struct {
	StringVal *string
	BoolVal   *bool
	NumberVal *float64
}

// ConfigSettingSpec describes a valid config setting.
type ConfigSettingSpec struct {
	Scope   string        // "global" or "settings"
	Kind    string        // "boolean" or "string"
	Options []string      // allowed values (if constrained)
	Path    []string      // JSON path segments
}

// supportedConfigSettings is the whitelist of valid config settings.
var supportedConfigSettings = map[string]ConfigSettingSpec{
	"theme": {
		Scope: "global", Kind: "string",
		Path: []string{"theme"},
	},
	"editorMode": {
		Scope: "global", Kind: "string",
		Path: []string{"editorMode"},
		Options: []string{"default", "vim", "emacs"},
	},
	"verbose": {
		Scope: "global", Kind: "boolean",
		Path: []string{"verbose"},
	},
	"preferredNotifChannel": {
		Scope: "global", Kind: "string",
		Path: []string{"preferredNotifChannel"},
	},
	"autoCompactEnabled": {
		Scope: "global", Kind: "boolean",
		Path: []string{"autoCompactEnabled"},
	},
	"autoMemoryEnabled": {
		Scope: "settings", Kind: "boolean",
		Path: []string{"autoMemoryEnabled"},
	},
	"autoDreamEnabled": {
		Scope: "settings", Kind: "boolean",
		Path: []string{"autoDreamEnabled"},
	},
	"fileCheckpointingEnabled": {
		Scope: "global", Kind: "boolean",
		Path: []string{"fileCheckpointingEnabled"},
	},
	"showTurnDuration": {
		Scope: "global", Kind: "boolean",
		Path: []string{"showTurnDuration"},
	},
	"terminalProgressBarEnabled": {
		Scope: "global", Kind: "boolean",
		Path: []string{"terminalProgressBarEnabled"},
	},
	"todoFeatureEnabled": {
		Scope: "global", Kind: "boolean",
		Path: []string{"todoFeatureEnabled"},
	},
	"model": {
		Scope: "settings", Kind: "string",
		Path: []string{"model"},
	},
	"alwaysThinkingEnabled": {
		Scope: "settings", Kind: "boolean",
		Path: []string{"alwaysThinkingEnabled"},
	},
	"permissions.defaultMode": {
		Scope: "settings", Kind: "string",
		Path: []string{"permissions", "defaultMode"},
		Options: []string{"default", "plan", "acceptEdits", "dontAsk", "auto"},
	},
	"language": {
		Scope: "settings", Kind: "string",
		Path: []string{"language"},
	},
	"teammateMode": {
		Scope: "global", Kind: "string",
		Path: []string{"teammateMode"},
		Options: []string{"tmux", "in-process", "auto"},
	},
}

// --- ToolSearch ---

// ToolSearchInput is the typed input for the tool_search tool.
type ToolSearchInput struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
}

// ToolSearchOutput is the typed output for the tool_search tool.
type ToolSearchOutput struct {
	Results []ToolSearchResult `json:"results"`
	Total   int                `json:"total"`
	Query   string             `json:"query"`
}

// ToolSearchResult represents a scored tool search result.
type ToolSearchResult struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Score       int    `json:"score"`
}

// --- NotebookEdit ---

// NotebookEditInput is the typed input for the notebook_edit tool.
type NotebookEditInput struct {
	NotebookPath string `json:"notebook_path"`
	NewSource    string `json:"new_source"`
	CellID       string `json:"cell_id,omitempty"`
	CellType     string `json:"cell_type,omitempty"` // code, markdown
	EditMode     string `json:"edit_mode,omitempty"` // replace, insert, delete
}

// --- Helpers ---

// parseTypedInput extracts typed input from a raw map.
func parseTypedInput(input map[string]interface{}, dest interface{}) error {
	data, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("failed to marshal input: %w", err)
	}
	return json.Unmarshal(data, dest)
}

// formatTypedOutput returns pretty-printed JSON for a typed output struct.
func formatTypedOutput(v interface{}) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// validateConfigSetting checks if a setting name is in the whitelist.
func validateConfigSetting(setting string) (*ConfigSettingSpec, error) {
	spec, ok := supportedConfigSettings[setting]
	if !ok {
		// Build list of valid settings for the error message
		validSettings := make([]string, 0, len(supportedConfigSettings))
		for k := range supportedConfigSettings {
			validSettings = append(validSettings, k)
		}
		return nil, fmt.Errorf("unknown setting %q. Valid settings: %s", setting, strings.Join(validSettings, ", "))
	}
	return &spec, nil
}

// normalizeConfigValue normalizes a config value based on the setting's kind.
// Converts string booleans to actual booleans, validates constrained values.
func normalizeConfigValue(spec *ConfigSettingSpec, value string) (interface{}, error) {
	switch spec.Kind {
	case "boolean":
		switch strings.ToLower(value) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return nil, fmt.Errorf("invalid boolean value %q (use true or false)", value)
		}
	case "string":
		if len(spec.Options) > 0 {
			valid := false
			for _, opt := range spec.Options {
				if value == opt {
					valid = true
					break
				}
			}
			if !valid {
				return nil, fmt.Errorf("invalid value %q. Options: %s", value, strings.Join(spec.Options, ", "))
			}
		}
		return value, nil
	default:
		return value, nil
	}
}

// configFilePath returns the config file path for a given scope.
func configFilePath(scope string) string {
	home, _ := os.UserHomeDir()
	switch scope {
	case "global":
		return filepath.Join(home, ".go-claw", "settings.json")
	case "settings":
		return filepath.Join(".claw", "settings.local.json")
	default:
		return filepath.Join(home, ".go-claw", "settings.json")
	}
}
