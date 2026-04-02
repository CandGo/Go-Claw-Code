package runtime

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	compactionPreserveRecent = 4
	compactionMaxTokens      = 10000
	compactSummaryPrefix     = "[Compacted conversation summary]\n"
)

// CompactionConfig holds compaction parameters.
type CompactionConfig struct {
	PreserveRecent int `json:"preserve_recent"`
	MaxTokens      int `json:"max_estimated_tokens"`
}

// CompactionDefaults returns default compaction settings.
func CompactionDefaults() CompactionConfig {
	return CompactionConfig{
		PreserveRecent: compactionPreserveRecent,
		MaxTokens:      compactionMaxTokens,
	}
}

// ShouldCompact returns true if the session history should be compacted.
func (s *Session) ShouldCompact(cfg CompactionConfig) bool {
	msgs := s.compactableMessages(cfg)
	if len(msgs) <= cfg.PreserveRecent {
		return false
	}
	return estimateTokens(msgs) > cfg.MaxTokens
}

// Compact compacts the session history, keeping recent messages and summarizing older ones.
func (s *Session) Compact(cfg CompactionConfig) {
	msgs := s.compactableMessages(cfg)
	if len(msgs) <= cfg.PreserveRecent {
		return
	}

	// Find existing summary
	var existingSummary string
	splitIdx := 0
	if len(s.Messages) > 0 {
		first := s.Messages[0]
		if first.Role == MsgRoleUser && len(first.Content) > 0 {
			if tb, ok := first.Content[0].(*TextBlock); ok && strings.HasPrefix(tb.Text, compactSummaryPrefix) {
				existingSummary = strings.TrimPrefix(tb.Text, compactSummaryPrefix)
				splitIdx = 1
			}
		}
	}

	// Messages to compact: from splitIdx to len-recent
	recentStart := len(s.Messages) - cfg.PreserveRecent
	if recentStart < splitIdx {
		return
	}

	toCompact := s.Messages[splitIdx:recentStart]
	summary := summarizeMessagesRich(toCompact)

	if existingSummary != "" {
		summary = mergeSummaries(existingSummary, summary)
	}

	// Build new message list
	var newMsgs []ConversationMessage
	newMsgs = append(newMsgs, ConversationMessage{
		Role:    MsgRoleUser,
		Content: []ContentBlock{&TextBlock{Text: compactSummaryPrefix + summary}},
	})
	newMsgs = append(newMsgs, s.Messages[recentStart:]...)
	s.Messages = newMsgs
}

func (s *Session) compactableMessages(cfg CompactionConfig) []ConversationMessage {
	// Skip summary prefix if present
	start := 0
	if len(s.Messages) > 0 {
		first := s.Messages[0]
		if first.Role == MsgRoleUser && len(first.Content) > 0 {
			if tb, ok := first.Content[0].(*TextBlock); ok && strings.HasPrefix(tb.Text, compactSummaryPrefix) {
				start = 1
			}
		}
	}
	return s.Messages[start:]
}

func estimateTokens(msgs []ConversationMessage) int {
	total := 0
	for _, m := range msgs {
		for _, b := range m.Content {
			switch v := b.(type) {
			case *TextBlock:
				total += len(v.Text)/4 + 1
			case *ToolUseBlock:
				args := fmt.Sprintf("%v", v.Input)
				total += (len(v.Name) + len(v.ID) + len(args))/4 + 1
			case *ToolResultBlock:
				total += len(v.Content)/4 + 1
			}
		}
	}
	return total
}

