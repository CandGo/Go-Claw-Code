package runtime

import (
	"fmt"
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
	summary := summarizeMessages(toCompact)

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
				args, _ := fmt.Sprintf("%v", v.Input), ""
				total += (len(v.Name)+len(v.ID)+len(args))/4 + 1
			case *ToolResultBlock:
				total += len(v.Content)/4 + 1
			}
		}
	}
	return total
}

func summarizeMessages(msgs []ConversationMessage) string {
	var buf strings.Builder
	userCount, assistantCount, toolCount := 0, 0, 0
	var toolNames []string
	toolNameSet := make(map[string]bool)
	var lastUserTexts []string
	var keyFiles []string
	fileSet := make(map[string]bool)

	for _, m := range msgs {
		for _, b := range m.Content {
			switch v := b.(type) {
			case *TextBlock:
				if m.Role == MsgRoleUser {
					userCount++
					lastUserTexts = append(lastUserTexts, truncate(v.Text, 160))
					if len(lastUserTexts) > 3 {
						lastUserTexts = lastUserTexts[1:]
					}
				} else if m.Role == MsgRoleAssistant {
					assistantCount++
				}
				extractPaths(v.Text, fileSet, &keyFiles)
			case *ToolUseBlock:
				toolCount++
				if !toolNameSet[v.Name] {
					toolNameSet[v.Name] = true
					toolNames = append(toolNames, v.Name)
				}
			case *ToolResultBlock:
				toolCount++
			}
		}
	}

	fmt.Fprintf(&buf, "Conversation summary (%d user, %d assistant, %d tool messages):\n", userCount, assistantCount, toolCount)

	if len(toolNames) > 0 {
		fmt.Fprintf(&buf, "Tools used: %s\n", strings.Join(toolNames, ", "))
	}

	if len(lastUserTexts) > 0 {
		buf.WriteString("Recent user requests:\n")
		for _, t := range lastUserTexts {
			fmt.Fprintf(&buf, "  - %s\n", t)
		}
	}

	if len(keyFiles) > 0 {
		buf.WriteString("Key files referenced:\n")
		for i, f := range keyFiles {
			if i >= 8 {
				break
			}
			fmt.Fprintf(&buf, "  - %s\n", f)
		}
	}

	return buf.String()
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

func extractPaths(text string, seen map[string]bool, paths *[]string) {
	// Simple heuristic: look for tokens with / and interesting extensions
	words := strings.Fields(text)
	for _, w := range words {
		w = strings.Trim(w, "()[]{}\"'`:,;")
		if strings.Contains(w, "/") && !seen[w] {
			for _, ext := range []string{".go", ".rs", ".ts", ".js", ".py", ".json", ".yaml", ".yml", ".toml", ".md"} {
				if strings.HasSuffix(w, ext) {
					seen[w] = true
					*paths = append(*paths, w)
					break
				}
			}
		}
	}
}
