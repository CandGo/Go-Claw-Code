package tools

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CronJob represents a scheduled task.
type CronJob struct {
	ID        string `json:"id"`
	Cron      string `json:"cron"`
	Prompt    string `json:"prompt"`
	Recurring bool   `json:"recurring"`
	Durable   bool   `json:"durable"`
	CreatedAt int64  `json:"created_at"`
}

// cronStore manages scheduled cron jobs.
var (
	cronJobs   []*CronJob
	cronMu     sync.Mutex
	cronNextID int
)

func init() {
	cronJobs = make([]*CronJob, 0)
	cronNextID = 1
}

// CronScheduler runs a background goroutine that periodically checks cron
// jobs and fires matching ones by calling the promptFn callback.
type CronScheduler struct {
	mu       sync.Mutex
	jobs     []*CronJob
	promptFn func(prompt string) (string, error)
	stopCh   chan struct{}
	running  bool
}

// global cronScheduler instance, set via SetCronScheduler
var cronScheduler *CronScheduler

// SetCronScheduler sets the global cron scheduler used by tool handlers.
func SetCronScheduler(s *CronScheduler) {
	cronScheduler = s
}

// NewCronScheduler creates a new CronScheduler with the given prompt
// execution callback.
func NewCronScheduler(promptFn func(prompt string) (string, error)) *CronScheduler {
	return &CronScheduler{
		promptFn: promptFn,
		stopCh:   make(chan struct{}),
	}
}

// Start launches the background goroutine that checks jobs every 30 seconds.
func (s *CronScheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go s.run()
}

func (s *CronScheduler) run() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case now := <-ticker.C:
			s.fireMatching(now)
		}
	}
}

func (s *CronScheduler) fireMatching(now time.Time) {
	s.mu.Lock()
	var toFire []*CronJob
	for _, j := range s.jobs {
		if matchesCron(j.Cron, now) {
			toFire = append(toFire, j)
		}
	}
	s.mu.Unlock()

	for _, j := range toFire {
		if _, err := s.promptFn(j.Prompt); err != nil {
			log.Printf("[cron] error executing job %s: %v", j.ID, err)
		} else {
			log.Printf("[cron] fired job %s", j.ID)
		}

		// Remove one-shot jobs after firing.
		if !j.Recurring {
			s.mu.Lock()
			var remaining []*CronJob
			for _, jj := range s.jobs {
				if jj.ID != j.ID {
					remaining = append(remaining, jj)
				}
			}
			s.jobs = remaining
			s.mu.Unlock()

			// Also remove from the global store.
			cronMu.Lock()
			var storeRemaining []*CronJob
			for _, jj := range cronJobs {
				if jj.ID != j.ID {
					storeRemaining = append(storeRemaining, jj)
				}
			}
			cronJobs = storeRemaining
			cronMu.Unlock()
		}
	}
}

// Stop signals the background goroutine to exit and waits for it to finish.
func (s *CronScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopCh)
}

// SyncJobs replaces the scheduler's internal job list with the latest from
// the store.
func (s *CronScheduler) SyncJobs(jobs []*CronJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = make([]*CronJob, len(jobs))
	copy(s.jobs, jobs)
}

// matchesCron parses a standard 5-field cron expression and returns whether
// the given time matches. Fields: minute hour day-of-month month day-of-week.
// Supported syntax: *, specific numbers, ranges (1-5), steps (*/15),
// lists (1,3,5).
func matchesCron(cronExpr string, t time.Time) bool {
	fields := strings.Fields(cronExpr)
	if len(fields) != 5 {
		return false
	}

	minute := t.Minute()
	hour := t.Hour()
	day := t.Day()
	month := int(t.Month())
	weekday := int(t.Weekday()) // Sunday = 0

	return matchField(fields[0], minute, 0, 59) &&
		matchField(fields[1], hour, 0, 23) &&
		matchField(fields[2], day, 1, 31) &&
		matchField(fields[3], month, 1, 12) &&
		matchField(fields[4], weekday, 0, 6)
}

// matchField checks whether a single cron field matches the given value.
func matchField(field string, value, minVal, maxVal int) bool {
	// Handle lists first (e.g., "1,3,5")
	if strings.Contains(field, ",") {
		for _, part := range strings.Split(field, ",") {
			if matchField(part, value, minVal, maxVal) {
				return true
			}
		}
		return false
	}

	// Handle steps (e.g., "*/15", "1-10/2")
	if strings.Contains(field, "/") {
		parts := strings.SplitN(field, "/", 2)
		rangePart := parts[0]
		step, err := strconv.Atoi(parts[1])
		if err != nil || step == 0 {
			return false
		}

		var rangeMin, rangeMax int
		if rangePart == "*" {
			rangeMin = minVal
			rangeMax = maxVal
		} else if strings.Contains(rangePart, "-") {
			bounds := strings.SplitN(rangePart, "-", 2)
			rangeMin, err = strconv.Atoi(bounds[0])
			if err != nil {
				return false
			}
			rangeMax, err = strconv.Atoi(bounds[1])
			if err != nil {
				return false
			}
		} else {
			rangeMin, err = strconv.Atoi(rangePart)
			if err != nil {
				return false
			}
			rangeMax = maxVal
		}

		if value < rangeMin || value > rangeMax {
			return false
		}
		return (value-rangeMin)%step == 0
	}

	// Handle ranges (e.g., "1-5")
	if strings.Contains(field, "-") {
		bounds := strings.SplitN(field, "-", 2)
		lo, err := strconv.Atoi(bounds[0])
		if err != nil {
			return false
		}
		hi, err := strconv.Atoi(bounds[1])
		if err != nil {
			return false
		}
		return value >= lo && value <= hi
	}

	// Handle wildcard
	if field == "*" {
		return true
	}

	// Specific number
	n, err := strconv.Atoi(field)
	if err != nil {
		return false
	}
	return value == n
}

