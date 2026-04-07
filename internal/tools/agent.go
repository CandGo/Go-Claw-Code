package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AgentRuntime is a minimal interface for executing agent sub-tasks.
type AgentRuntime interface {
	ExecuteSubAgent(ctx context.Context, prompt string, maxIterations int, agentType string, modelOverride string) (string, error)
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
	ModelOverride string  `json:"model_override,omitempty"`

	// resultCh is used for background agents. When the agent finishes,
	// the output is sent on this channel and also stored in Output.
	// The TaskOutput tool polls this via the Output field.
	resultCh chan string `json:"-"`
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
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"description":       map[string]interface{}{"type": "string", "description": "Short description of the task"},
				"prompt":            map[string]interface{}{"type": "string", "description": "The full task prompt"},
				"subagent_type":     map[string]interface{}{"type": "string", "description": "Agent type: general-purpose, Explore, Plan, Verification, claude-code-guide, statusline-setup", "enum": []string{"general-purpose", "Explore", "Plan", "claude-code-guide", "statusline-setup", "Verification"}},
				"model":             map[string]interface{}{"type": "string", "description": "Model override", "enum": []string{"sonnet", "opus", "haiku"}},
				"run_in_background": map[string]interface{}{"type": "boolean", "description": "Run the agent asynchronously, return immediately"},
				"isolation":         map[string]interface{}{"type": "string", "description": "'worktree' to create an isolated copy, default '' for shared workspace", "enum": []string{"worktree"}},
				"max_iterations":    map[string]interface{}{"type": "integer", "description": "Override max iterations for this agent", "minimum": 1},
			},
			"required": []string{"prompt"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			description, _ := input["description"].(string)
			prompt, _ := input["prompt"].(string)
			agentType, _ := input["subagent_type"].(string)
			if agentType == "" {
				agentType = "general-purpose"
			}

			runInBackground := false
			if v, ok := input["run_in_background"].(bool); ok {
				runInBackground = v
			}

			isolation, _ := input["isolation"].(string)
			modelOverride, _ := input["model"].(string)

			maxIter := MaxIterationsForAgent(agentType)
			if v, ok := input["max_iterations"].(float64); ok && v > 0 {
				maxIter = int(v)
			}

			agentMu.Lock()
			agentSeq++
			job := AgentJob{
				ID:            fmt.Sprintf("agent-%d", agentSeq),
				Name:          fmt.Sprintf("Agent %d", agentSeq),
				Description:   description,
				Type:          agentType,
				Status:        "running",
				Prompt:        prompt,
				StartedAt:     time.Now(),
				ModelOverride: modelOverride,
			}
			if runInBackground {
				job.resultCh = make(chan string, 1)
			}
			agents = append(agents, job)
			agentMu.Unlock()

			// Persist agent state
			agentDir := ".claw-agents"
			os.MkdirAll(agentDir, 0755)
			manifest, _ := json.MarshalIndent(job, "", "  ")
			os.WriteFile(filepath.Join(agentDir, job.ID+".json"), manifest, 0644)

			// If isolation == "worktree", create a git worktree, run agent there, clean up.
			worktreeDir := ""
			if isolation == "worktree" {
				wtBranch := fmt.Sprintf("agent-%s", job.ID)
				wtPath := fmt.Sprintf(".claude/worktrees/%s", job.ID)
				os.MkdirAll(".claude/worktrees", 0755)

				wtCmd := exec.Command("git", "worktree", "add", wtPath, "-b", wtBranch, "HEAD")
				if _, wtErr := wtCmd.CombinedOutput(); wtErr != nil {
					// Try reusing existing branch
					wtCmd = exec.Command("git", "worktree", "add", wtPath, wtBranch)
					if wtOut2, wtErr2 := wtCmd.CombinedOutput(); wtErr2 != nil {
						return "", fmt.Errorf("failed to create worktree for agent: %s: %w", string(wtOut2), wtErr2)
					}
				}
				worktreeDir = wtPath
			}

			// runAgent is the core execution logic, factored out so it can
			// run synchronously or in a goroutine for background mode.
			runAgent := func() (string, error) {
				// Panic recovery for agent execution
				defer func() {
					if r := recover(); r != nil {
						agentMu.Lock()
						job.Status = "failed"
						job.Output = fmt.Sprintf("panic: %v", r)
						job.CompletedAt = time.Now()
						manifest, _ = json.MarshalIndent(job, "", "  ")
						os.WriteFile(filepath.Join(agentDir, job.ID+".json"), manifest, 0644)
						if job.resultCh != nil {
							select {
							case job.resultCh <- job.Output:
							default:
							}
						}
						agentMu.Unlock()

						// Clean up worktree if isolated
						if worktreeDir != "" {
							exec.Command("git", "worktree", "remove", "--force", worktreeDir).Run()
						}
					}
				}()

				// Execute sub-agent if runtime is available
				if agentRuntime != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()

					output, err := agentRuntime.ExecuteSubAgent(ctx, prompt, maxIter, agentType, modelOverride)

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
					if job.resultCh != nil {
						select {
						case job.resultCh <- job.Output:
						default:
						}
					}
					agentMu.Unlock()

					// Clean up worktree if isolated
					if worktreeDir != "" {
						exec.Command("git", "worktree", "remove", "--force", worktreeDir).Run()
					}

					if err != nil {
						return fmt.Sprintf("Agent %s failed: %v", job.ID, err), nil
					}
					return output, nil
				}

				// Clean up worktree if isolated (no runtime path)
				if worktreeDir != "" {
					exec.Command("git", "worktree", "remove", "--force", worktreeDir).Run()
				}

				// Fallback: no runtime available
				return fmt.Sprintf("Agent task not executed: %s\nType: %s\nStatus: pending\n\nNo agent runtime available. The agent prompt has been recorded but will not run.", description, agentType), nil
			}

			// Background execution: launch in a goroutine and return the ID immediately.
			if runInBackground {
				go func() {
					result, _ := runAgent()
					_ = result
				}()
				return fmt.Sprintf("Agent %s started in background.\nDescription: %s\nType: %s\nMaxIterations: %d\nIsolation: %s\n\nUse TaskOutput with task_id=%s to poll for results.", job.ID, description, agentType, maxIter, isolation, job.ID), nil
			}

			// Synchronous execution
			return runAgent()
		},
	}
}

