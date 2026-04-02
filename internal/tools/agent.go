package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AgentRuntime is a minimal interface for executing agent sub-tasks.
type AgentRuntime interface {
	ExecuteSubAgent(ctx context.Context, prompt string, maxIterations int, agentType string) (string, error)
}

var agentRuntime AgentRuntime

// SetAgentRuntime sets the global agent runtime for sub-agent execution.
func SetAgentRuntime(rt AgentRuntime) {
	agentRuntime = rt
}

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
		Permission:  PermDangerFullAccess,
		Description: "Launch a sub-agent to handle a task autonomously.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"description":   map[string]interface{}{"type": "string", "description": "Short description of the task"},
				"prompt":        map[string]interface{}{"type": "string", "description": "The full task prompt"},
				"subagent_type": map[string]interface{}{"type": "string", "description": "Agent type: general-purpose, Explore, Plan"},
				"name":          map[string]interface{}{"type": "string", "description": "Optional agent name"},
				"model":         map[string]interface{}{"type": "string", "description": "Model override"},
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
				Status:      "running",
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

			// Execute sub-agent if runtime is available
			if agentRuntime != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()

				maxIter := 10
				if agentType == "Explore" {
					maxIter = 5
				} else if agentType == "Plan" {
					maxIter = 3
				}

				output, err := agentRuntime.ExecuteSubAgent(ctx, prompt, maxIter, agentType)

				agentMu.Lock()
				job.Status = "completed"
				job.Output = output
				job.CompletedAt = time.Now()
				if err != nil {
					job.Status = "failed"
					job.Output = fmt.Sprintf("error: %v", err)
				}
				// Update persisted state
				manifest, _ = json.MarshalIndent(job, "", "  ")
				os.WriteFile(filepath.Join(agentDir, job.ID+".json"), manifest, 0644)
				agentMu.Unlock()

				if err != nil {
					return fmt.Sprintf("Agent %s failed: %v", job.ID, err), nil
				}
				return output, nil
			}

			// Fallback: queue only
			return fmt.Sprintf("Agent task queued: %s\nType: %s\nStatus: pending\n\nNote: No agent runtime available. The agent prompt has been recorded.", description, agentType), nil
		},
	}
}

func skillTool() *ToolSpec {
	return &ToolSpec{
		Name:        "Skill",
		Permission:  PermReadOnly,
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
	searchDirs := []string{
		".claw/skills",
		filepath.Join(os.Getenv("HOME"), ".claw/skills"),
	}

	for _, dir := range searchDirs {
		skillFile := filepath.Join(dir, name, "SKILL.md")
		if _, err := os.Stat(skillFile); err == nil {
			return skillFile
		}
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
		Permission:  PermReadOnly,
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
