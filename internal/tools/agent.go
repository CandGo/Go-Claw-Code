package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AgentJob represents a running sub-agent.
type AgentJob struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Type        string    `json:"type"`
	Status      string    `json:"status"` // running, completed, failed
	Prompt      string    `json:"prompt"`
	Output      string    `json:"output,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

var (
	agents   []AgentJob
	agentMu  sync.Mutex
	agentSeq int
)

func agentTool() *ToolSpec {
	return &ToolSpec{
		Name:        "Agent",
		Description: "Launch a sub-agent to handle a task autonomously.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"description":    map[string]interface{}{"type": "string", "description": "Short description of the task"},
				"prompt":         map[string]interface{}{"type": "string", "description": "The full task prompt"},
				"subagent_type":  map[string]interface{}{"type": "string", "description": "Agent type: general-purpose, Explore, Plan"},
				"name":           map[string]interface{}{"type": "string", "description": "Optional agent name"},
				"model":          map[string]interface{}{"type": "string", "description": "Model override"},
			},
			"required": []string{"description", "prompt"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			description, _ := input["description"].(string)
			prompt, _ := input["prompt"].(string)
			agentType, _ := input["subagent_type"].(string)
			if agentType == "" {
				agentType = "general-purpose"
			}

			agentMu.Lock()
			agentSeq++
			job := AgentJob{
				ID:          fmt.Sprintf("agent-%d", agentSeq),
				Name:        fmt.Sprintf("Agent %d", agentSeq),
				Description: description,
				Type:        agentType,
				Status:      "pending",
				Prompt:      prompt,
				StartedAt:   time.Now(),
			}
			agents = append(agents, job)
			agentMu.Unlock()

			// Persist agent state
			agentDir := ".claw-agents"
			os.MkdirAll(agentDir, 0755)
			manifest, _ := json.MarshalIndent(job, "", "  ")
			os.WriteFile(filepath.Join(agentDir, job.ID+".json"), manifest, 0644)

			// For MVP: return info about the agent task
			// In a full implementation, this would spawn a goroutine with its own ConversationRuntime
			output := fmt.Sprintf("Agent task queued: %s\nType: %s\nStatus: pending\n\nNote: Sub-agent execution requires a running conversation loop. The agent prompt has been recorded.", description, agentType)

			return output, nil
		},
	}
}

func skillTool() *ToolSpec {
	return &ToolSpec{
		Name:        "Skill",
		Description: "Execute a named skill.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"skill": map[string]interface{}{"type": "string", "description": "Skill name"},
				"args":  map[string]interface{}{"type": "string", "description": "Arguments for the skill"},
			},
			"required": []string{"skill"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			skillName, _ := input["skill"].(string)
			args, _ := input["args"].(string)
			if skillName == "" {
				return "", fmt.Errorf("skill name is required")
			}

			// Search for skill in .claw/skills/ and ~/.claw/skills/
			skillPath := discoverSkill(skillName)
			if skillPath == "" {
				return fmt.Sprintf("Skill '%s' not found. No skills directory found.", skillName), nil
			}

			data, err := os.ReadFile(skillPath)
			if err != nil {
				return "", fmt.Errorf("failed to read skill: %w", err)
			}

			content := string(data)
			if args != "" {
				content = content + "\n\nArguments: " + args
			}

			return fmt.Sprintf("Skill: %s\nPath: %s\n\n%s", skillName, skillPath, content), nil
		},
	}
}

func discoverSkill(name string) string {
	// Search paths: .claw/skills/, ~/.claw/skills/
	searchDirs := []string{
		".claw/skills",
		filepath.Join(os.Getenv("HOME"), ".claw/skills"),
	}

	for _, dir := range searchDirs {
		// Check for <name>/SKILL.md
		skillFile := filepath.Join(dir, name, "SKILL.md")
		if _, err := os.Stat(skillFile); err == nil {
			return skillFile
		}
		// Check for <name>.md
		skillFile = filepath.Join(dir, name+".md")
		if _, err := os.Stat(skillFile); err == nil {
			return skillFile
		}
	}
	return ""
}

func sendUserMessageTool() *ToolSpec {
	return &ToolSpec{
		Name:        "SendUserMessage",
		Description: "Send a message or ask a question to the user.",
		Aliases:     []string{"Brief"},
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{"type": "string", "description": "The message to send"},
			},
			"required": []string{"message"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			message, _ := input["message"].(string)
			return "[User message: " + message + "]", nil
		},
	}
}

// GetAgents returns all agent jobs.
func GetAgents() []AgentJob {
	agentMu.Lock()
	defer agentMu.Unlock()
	return append([]AgentJob{}, agents...)
}

// DiscoverSkills lists all available skills.
func DiscoverSkills() []string {
	var skills []string
	searchDirs := []string{
		".claw/skills",
		filepath.Join(os.Getenv("HOME"), ".claw/skills"),
	}
	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasSuffix(name, ".md") {
				skills = append(skills, strings.TrimSuffix(name, ".md"))
			} else if e.IsDir() {
				if _, err := os.Stat(filepath.Join(dir, name, "SKILL.md")); err == nil {
					skills = append(skills, name)
				}
			}
		}
	}
	return skills
}