// ConcurrentExecute runs multiple agent jobs concurrently with panic protection.
func ConcurrentExecute(jobs []*AgentJob, fn func(*AgentJob) (string, error)) {
	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Add(1)
		go func(j *AgentJob) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					agentMu.Lock()
					j.Status = "failed"
					j.Output = fmt.Sprintf("panic: %v", r)
					j.CompletedAt = time.Now()
					agentMu.Unlock()
				}
			}()
			output, err := fn(j)
			agentMu.Lock()
			if err != nil {
				j.Status = "failed"
				j.Output = fmt.Sprintf("error: %v", err)
			} else {
				j.Status = "completed"
				j.Output = output
			}
			j.CompletedAt = time.Now()
			agentMu.Unlock()
		}(job)
	}
	wg.Wait()
}

func skillTool() *ToolSpec {
	return &ToolSpec{
		Name:        "Skill",
		Permission:  PermReadOnly,
		Description: "Execute a named skill.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"additionalProperties": false,
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
		".claude/skills",
		filepath.Join(os.Getenv("HOME"), ".claude/skills"),
		".claw/skills",
		filepath.Join(os.Getenv("HOME"), ".go-claw/skills"),
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
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"message":     map[string]interface{}{"type": "string", "description": "The message to send"},
				"status":      map[string]interface{}{"type": "string", "enum": []string{"normal", "proactive"}, "description": "Message status type"},
				"attachments": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Optional attachment paths or URLs"},
			},
			"required": []string{"message"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			message, _ := input["message"].(string)
			status, _ := input["status"].(string)
			if status == "" {
				status = "normal"
			}
			if globalUserMessageHandler != nil {
				globalUserMessageHandler(message, status)
				return "Message sent to user.", nil
			}
			return "[User message (" + status + "): " + message + "]", nil
		},
	}
}

var globalUserMessageHandler func(message, status string)

// SetUserMessageHandler sets the callback for delivering messages to the user.
func SetUserMessageHandler(fn func(string, string)) {
	globalUserMessageHandler = fn
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
		".claude/skills",
		filepath.Join(os.Getenv("HOME"), ".claude/skills"),
		".claw/skills",
		filepath.Join(os.Getenv("HOME"), ".go-claw/skills"),
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