func cronCreateTool() *ToolSpec {
	return &ToolSpec{
		Name:        "CronCreate",
		Permission:  PermWorkspaceWrite,
		Description: "Schedule a prompt to be enqueued at a future time. Supports both recurring schedules and one-shot reminders.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"cron": map[string]interface{}{
					"type":        "string",
					"description": "Standard 5-field cron expression (minute hour day-of-month month day-of-week)",
				},
				"prompt": map[string]interface{}{
					"type":        "string",
					"description": "The prompt to enqueue at each fire time",
				},
				"recurring": map[string]interface{}{
					"type":        "boolean",
					"default":     true,
					"description": "True = fire on every cron match until deleted. False = fire once then auto-delete",
				},
				"durable": map[string]interface{}{
					"type":        "boolean",
					"default":     false,
					"description": "True = persist to .claude/scheduled_tasks.json so it survives restarts",
				},
			},
			"required": []string{"cron", "prompt"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			cronExpr, _ := input["cron"].(string)
			prompt, _ := input["prompt"].(string)
			recurring := true
			if v, ok := input["recurring"].(bool); ok {
				recurring = v
			}
			durable := false
			if v, ok := input["durable"].(bool); ok {
				durable = v
			}

			if cronExpr == "" {
				return "", fmt.Errorf("cron expression is required")
			}
			if prompt == "" {
				return "", fmt.Errorf("prompt is required")
			}

			cronMu.Lock()
			job := &CronJob{
				ID:        fmt.Sprintf("cron_%d", cronNextID),
				Cron:      cronExpr,
				Prompt:    prompt,
				Recurring: recurring,
				Durable:   durable,
				CreatedAt: time.Now().Unix(),
			}
			cronNextID++
			cronJobs = append(cronJobs, job)
			cronMu.Unlock()

			// Sync the live scheduler
			if cronScheduler != nil {
				cronScheduler.SyncJobs(GetCronJobs())
			}

			// Persist if durable
			if durable {
				if err := saveCronJobs(); err != nil {
					return fmt.Sprintf("Job created: %s (persist failed: %v)", job.ID, err), nil
				}
			}

			return fmt.Sprintf("<cron_result>\n  <id>%s</id>\n  <cron>%s</cron>\n  <recurring>%v</recurring>\n  <durable>%v</durable>\n</cron_result>",
				job.ID, job.Cron, job.Recurring, job.Durable), nil
		},
	}
}

func cronDeleteTool() *ToolSpec {
	return &ToolSpec{
		Name:        "CronDelete",
		Permission:  PermWorkspaceWrite,
		Description: "Cancel a previously scheduled cron job.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"additionalProperties": false,
			"properties": map[string]interface{}{
				"id": map[string]interface{}{
					"type":        "string",
					"description": "Job ID returned by CronCreate",
				},
			},
			"required": []string{"id"},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			id, _ := input["id"].(string)
			if id == "" {
				return "", fmt.Errorf("job id is required")
			}

			cronMu.Lock()
			found := false
			var newJobs []*CronJob
			for _, j := range cronJobs {
				if j.ID == id {
					found = true
					continue
				}
				newJobs = append(newJobs, j)
			}
			if found {
				cronJobs = newJobs
			}
			cronMu.Unlock()

			if !found {
				return fmt.Sprintf("Job %s not found", id), nil
			}

			// Sync the live scheduler
			if cronScheduler != nil {
				cronScheduler.SyncJobs(GetCronJobs())
			}

			// Update persistence
			if err := saveCronJobs(); err != nil {
				return fmt.Sprintf("Job %s deleted (persist failed: %v)", id, err), nil
			}

			return fmt.Sprintf("<cron_result>\n  <deleted>%s</deleted>\n</cron_result>", id), nil
		},
	}
}

// GetCronJobs returns all scheduled cron jobs.
func GetCronJobs() []*CronJob {
	cronMu.Lock()
	defer cronMu.Unlock()
	result := make([]*CronJob, len(cronJobs))
	copy(result, cronJobs)
	return result
}

// truncateCronPrompt truncates a string to max characters with ellipsis.
func truncateCronPrompt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func cronListTool() *ToolSpec {
	return &ToolSpec{
		Name:        "CronList",
		Permission:  PermReadOnly,
		Description: "List all scheduled cron jobs.",
		InputSchema: map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
			"properties":          map[string]interface{}{},
		},
		Handler: func(input map[string]interface{}) (string, error) {
			cronMu.Lock()
			defer cronMu.Unlock()

			if len(cronJobs) == 0 {
				return "No scheduled cron jobs.", nil
			}

			var buf strings.Builder
			buf.WriteString(fmt.Sprintf("Scheduled cron jobs (%d):\n", len(cronJobs)))
			for _, job := range cronJobs {
				status := "active"
				if !job.Recurring {
					status = "one-shot"
				}
				durable := ""
				if job.Durable {
					durable = " [durable]"
				}
				fmt.Fprintf(&buf, "  %s: %s (%s%s) - %s\n", job.ID, job.Cron, status, durable, truncateCronPrompt(job.Prompt, 60))
			}
			return buf.String(), nil
		},
	}
}

// saveCronJobs persists durable cron jobs to disk.
func saveCronJobs() error {
	cronMu.Lock()
	var durable []*CronJob
	for _, j := range cronJobs {
		if j.Durable {
			durable = append(durable, j)
		}
	}
	cronMu.Unlock()

	dir := ".claude"
	os.MkdirAll(dir, 0755)
	data, err := json.MarshalIndent(durable, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "scheduled_tasks.json"), data, 0644)
}
