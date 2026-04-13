package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// globalUserPrompter is set by main to enable interactive user prompting.
var globalUserPrompter func(questions []interface{}) (string, error)

// SetUserPrompter sets the global user prompter for AskUserQuestion.
func SetUserPrompter(fn func(questions []interface{}) (string, error)) {
	globalUserPrompter = fn
}

func sleepTool() *ToolSpec {
	return &ToolSpec{
		Name:       "sleep",
		Permission: PermReadOnly,
		Description: "Sleep for a specified duration in milliseconds. Avoid unnecessary sleep commands between operations that can run immediately. Only use when genuinely waiting for an external process to complete (e.g., a server starting up). Keep durations short (under 5 seconds).",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"duration_ms": map[string]interface{}{"type": "integer", "description": "Duration in milliseconds", "minimum": 1},
			},
			"required": []string{"duration_ms"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			ms, _ := input["duration_ms"].(float64)
			if ms <= 0 {
				return "", fmt.Errorf("duration_ms must be positive")
			}
			if ms > 30000 {
				return "", fmt.Errorf("sleep duration too long (max 30s)")
			}
			time.Sleep(time.Duration(ms) * time.Millisecond)
			return fmt.Sprintf("Slept %dms", int(ms)), nil
		},
	}
}

// scoredTool is used internally for search ranking.
type scoredTool struct {
	name        string
	description string
	score       int
}

func toolSearchTool() *ToolSpec {
	return &ToolSpec{
		Name:       "ToolSearch",
		Permission: PermReadOnly,
		Description: "Search for available tools by keyword with multi-signal scoring engine.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"query":       map[string]interface{}{"type": "string", "description": "Search query (or select:name1,name2 for direct lookup)"},
				"max_results": map[string]interface{}{"type": "integer", "description": "Max results (default 5)"},
			},
			"required": []string{"query"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			query, _ := input["query"].(string)
			maxResults := 5
			if m, ok := input["max_results"].(float64); ok && m > 0 {
				maxResults = int(m)
			}
			if maxResults < 1 {
				maxResults = 1
			}

			// Select mode: direct lookup by name
			if strings.HasPrefix(query, "select:") {
				names := strings.Split(strings.TrimPrefix(query, "select:"), ",")
				var results []ToolSearchResult
				for _, n := range names {
					n = strings.TrimSpace(n)
					if spec, ok := globalRegistry.specs[n]; ok {
						results = append(results, ToolSearchResult{
							Name: spec.Name, Description: spec.Description, Score: 100,
						})
					}
				}
				out := ToolSearchOutput{Results: results, Total: len(results), Query: query}
				return formatTypedOutput(out)
			}

			// Split query into terms
			terms := strings.Fields(query)
			var required []string
			var optional []string
			for _, t := range terms {
				if strings.HasPrefix(t, "+") {
					required = append(required, canonicalToolToken(strings.TrimPrefix(t, "+")))
				} else {
					optional = append(optional, t)
				}
			}

			// Score each tool
			var scored []scoredTool
			for name, spec := range globalRegistry.specs {
				haystack := strings.ToLower(name + " " + spec.Description)
				canonicalName := canonicalToolToken(name)

				// Check required terms
				allRequired := true
				for _, req := range required {
					if !strings.Contains(haystack, req) {
						allRequired = false
						break
					}
				}
				if !allRequired {
					continue
				}

				score := 0
				for _, term := range optional {
					lowerTerm := strings.ToLower(term)
					canonicalTerm := canonicalToolToken(term)

					// Base match: term appears in haystack
					if strings.Contains(haystack, lowerTerm) {
						score += 2
					}
					// Tool name equals term exactly
					if strings.EqualFold(name, term) {
						score += 8
					}
					// Tool name contains term as substring
					if strings.Contains(strings.ToLower(name), lowerTerm) {
						score += 4
					}
					// Canonical name matches canonical term
					if canonicalName == canonicalTerm {
						score += 12
					}
					// Canonical haystack contains canonical term
					if canonicalName != "" && canonicalTerm != "" && strings.Contains(canonicalName, canonicalTerm) {
						score += 3
					}
				}

				if score > 0 {
					scored = append(scored, scoredTool{name: name, description: spec.Description, score: score})
				}
			}

			// Sort by score desc, then name asc
			sort.Slice(scored, func(i, j int) bool {
				if scored[i].score != scored[j].score {
					return scored[i].score > scored[j].score
				}
				return scored[i].name < scored[j].name
			})

			// Truncate
			if len(scored) > maxResults {
				scored = scored[:maxResults]
			}

			results := make([]ToolSearchResult, len(scored))
			for i, s := range scored {
				results[i] = ToolSearchResult{Name: s.name, Description: s.description, Score: s.score}
			}

			out := ToolSearchOutput{Results: results, Total: len(results), Query: query}
			return formatTypedOutput(out)
		},
	}
}

