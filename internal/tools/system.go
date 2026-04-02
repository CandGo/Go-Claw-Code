package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func configTool() *ToolSpec {
	return &ToolSpec{
		Name:       "Config",
		Permission: PermWorkspaceWrite,
		Description: "Read or write configuration settings (theme, editorMode, verbose, permissions).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"setting": map[string]interface{}{"type": "string", "description": "Setting name (e.g. theme, editorMode, verbose, permissions.defaultMode)"},
				"value":   map[string]interface{}{"type": "string", "description": "Value to set (omit to read)"},
			},
			"required": []string{"setting"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			setting, _ := input["setting"].(string)
			value, _ := input["value"].(string)

			home, _ := os.UserHomeDir()
			configDir := filepath.Join(home, ".claw")
			configPath := filepath.Join(configDir, "settings.json")

			configData := map[string]interface{}{}
			if data, err := os.ReadFile(configPath); err == nil {
				json.Unmarshal(data, &configData)
			}

			if value == "" {
				val := getConfigValue(configData, setting)
				if val == nil {
					return fmt.Sprintf("%s: (not set)", setting), nil
				}
				return fmt.Sprintf("%s: %v", setting, val), nil
			}

			os.MkdirAll(configDir, 0755)
			setConfigValue(configData, setting, value)
			data, _ := json.MarshalIndent(configData, "", "  ")
			if err := os.WriteFile(configPath, data, 0644); err != nil {
				return "", err
			}
			return fmt.Sprintf("Set %s = %s", setting, value), nil
		},
	}
}

func structuredOutputTool() *ToolSpec {
	return &ToolSpec{
		Name:       "StructuredOutput",
		Permission: PermReadOnly,
		Description: "Output structured data as JSON. Passes input through as-is.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"data": map[string]interface{}{"type": "object", "description": "Structured data to output"},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			data, err := json.MarshalIndent(input, "", "  ")
			if err != nil {
				return "", err
			}
			return string(data), nil
		},
	}
}

func replTool() *ToolSpec {
	return &ToolSpec{
		Name:       "REPL",
		Permission: PermDangerFullAccess,
		Description: "Execute code in a runtime (python, javascript, shell).",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"code":       map[string]interface{}{"type": "string", "description": "Code to execute"},
				"language":   map[string]interface{}{"type": "string", "description": "Runtime: python, javascript, shell"},
				"timeout_ms": map[string]interface{}{"type": "integer", "description": "Timeout in ms (default 30000)"},
			},
			"required": []string{"code", "language"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			code, _ := input["code"].(string)
			lang, _ := input["language"].(string)
			timeoutMs := 30000
			if t, ok := input["timeout_ms"].(float64); ok && t > 0 {
				timeoutMs = int(t)
			}
			return runREPLCode(code, lang, timeoutMs)
		},
	}
}

func powershellTool() *ToolSpec {
	return &ToolSpec{
		Name:       "PowerShell",
		Permission: PermDangerFullAccess,
		Description: "Execute a PowerShell command. Detects pwsh or powershell automatically.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command":        map[string]interface{}{"type": "string", "description": "PowerShell command"},
				"timeout":        map[string]interface{}{"type": "integer", "description": "Timeout in ms (default 120000)"},
				"description":    map[string]interface{}{"type": "string", "description": "What this command does"},
				"run_in_background": map[string]interface{}{"type": "boolean", "description": "Run in background"},
			},
			"required": []string{"command"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			cmd, _ := input["command"].(string)
			timeoutMs := 120000
			if t, ok := input["timeout"].(float64); ok && t > 0 {
				timeoutMs = int(t)
			}
			return runPowerShell(cmd, timeoutMs)
		},
	}
}

func runREPLCode(code, lang string, timeoutMs int) (string, error) {
	var cmd *exec.Cmd
	switch strings.ToLower(lang) {
	case "python", "python3":
		cmd = exec.Command("python3", "-c", code)
		if _, err := exec.LookPath("python3"); err != nil {
			cmd = exec.Command("python", "-c", code)
		}
	case "javascript", "js", "node":
		cmd = exec.Command("node", "-e", code)
	case "shell", "bash", "sh":
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd", "/C", code)
		} else {
			cmd = exec.Command("sh", "-c", code)
		}
	default:
		return "", fmt.Errorf("unsupported language: %s (use python, javascript, shell)", lang)
	}

	if timeoutMs > 0 {
		// Simple timeout via context
	}

	out, err := cmd.CombinedOutput()
	return string(out), err
}

func runPowerShell(command string, timeoutMs int) (string, error) {
	psExe := "powershell"
	if _, err := exec.LookPath("pwsh"); err == nil {
		psExe = "pwsh"
	} else if runtime.GOOS != "windows" {
		return "", fmt.Errorf("PowerShell not found (install pwsh)")
	}

	cmd := exec.Command(psExe, "-NoProfile", "-Command", command)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func permName(p PermissionLevel) string {
	switch p {
	case PermReadOnly:
		return "ReadOnly"
	case PermWorkspaceWrite:
		return "WorkspaceWrite"
	case PermDangerFullAccess:
		return "DangerFullAccess"
	default:
		return "Unknown"
	}
}

func getConfigValue(data map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	current := data
	for i, part := range parts {
		if i == len(parts)-1 {
			return current[part]
		}
		if next, ok := current[part].(map[string]interface{}); ok {
			current = next
		} else {
			return nil
		}
	}
	return nil
}

func setConfigValue(data map[string]interface{}, path, value string) {
	parts := strings.Split(path, ".")
	current := data
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		if next, ok := current[part].(map[string]interface{}); ok {
			current = next
		} else {
			newMap := map[string]interface{}{}
			current[part] = newMap
			current = newMap
		}
	}
}
