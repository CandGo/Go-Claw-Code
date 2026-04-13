package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// globalMemoryDir is the project .claude/memory/ directory, set during startup.
var globalMemoryDir string

// globalUserMemoryDir is the user ~/.claude/memory/ directory, set during startup.
var globalUserMemoryDir string

// SetMemoryDirs sets the global memory directories for the WriteMemory tool.
func SetMemoryDirs(projectDir, userDir string) {
	globalMemoryDir = projectDir
	globalUserMemoryDir = userDir
}

// writeMemoryTool creates the WriteMemory tool spec.
// This tool creates memory files with frontmatter and updates the MEMORY.md index.
func writeMemoryTool() *ToolSpec {
	return &ToolSpec{
		Name:       "WriteMemory",
		Permission: PermWorkspaceWrite,
		Description: "Create or update a memory file in the persistent memory system. Memories are loaded into future conversation contexts automatically. Types: user (role/preferences), feedback (what to avoid/keep doing), project (ongoing work context), reference (external resource pointers). Do NOT save: code patterns derivable from files, git history, debugging solutions, anything in CLAUDE.md. Save only when the user explicitly asks to remember, or when you learn surprising preferences. Always verify memory is still current before acting on it.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Short unique name for this memory (used as filename)",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The content to remember",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"user", "feedback", "project", "reference"},
					"description": "Memory type: user (personal preference), feedback (user correction), project (project-specific), reference (general knowledge)",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "One-line description for the memory index",
				},
			},
			"required": []string{"name", "content", "type", "description"},
		},
		Handler: handleWriteMemory,
	}
}

func handleWriteMemory(input map[string]interface{}) (string, error) {
	name, _ := input["name"].(string)
	content, _ := input["content"].(string)
	memType, _ := input["type"].(string)
	description, _ := input["description"].(string)

	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	if content == "" {
		return "", fmt.Errorf("content is required")
	}

	// Validate type.
	validTypes := map[string]bool{"user": true, "feedback": true, "project": true, "reference": true}
	if !validTypes[memType] {
		return "", fmt.Errorf("invalid type %q; must be one of: user, feedback, project, reference", memType)
	}

	// Determine target directory based on type.
	dir := globalMemoryDir
	switch memType {
	case "user", "feedback":
		dir = globalUserMemoryDir
	default:
		dir = globalMemoryDir
	}

	if dir == "" {
		// Fallback: create .claaw/memory in cwd.
		cwd, _ := os.Getwd()
		dir = filepath.Join(cwd, ".claaw", "memory")
	}

	// Ensure directory exists.
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create memory directory: %w", err)
	}

	// Write memory file with frontmatter.
	filename := sanitizeName(name) + ".md"
	filePath := filepath.Join(dir, filename)

	var buf strings.Builder
	buf.WriteString("---\n")
	buf.WriteString(fmt.Sprintf("name: %s\n", name))
	buf.WriteString(fmt.Sprintf("description: %s\n", description))
	buf.WriteString(fmt.Sprintf("type: %s\n", memType))
	buf.WriteString("---\n")
	buf.WriteString(content)
	buf.WriteString("\n")

	if err := os.WriteFile(filePath, []byte(buf.String()), 0644); err != nil {
		return "", fmt.Errorf("failed to write memory file: %w", err)
	}

	// Update MEMORY.md index.
	updateMemoryIndex(dir)

	return fmt.Sprintf("Memory '%s' (%s) saved to %s", name, memType, filePath), nil
}

// sanitizeName converts a name to a safe filename.
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ToLower(name)
	var safe strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			safe.WriteRune(r)
		}
	}
	result := safe.String()
	if result == "" {
		result = "memory"
	}
	return result
}

// updateMemoryIndex regenerates the MEMORY.md file in a memory directory.
func updateMemoryIndex(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type memInfo struct {
		name        string
		memType     string
		description string
	}

	var memories []memInfo

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if entry.Name() == "MEMORY.md" {
			continue
		}

		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}

		info := parseFrontmatter(string(data))
		stem := strings.TrimSuffix(entry.Name(), ".md")
		if info.name == "" {
			info.name = stem
		}
		memories = append(memories, memInfo{
			name:        info.name,
			memType:     info.memType,
			description: info.description,
		})
	}

	var buf strings.Builder
	buf.WriteString("# Memory Index\n\n")
	if len(memories) == 0 {
		buf.WriteString("No memories stored.\n")
	} else {
		for _, m := range memories {
			buf.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", m.name, m.memType, m.description))
		}
	}

	indexPath := filepath.Join(dir, "MEMORY.md")
	os.WriteFile(indexPath, []byte(buf.String()), 0644)
}

type frontmatterInfo struct {
	name        string
	description string
	memType     string
}

func parseFrontmatter(content string) frontmatterInfo {
	info := frontmatterInfo{}

	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return info
	}

	rest := content[4:]
	endIdx := strings.Index(rest, "\n---")
	if endIdx < 0 {
		return info
	}

	frontmatter := rest[:endIdx]
	for _, line := range strings.Split(frontmatter, "\n") {
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		value := strings.TrimSpace(line[colonIdx+1:])
		switch key {
		case "name":
			info.name = value
		case "description":
			info.description = value
		case "type":
			info.memType = value
		}
	}

	return info
}