// canonicalToolToken strips non-alphanumeric chars, lowercases, and removes trailing "tool".
func canonicalToolToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	result := strings.ToLower(b.String())
	result = strings.TrimSuffix(result, "tool")
	return result
}

func notebookEditTool() *ToolSpec {
	return &ToolSpec{
		Name:       "NotebookEdit",
		Permission: PermWorkspaceWrite,
		Description: "Edit a Jupyter notebook cell. Replaces the entire cell content. Supports markdown and code cell types. Use edit_mode=insert to add new cells and edit_mode=delete to remove cells. For cell output retrieval, use the Read tool on the .ipynb file.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"notebook_path": map[string]interface{}{"type": "string", "description": "Absolute path to the .ipynb file"},
				"cell_id":       map[string]interface{}{"type": "string", "description": "Cell ID to edit"},
				"new_source":    map[string]interface{}{"type": "string", "description": "New cell source"},
				"cell_type":     map[string]interface{}{"type": "string", "description": "code or markdown", "enum": []string{"code", "markdown"}},
				"edit_mode":     map[string]interface{}{"type": "string", "description": "replace, insert, or delete", "enum": []string{"replace", "insert", "delete"}},
			},
			"required": []string{"notebook_path", "new_source"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			path, _ := input["notebook_path"].(string)
			newSource, _ := input["new_source"].(string)
			if path == "" {
				return "", fmt.Errorf("notebook_path is required")
			}
			if !strings.HasSuffix(path, ".ipynb") {
				return "", fmt.Errorf("file must be .ipynb")
			}

			data, err := os.ReadFile(path)
			if err != nil {
				notebook := map[string]interface{}{
					"cells": []interface{}{
						map[string]interface{}{
							"cell_type": "code", "source": newSource,
							"metadata": map[string]interface{}{}, "execution_count": nil, "outputs": []interface{}{},
						},
					},
					"metadata":    map[string]interface{}{},
					"nbformat":    4, "nbformat_minor": 5,
				}
				os.MkdirAll(filepath.Dir(path), 0755)
				nbData, _ := json.MarshalIndent(notebook, "", "  ")
				os.WriteFile(path, nbData, 0644)
				return "Created notebook: " + path, nil
			}

			var notebook map[string]interface{}
			if err := json.Unmarshal(data, &notebook); err != nil {
				return "", fmt.Errorf("failed to parse notebook JSON: %w", err)
			}
			cells, _ := notebook["cells"].([]interface{})
			cellID, _ := input["cell_id"].(string)
			editMode, _ := input["edit_mode"].(string)
			if editMode == "" {
				editMode = "replace"
			}

			switch editMode {
			case "insert":
				cellType, _ := input["cell_type"].(string)
				if cellType == "" {
					cellType = "code"
				}
				cells = append(cells, map[string]interface{}{
					"cell_type": cellType, "source": newSource,
					"metadata": map[string]interface{}{}, "execution_count": nil, "outputs": []interface{}{},
				})
				notebook["cells"] = cells
			case "delete":
				for i, c := range cells {
					if cell, ok := c.(map[string]interface{}); ok {
						if id, ok := cell["id"].(string); ok && id == cellID {
							cells = append(cells[:i], cells[i+1:]...)
							break
						}
					}
				}
				notebook["cells"] = cells
			default:
				if cellID != "" {
					for i, c := range cells {
						if cell, ok := c.(map[string]interface{}); ok {
							if id, ok := cell["id"].(string); ok && id == cellID {
								cell["source"] = newSource
								cells[i] = cell
								break
							}
						}
					}
				} else if len(cells) > 0 {
					if cell, ok := cells[len(cells)-1].(map[string]interface{}); ok {
						cell["source"] = newSource
					}
				}
				notebook["cells"] = cells
			}

			nbData, _ := json.MarshalIndent(notebook, "", "  ")
			os.WriteFile(path, nbData, 0644)
			return fmt.Sprintf("Notebook edited: %s (%s)", path, editMode), nil
		},
	}
}