// summarizeMessagesRich creates a high-quality summary with structured sections.
// Extracts: key files, tools used, user requests, pending work, errors encountered.
func summarizeMessagesRich(msgs []ConversationMessage) string {
	var buf strings.Builder

	var (
		userCount, assistantCount, toolCount int
		toolNameSet                          = make(map[string]bool)
		toolNames                            []string
		fileSet                              = make(map[string]bool)
		keyFiles                             []string
		lastUserTexts                        []string
		errors                               []string
		pendingWork                          []string
		lastAssistantText                    string
	)

	for _, m := range msgs {
		for _, b := range m.Content {
			switch v := b.(type) {
			case *TextBlock:
				if m.Role == MsgRoleUser {
					userCount++
					text := truncate(v.Text, 200)
					lastUserTexts = append(lastUserTexts, text)
					if len(lastUserTexts) > 5 {
						lastUserTexts = lastUserTexts[1:]
					}
					// Detect pending work indicators
					detectPendingWork(v.Text, &pendingWork)
				} else if m.Role == MsgRoleAssistant {
					assistantCount++
					lastAssistantText = truncate(v.Text, 300)
				}
				extractPaths(v.Text, fileSet, &keyFiles)

			case *ToolUseBlock:
				toolCount++
				if !toolNameSet[v.Name] {
					toolNameSet[v.Name] = true
					toolNames = append(toolNames, v.Name)
				}
				// Extract file paths from tool inputs
				if v.Name == "write_file" || v.Name == "edit_file" || v.Name == "read_file" {
					if path, ok := v.Input["path"].(string); ok && path != "" {
						if !fileSet[path] {
							fileSet[path] = true
							keyFiles = append(keyFiles, path)
						}
					}
				}

			case *ToolResultBlock:
				toolCount++
				if v.IsError {
					errors = append(errors, truncate(v.Content, 120))
					if len(errors) > 5 {
						errors = errors[1:]
					}
				}
				extractPaths(v.Content, fileSet, &keyFiles)
			}
		}
	}

	// Build structured summary
	fmt.Fprintf(&buf, "Conversation summary (%d user, %d assistant, %d tool messages):\n", userCount, assistantCount, toolCount)

	if len(toolNames) > 0 {
		fmt.Fprintf(&buf, "Tools used: %s\n", strings.Join(toolNames, ", "))
	}

	if len(keyFiles) > 0 {
		buf.WriteString("Key files referenced:\n")
		limit := 10
		if len(keyFiles) < limit {
			limit = len(keyFiles)
		}
		for _, f := range keyFiles[:limit] {
			fmt.Fprintf(&buf, "  - %s\n", f)
		}
	}

	if len(lastUserTexts) > 0 {
		buf.WriteString("Recent user requests:\n")
		for _, t := range lastUserTexts {
			fmt.Fprintf(&buf, "  - %s\n", t)
		}
	}

	if lastAssistantText != "" {
		fmt.Fprintf(&buf, "Last assistant response: %s\n", lastAssistantText)
	}

	if len(pendingWork) > 0 {
		buf.WriteString("Pending work:\n")
		for _, pw := range pendingWork {
			fmt.Fprintf(&buf, "  - %s\n", pw)
		}
	}

	if len(errors) > 0 {
		buf.WriteString("Errors encountered:\n")
		for _, e := range errors {
			fmt.Fprintf(&buf, "  - %s\n", e)
		}
	}

	return buf.String()
}

// detectPendingWork looks for indicators of incomplete tasks in user messages.
var pendingPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:todo|fix|implement|add|refactor|create|update|change|modify)\s+(.+)`),
	regexp.MustCompile(`(?i)(?:need to|should|must|have to)\s+(.+)`),
	regexp.MustCompile(`(?i)(?:please|can you|could you)\s+(.+)`),
}

func detectPendingWork(text string, pending *[]string) {
	for _, pat := range pendingPatterns {
		matches := pat.FindStringSubmatch(text)
		if len(matches) > 1 {
			item := truncate(strings.TrimSpace(matches[1]), 100)
			if item != "" {
				*pending = append(*pending, item)
			}
		}
	}
	// Cap at 5 items
	if len(*pending) > 5 {
		*pending = (*pending)[len(*pending)-5:]
	}
}

func mergeSummaries(old, new string) string {
	var buf strings.Builder
	buf.WriteString("Previously compacted context:\n")
	buf.WriteString(old)
	buf.WriteString("\n\nNewly compacted context:\n")
	buf.WriteString(new)
	return buf.String()
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// extractPaths finds file paths in text using heuristic matching.
var pathExtPattern = regexp.MustCompile(`[\w/.\-]+\.(go|rs|ts|tsx|js|jsx|py|java|rb|ex|php|cs|c|h|cpp|hpp|json|yaml|yml|toml|md|txt|sql|sh|bash|dockerfile|makefile|cmake|proto|graphql|vue|svelte|css|html|xml)`)

func extractPaths(text string, seen map[string]bool, paths *[]string) {
	matches := pathExtPattern.FindAllString(text, -1)
	for _, m := range matches {
		// Clean up surrounding punctuation
		m = strings.Trim(m, "()[]{}\"'`:,;")
		if !seen[m] && len(m) > 3 {
			seen[m] = true
			*paths = append(*paths, m)
		}
	}
}
