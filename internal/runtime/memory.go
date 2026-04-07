package runtime

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MemoryEntry represents a persisted memory record.
// Each entry corresponds to a .md file in the memory directory.
type MemoryEntry struct {
	Name        string `yaml:"name"`        // from frontmatter (also the filename stem)
	Description string `yaml:"description"` // one-line hook for index
	Type        string `yaml:"type"`        // user, feedback, project, reference
	Content     string `yaml:"-"`           // actual file content (not frontmatter)
}

// MemorySystem manages project and user memory files.
// Project memories live in <project>/.claude/memory/
// User memories live in ~/.claude/memory/
type MemorySystem struct {
	projectDir string // .claude/memory/
	userDir    string // ~/.claude/memory/
}

// NewMemorySystem creates a new MemorySystem.
// projectDir is the .claude/memory/ path under the project root.
// userDir is the ~/.claude/memory/ path.
func NewMemorySystem(projectDir, userDir string) *MemorySystem {
	return &MemorySystem{
		projectDir: projectDir,
		userDir:    userDir,
	}
}

// LoadAll reads all .md memory files from both project and user directories.
func (m *MemorySystem) LoadAll() ([]MemoryEntry, error) {
	var entries []MemoryEntry

	projectEntries, err := m.loadFromDir(m.projectDir)
	if err != nil {
		return nil, fmt.Errorf("loading project memories: %w", err)
	}
	entries = append(entries, projectEntries...)

	userEntries, err := m.loadFromDir(m.userDir)
	if err != nil {
		// User memory dir is optional; don't fail if missing.
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("loading user memories: %w", err)
		}
	}
	entries = append(entries, userEntries...)

	// Sort by name for deterministic output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

// LoadIndex reads the MEMORY.md index file.
// It looks in the project directory first, then the user directory.
func (m *MemorySystem) LoadIndex() (string, error) {
	// Try project MEMORY.md first.
	projectIndex := filepath.Join(m.projectDir, "MEMORY.md")
	data, err := os.ReadFile(projectIndex)
	if err == nil {
		return string(data), nil
	}
	// Try user MEMORY.md.
	userIndex := filepath.Join(m.userDir, "MEMORY.md")
	data, err = os.ReadFile(userIndex)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// Save writes a memory file and updates the MEMORY.md index.
// The memory file is created at <dir>/<name>.md with YAML frontmatter.
func (m *MemorySystem) Save(name, memType, description, content string) error {
	// Determine which directory to save in based on type.
	dir := m.projectDir
	switch memType {
	case "user", "feedback":
		dir = m.userDir
	default:
		// project, reference -> project dir
		dir = m.projectDir
	}

	// Ensure directory exists.
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating memory directory: %w", err)
	}

	// Write the memory file with frontmatter.
	filename := sanitizeMemoryName(name) + ".md"
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
		return fmt.Errorf("writing memory file: %w", err)
	}

	// Update MEMORY.md index.
	return m.updateIndex(dir)
}

// Delete removes a memory file from the appropriate directory and updates the index.
func (m *MemorySystem) Delete(name string) error {
	filename := sanitizeMemoryName(name) + ".md"

	// Try to delete from project dir first.
	projectPath := filepath.Join(m.projectDir, filename)
	err := os.Remove(projectPath)
	if err == nil {
		return m.updateIndex(m.projectDir)
	}

	// Try user dir.
	userPath := filepath.Join(m.userDir, filename)
	err = os.Remove(userPath)
	if err == nil {
		return m.updateIndex(m.userDir)
	}

	return fmt.Errorf("memory '%s' not found", name)
}