func askUserQuestionTool() *ToolSpec {
	return &ToolSpec{
		Name:        "AskUserQuestion",
		Permission:  PermReadOnly,
		Description: "Use this tool when you need to ask the user questions during execution. This allows you to gather user preferences or requirements, clarify ambiguous instructions, or get decisions on implementation choices.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"questions": map[string]interface{}{
					"type":        "array",
					"description": "Questions to ask the user (1-4 questions)",
					"items": map[string]interface{}{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]interface{}{
							"question": map[string]interface{}{"type": "string", "description": "The complete question to ask the user"},
							"header":   map[string]interface{}{"type": "string", "description": "Short label for the question (max 12 chars)"},
							"options": map[string]interface{}{
								"type":        "array",
								"description": "Available choices (2-4 options)",
								"items": map[string]interface{}{
									"type":                 "object",
									"additionalProperties": false,
									"properties": map[string]interface{}{
										"label":       map[string]interface{}{"type": "string", "description": "Display text for this option"},
										"description": map[string]interface{}{"type": "string", "description": "What this option means"},
									},
									"required": []string{"label", "description"},
								},
								"minItems": 2,
								"maxItems": 4,
							},
							"multiSelect": map[string]interface{}{"type": "boolean", "description": "Allow selecting multiple options", "default": false},
						},
						"required": []string{"question", "options", "multiSelect"},
					},
					"minItems": 1,
					"maxItems": 4,
				},
				"answers":      map[string]interface{}{"type": "object", "description": "User answers collected by the permission component", "additionalProperties": map[string]interface{}{"type": "string"}},
				"annotations":  map[string]interface{}{"type": "object", "description": "Optional per-question annotations from the user", "additionalProperties": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"preview": map[string]interface{}{"type": "string"}}}},
				"metadata":     map[string]interface{}{"type": "object", "description": "Optional metadata for tracking and analytics purposes", "additionalProperties": false},
			},
			"required": []string{"questions"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			questionsRaw, ok := input["questions"].([]interface{})
			if !ok || len(questionsRaw) == 0 {
				return "", fmt.Errorf("questions array is required")
			}

			// If we have an interactive prompter, use it
			if globalUserPrompter != nil {
				return globalUserPrompter(questionsRaw)
			}

			// Fallback: format questions as text
			var buf strings.Builder
			buf.WriteString("Questions for the user:\n\n")
			for i, qRaw := range questionsRaw {
				q, ok := qRaw.(map[string]interface{})
				if !ok {
					continue
				}
				question, _ := q["question"].(string)
				header, _ := q["header"].(string)
				if header == "" {
					header = fmt.Sprintf("Q%d", i+1)
				}
				fmt.Fprintf(&buf, "%s: %s\n", header, question)

				if options, ok := q["options"].([]interface{}); ok {
					for j, optRaw := range options {
						opt, ok := optRaw.(map[string]interface{})
						if !ok {
							continue
						}
						label, _ := opt["label"].(string)
						desc, _ := opt["description"].(string)
						fmt.Fprintf(&buf, "  %d. %s - %s\n", j+1, label, desc)
					}
				}
				buf.WriteString("\n")
			}
			buf.WriteString("(No interactive prompter available - user input not captured)")
			return buf.String(), nil
		},
	}
}

// Plan mode tools

var globalPlanModeActive bool
var globalPlanModeSetter func(active bool)

// SetPlanModeSetter sets the callback invoked when plan mode changes.
func SetPlanModeSetter(fn func(active bool)) {
	globalPlanModeSetter = fn
}

func enterPlanModeTool() *ToolSpec {
	return &ToolSpec{
		Name:        "EnterPlanMode",
		Permission:  PermReadOnly,
		Description: "Enter plan mode. In plan mode, the assistant explores the codebase and designs an implementation approach without making changes. Use when you need to plan before implementing.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"reason": map[string]interface{}{"type": "string", "description": "Reason for entering plan mode"},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			globalPlanModeActive = true
			if globalPlanModeSetter != nil {
				globalPlanModeSetter(true)
			}
			reason, _ := input["reason"].(string)
			if reason != "" {
				return fmt.Sprintf("Entered plan mode: %s\nIn plan mode, I will explore and plan without making changes.", reason), nil
			}
			return "Entered plan mode. I will explore the codebase and design an approach before making changes.", nil
		},
	}
}

func exitPlanModeTool() *ToolSpec {
	return &ToolSpec{
		Name:        "ExitPlanMode",
		Permission:  PermReadOnly,
		Description: "Exit plan mode and return to normal implementation mode.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"plan": map[string]interface{}{"type": "string", "description": "The implementation plan text for user review"},
				"allowedPrompts": map[string]interface{}{"type": "array", "description": "Permission-based prompts needed to implement the plan", "items": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"tool": map[string]interface{}{"type": "string", "description": "Tool name"}, "prompt": map[string]interface{}{"type": "string", "description": "Semantic description of the action"}}, "required": []string{"tool", "prompt"}}},
			},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			globalPlanModeActive = false
			if globalPlanModeSetter != nil {
				globalPlanModeSetter(false)
			}
			plan, _ := input["plan"].(string)
			if plan != "" {
				return fmt.Sprintf("Exited plan mode. Plan:\n%s\nNow ready to implement.", plan), nil
			}
			return "Exited plan mode. Ready to implement changes.", nil
		},
	}
}

// Background task management tools

var globalTaskManager *BackgroundTaskManager

// BackgroundTaskManager manages background shell tasks.
type BackgroundTaskManager struct {
	mu    sync.Mutex
	tasks map[string]*BackgroundTask
}

// BackgroundTask represents a running background task.
type BackgroundTask struct {
	ID        string
	Command   string
	Process   *os.Process
	Output    string
	Status    string // "running", "completed", "failed", "stopped"
	StartedAt time.Time
	done     chan struct{} // closed when task finishes
}

// SetTaskManager sets the global background task manager.
func SetTaskManager(m *BackgroundTaskManager) {
	globalTaskManager = m
}

// NewBackgroundTaskManager creates a new BackgroundTaskManager.
func NewBackgroundTaskManager() *BackgroundTaskManager {
	return &BackgroundTaskManager{tasks: make(map[string]*BackgroundTask)}
}

// AddTask registers a background task and starts monitoring it
func (m *BackgroundTaskManager) AddTask(task *BackgroundTask) {
	m.mu.Lock()
	m.tasks[task.ID] = task
	m.mu.Unlock()
}

// WaitForTask blocks until the task reaches a terminal state or timeout elapses.
func (m *BackgroundTaskManager) WaitForTask(taskID string, timeoutMs int) (*BackgroundTask, error) {
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	for {
		m.mu.Lock()
		task, ok := m.tasks[taskID]
		m.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("task %s not found", taskID)
		}
		if task.Status != "running" {
			return task, nil
		}
		if time.Now().After(deadline) {
			return task, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func taskOutputTool() *ToolSpec {
	return &ToolSpec{
		Name:        "TaskOutput",
		Permission:  PermReadOnly,
		Description: "Get the output of a background task. Returns the task's current output and status.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{"type": "string", "description": "The ID of the background task"},
				"block":   map[string]interface{}{"type": "boolean", "description": "Wait for completion (default true)", "default": true},
				"timeout": map[string]interface{}{"type": "integer", "description": "Max wait time in ms (default 30000)", "minimum": 0, "maximum": 600000, "default": 30000},
			},
			"required": []string{"task_id"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			taskID, _ := input["task_id"].(string)
			if taskID == "" {
				return "", fmt.Errorf("task_id is required")
			}
			if globalTaskManager == nil {
				return "", fmt.Errorf("no task manager available")
			}

			// Check if block is set (default true)
			block := true
			if b, ok := input["block"].(bool); ok {
				block = b
			}
			timeoutMs := 30000
			if t, ok := input["timeout"].(float64); ok && int(t) > 0 {
				timeoutMs = int(t)
			}

			if block {
				task, err := globalTaskManager.WaitForTask(taskID, timeoutMs)
				if err != nil {
					return "", err
				}
				elapsed := time.Since(task.StartedAt).Truncate(time.Second)
				return fmt.Sprintf("Task %s [%s] (%s elapsed)\n%s", taskID, task.Status, elapsed, task.Output), nil
			}

			// Non-blocking: return current state immediately
			globalTaskManager.mu.Lock()
			task, ok := globalTaskManager.tasks[taskID]
			if !ok {
				globalTaskManager.mu.Unlock()
				return "", fmt.Errorf("task %s not found", taskID)
			}
			globalTaskManager.mu.Unlock()

			status := task.Status
			output := task.Output
			elapsed := time.Since(task.StartedAt).Truncate(time.Second)
			return fmt.Sprintf("Task %s [%s] (%s elapsed)\n%s", taskID, status, elapsed, output), nil
		},
	}
}

func taskStopTool() *ToolSpec {
	return &ToolSpec{
		Name:        "TaskStop",
		Permission:  PermDangerFullAccess,
		Description: "Stop a running background task.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{"type": "string", "description": "The ID of the background task to stop"},
			},
			"required": []string{"task_id"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			taskID, _ := input["task_id"].(string)
			if taskID == "" {
				return "", fmt.Errorf("task_id is required")
			}
			if globalTaskManager == nil {
				return "", fmt.Errorf("no task manager available")
			}
			globalTaskManager.mu.Lock()
			task, ok := globalTaskManager.tasks[taskID]
			if !ok {
				globalTaskManager.mu.Unlock()
				return "", fmt.Errorf("task %s not found", taskID)
			}
			if task.Status != "running" {
				globalTaskManager.mu.Unlock()
				return fmt.Sprintf("Task %s is already %s", taskID, task.Status), nil
			}
			if task.Process != nil {
				task.Process.Kill()
			}
			task.Status = "stopped"
			globalTaskManager.mu.Unlock()
			return fmt.Sprintf("Task %s stopped.", taskID), nil
		},
	}
}