// FormatForPrompt formats all memories for system prompt inclusion.
// It first tries to use MEMORY.md content, and falls back to listing
// all individual memory entries. Includes behavioral guidance for
// how the memory system should be used.
func (m *MemorySystem) FormatForPrompt() string {
	var guidance strings.Builder
	guidance.WriteString("# Memory\n\n")
	guidance.WriteString("This session has access to the memory system, which allows you to persist and recall information across sessions.\n\n")
	guidance.WriteString("## When to save memories\n")
	guidance.WriteString("- When the user explicitly asks you to remember something (\"remember this\", \"save this for next time\", \"next time I ask about X, tell me Y\")\n")
	guidance.WriteString("- When the user provides preferences about how they want things done (coding style, preferred libraries, naming conventions)\n")
	guidance.WriteString("- When you discover project-specific knowledge that would be useful in future sessions (build commands, deployment steps, architecture decisions)\n")
	guidance.WriteString("- When the user gives feedback about your behavior that should persist (\"always use tabs\", \"never use arrow functions\", \"I prefer pytest over unittest\")\n\n")
	guidance.WriteString("## When NOT to save memories\n")
	guidance.WriteString("- Do not save temporary or session-specific information (current task progress, conversation state)\n")
	guidance.WriteString("- Do not save information the user has not asked you to remember unless it is clearly a durable preference or project fact\n")
	guidance.WriteString("- Do not save sensitive information (API keys, passwords, tokens, secrets)\n")
	guidance.WriteString("- Do not duplicate information already present in instruction files (CLAW.md, CLAUDE.md)\n\n")
	guidance.WriteString("## When to access memories\n")
	guidance.WriteString("- At the start of a session, memories are automatically loaded into the system prompt\n")
	guidance.WriteString("- You may search or browse memories when context suggests a prior preference or fact may be relevant\n")
	guidance.WriteString("- When a user says something like \"what did I tell you about X\" or \"remember my preferences for Y\"\n\n")

	// Try the index file first.
	index, err := m.LoadIndex()
	if err == nil && index != "" {
		guidance.WriteString("## Stored memories\n\n")
		guidance.WriteString(strings.TrimSpace(index))
		return guidance.String()
	}

	// Fall back to loading all entries.
	entries, err := m.LoadAll()
	if err != nil || len(entries) == 0 {
		// Still return the guidance even if no memories are stored.
		return guidance.String()
	}

	guidance.WriteString("## Stored memories\n\n")
	for _, entry := range entries {
		guidance.WriteString(fmt.Sprintf("### %s (%s)\n", entry.Name, entry.Type))
		if entry.Description != "" {
			guidance.WriteString(fmt.Sprintf("> %s\n\n", entry.Description))
		}
		guidance.WriteString(entry.Content)
		guidance.WriteString("\n\n")
	}
	return guidance.String()
}

// --- internal helpers ---

// loadFromDir reads all .md files from a directory and parses frontmatter.
func (m *MemorySystem) loadFromDir(dir string) ([]MemoryEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var memories []MemoryEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		// Skip the index file itself.
		if entry.Name() == "MEMORY.md" {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		mem := parseMemoryFile(string(data))
		if mem.Name == "" {
			// Derive name from filename.
			mem.Name = strings.TrimSuffix(entry.Name(), ".md")
		}
		memories = append(memories, mem)
	}

	return memories, nil
}

// parseMemoryFile parses a memory file with optional YAML frontmatter.
// Frontmatter is delimited by --- lines at the start of the file.
func parseMemoryFile(content string) MemoryEntry {
	entry := MemoryEntry{}

	// Check for frontmatter.
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		entry.Content = strings.TrimSpace(content)
		return entry
	}

	// Find closing ---.
	rest := content[4:]
	endIdx := strings.Index(rest, "\n---")
	if endIdx < 0 {
		// No closing delimiter; treat whole thing as content.
		entry.Content = strings.TrimSpace(content)
		return entry
	}

	frontmatter := rest[:endIdx]
	bodyStart := endIdx + 4 // skip past \n---
	if bodyStart < len(rest) {
		entry.Content = strings.TrimSpace(rest[bodyStart:])
	}

	// Parse simple YAML key: value pairs.
	scanner := bufio.NewScanner(strings.NewReader(frontmatter))
	for scanner.Scan() {
		line := scanner.Text()
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		value := strings.TrimSpace(line[colonIdx+1:])

		switch key {
		case "name":
			entry.Name = value
		case "description":
			entry.Description = value
		case "type":
			entry.Type = value
		}
	}

	return entry
}

// updateIndex regenerates the MEMORY.md index file for a directory.
func (m *MemorySystem) updateIndex(dir string) error {
	entries, err := m.loadFromDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var buf strings.Builder
	buf.WriteString("# Memory Index\n\n")
	if len(entries) == 0 {
		buf.WriteString("No memories stored.\n")
	} else {
		for _, entry := range entries {
			buf.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", entry.Name, entry.Type, entry.Description))
		}
	}

	indexPath := filepath.Join(dir, "MEMORY.md")
	return os.WriteFile(indexPath, []byte(buf.String()), 0644)
}

// sanitizeMemoryName makes a name safe for use as a filename.
func sanitizeMemoryName(name string) string {
	// Replace spaces and special chars with hyphens.
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ToLower(name)
	// Keep only alphanumeric, hyphens, underscores.
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
