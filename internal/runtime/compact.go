package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// globalLLMSummarizeFunc allows injecting an LLM-based summarization function.
// When set, CompactSession uses it instead of the heuristic summarizeMessages.
var globalLLMSummarizeFunc func(ctx context.Context, messages []ConversationMessage, existingSummary string) (string, error)

// SetLLMSummarizeFunc sets the LLM summarization function for compaction.
func SetLLMSummarizeFunc(fn func(ctx context.Context, messages []ConversationMessage, existingSummary string) (string, error)) {
	globalLLMSummarizeFunc = fn
}

const (
	compactContinuationPreamble     = "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n"
	compactRecentMessagesNote       = "Recent messages are preserved verbatim."
	compactDirectResumeInstruction  = "Continue the conversation from where it left off without asking the user any further questions. Resume directly — do not acknowledge the summary, do not recap what was happening, and do not preface with continuation text."
)

// CompactionConfig holds compaction parameters.
// Mirrors Rust CompactionConfig.
type CompactionConfig struct {
	PreserveRecentMessages int `json:"preserve_recent_messages"`
	MaxEstimatedTokens     int `json:"max_estimated_tokens"`
}

// DefaultCompactionConfig returns default compaction settings.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		PreserveRecentMessages: 4,
		MaxEstimatedTokens:     10_000,
	}
}

// CompactionResult holds the result of a compaction pass.
// Mirrors Rust CompactionResult.
type CompactionResult struct {
	Summary             string   `json:"summary"`
	FormattedSummary    string   `json:"formatted_summary"`
	CompactedSession    *Session `json:"-"`
	RemovedMessageCount int      `json:"removed_message_count"`
	// Legacy fields
	MessagesBefore int `json:"messages_before,omitempty"`
	MessagesAfter  int `json:"messages_after,omitempty"`
	TokensSaved    int `json:"tokens_saved,omitempty"`
}

// EstimateSessionTokens estimates the token count for all messages in a session.
func EstimateSessionTokens(session *Session) int {
	total := 0
	for _, m := range session.Messages {
		total += estimateMessageTokens(&m)
	}
	return total
}

// ShouldCompact determines if the session should be compacted.
// Mirrors Rust should_compact.
func ShouldCompact(session *Session, config CompactionConfig) bool {
	start := compactedSummaryPrefixLen(session)
	compactable := session.Messages[start:]
	if len(compactable) <= config.PreserveRecentMessages {
		return false
	}
	total := 0
	for _, m := range compactable {
		total += estimateMessageTokens(&m)
	}
	return total >= config.MaxEstimatedTokens
}

// ShouldCompactOnSession is a method version for backwards compatibility.
func (s *Session) ShouldCompact(cfg CompactionConfig) bool {
	return ShouldCompact(s, cfg)
}

// FormatCompactSummary formats a summary by stripping analysis tags.
// Mirrors Rust format_compact_summary.
func FormatCompactSummary(summary string) string {
	withoutAnalysis := stripTagBlock(summary, "analysis")
	formatted := withoutAnalysis
	if content := extractTagBlock(withoutAnalysis, "summary"); content != "" {
		formatted = strings.Replace(withoutAnalysis,
			"<summary>"+content+"</summary>",
			"Summary:\n"+strings.TrimSpace(content),
			1,
		)
	}
	return strings.TrimSpace(collapseBlankLines(formatted))
}

// GetCompactContinuationMessage builds the system message for a compacted session.
// Mirrors Rust get_compact_continuation_message.
func GetCompactContinuationMessage(summary string, suppressFollowUp bool, recentPreserved bool) string {
	base := compactContinuationPreamble + FormatCompactSummary(summary)

	if recentPreserved {
		base += "\n\n" + compactRecentMessagesNote
	}

	if suppressFollowUp {
		base += "\n" + compactDirectResumeInstruction
	}

	return base
}

// CompactSession compacts a session by summarizing older messages.
// Mirrors Rust compact_session.
func CompactSession(session *Session, config CompactionConfig) CompactionResult {
	if !ShouldCompact(session, config) {
		return CompactionResult{
			CompactedSession:    session,
			RemovedMessageCount: 0,
		}
	}

	existingSummary := ""
	if len(session.Messages) > 0 {
		existingSummary = extractExistingCompactedSummary(&session.Messages[0])
	}
	compactedPrefixLen := 0
	if existingSummary != "" {
		compactedPrefixLen = 1
	}

	keepFrom := len(session.Messages) - config.PreserveRecentMessages
	if keepFrom < compactedPrefixLen {
		keepFrom = compactedPrefixLen
	}

	removed := session.Messages[compactedPrefixLen:keepFrom]
	preserved := session.Messages[keepFrom:]

	// Use LLM summarization if available, fallback to heuristic
	var newSummary string
	if globalLLMSummarizeFunc != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		llmSummary, err := globalLLMSummarizeFunc(ctx, removed, existingSummary)
		if err == nil && llmSummary != "" {
			newSummary = llmSummary
		} else {
			// Fallback to heuristic
			newSummary = summarizeMessages(removed)
		}
	} else {
		newSummary = summarizeMessages(removed)
	}

	summary := mergeCompactSummaries(existingSummary, newSummary)
	formattedSummary := FormatCompactSummary(summary)
	continuation := GetCompactContinuationMessage(summary, true, len(preserved) > 0)

	compactedMessages := []ConversationMessage{
		{
			Role:    MsgRoleSystem,
			Content: []ContentBlock{&TextBlock{Text: continuation}},
		},
	}
	compactedMessages = append(compactedMessages, preserved...)

	tokensBefore := EstimateSessionTokens(session)
	result := CompactionResult{
		Summary:             summary,
		FormattedSummary:    formattedSummary,
		RemovedMessageCount: len(removed),
		MessagesBefore:      len(session.Messages),
		MessagesAfter:       len(compactedMessages),
		TokensSaved:         tokensBefore - EstimateSessionTokens(&Session{Messages: compactedMessages}),
	}

	result.CompactedSession = &Session{
		Version:  session.Version,
		Messages: compactedMessages,
		Info:     session.Info,
	}

	return result
}

// Compact is a method version for backwards compatibility.
// It compacts the session in-place and returns the result.
func (s *Session) Compact(cfg CompactionConfig) CompactionResult {
	result := CompactSession(s, cfg)
	if result.CompactedSession != nil {
		s.Messages = result.CompactedSession.Messages
		s.Version = result.CompactedSession.Version
		s.Info = result.CompactedSession.Info
	}
	return result
}

// Internal functions

func compactedSummaryPrefixLen(session *Session) int {
	if len(session.Messages) > 0 {
		if extractExistingCompactedSummary(&session.Messages[0]) != "" {
			return 1
		}
	}
	return 0
}

func summarizeMessages(messages []ConversationMessage) string {
	userCount := 0
	assistantCount := 0
	toolCount := 0
	toolNameSet := make(map[string]bool)
	var toolNames []string

	for _, m := range messages {
		switch m.Role {
		case MsgRoleUser:
			userCount++
		case MsgRoleAssistant:
			assistantCount++
		case MsgRoleTool:
			toolCount++
		}
		for _, b := range m.Content {
			switch v := b.(type) {
			case *ToolUseBlock:
				if !toolNameSet[v.Name] {
					toolNameSet[v.Name] = true
					toolNames = append(toolNames, v.Name)
				}
			case *ToolResultBlock:
				if v.ToolName != "" && !toolNameSet[v.ToolName] {
					toolNameSet[v.ToolName] = true
					toolNames = append(toolNames, v.ToolName)
				}
			}
		}
	}

	lines := []string{
		"<summary>",
		"Conversation summary:",
		fmt.Sprintf("- Scope: %d earlier messages compacted (user=%d, assistant=%d, tool=%d).",
			len(messages), userCount, assistantCount, toolCount),
	}

	sort.Strings(toolNames)
	if len(toolNames) > 0 {
		lines = append(lines, fmt.Sprintf("- Tools mentioned: %s.", strings.Join(toolNames, ", ")))
	}

	recentUserRequests := collectRecentRoleSummaries(messages, MsgRoleUser, 3)
	if len(recentUserRequests) > 0 {
		lines = append(lines, "- Recent user requests:")
		for _, r := range recentUserRequests {
			lines = append(lines, "  - "+r)
		}
	}

	pendingWork := inferPendingWork(messages)
	if len(pendingWork) > 0 {
		lines = append(lines, "- Pending work:")
		for _, item := range pendingWork {
			lines = append(lines, "  - "+item)
		}
	}

	keyFiles := collectKeyFiles(messages)
	if len(keyFiles) > 0 {
		lines = append(lines, fmt.Sprintf("- Key files referenced: %s.", strings.Join(keyFiles, ", ")))
	}

	if currentWork := inferCurrentWork(messages); currentWork != "" {
		lines = append(lines, "- Current work: "+currentWork)
	}

	lines = append(lines, "- Key timeline:")
	for _, m := range messages {
		role := string(m.Role)
		var blockSummaries []string
		for _, b := range m.Content {
			blockSummaries = append(blockSummaries, summarizeBlock(b))
		}
		content := strings.Join(blockSummaries, " | ")
		lines = append(lines, fmt.Sprintf("  - %s: %s", role, content))
	}
	lines = append(lines, "</summary>")

	return strings.Join(lines, "\n")
}

func mergeCompactSummaries(existingSummary, newSummary string) string {
	if existingSummary == "" {
		return newSummary
	}

	previousHighlights := extractSummaryHighlights(existingSummary)
	newFormattedSummary := FormatCompactSummary(newSummary)
	newHighlights := extractSummaryHighlights(newFormattedSummary)
	newTimeline := extractSummaryTimeline(newFormattedSummary)

	lines := []string{"<summary>", "Conversation summary:"}

	if len(previousHighlights) > 0 {
		lines = append(lines, "- Previously compacted context:")
		for _, line := range previousHighlights {
			lines = append(lines, "  "+line)
		}
	}

	if len(newHighlights) > 0 {
		lines = append(lines, "- Newly compacted context:")
		for _, line := range newHighlights {
			lines = append(lines, "  "+line)
		}
	}

	if len(newTimeline) > 0 {
		lines = append(lines, "- Key timeline:")
		for _, line := range newTimeline {
			lines = append(lines, "  "+line)
		}
	}

	lines = append(lines, "</summary>")
	return strings.Join(lines, "\n")
}

func summarizeBlock(block ContentBlock) string {
	switch v := block.(type) {
	case *TextBlock:
		return truncateSummary(v.Text, 160)
	case *ToolUseBlock:
		return truncateSummary(fmt.Sprintf("tool_use %s(%s)", v.Name, marshalBlockInput(v.Input)), 160)
	case *ToolResultBlock:
		prefix := ""
		if v.IsError {
			prefix = "error "
		}
		return truncateSummary(fmt.Sprintf("tool_result %s: %s%s", v.ToolName, prefix, v.Content), 160)
	case *ImageBlock:
		return fmt.Sprintf("[image: %s]", v.Source.MediaType)
	}
	return ""
}

func collectRecentRoleSummaries(messages []ConversationMessage, role MessageRole, limit int) []string {
	var results []string
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != role {
			continue
		}
		text := firstTextBlock(m)
		if text == "" {
			continue
		}
		results = append(results, truncateSummary(text, 160))
		if len(results) >= limit {
			break
		}
	}
	// Reverse to get chronological order
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results
}

func inferPendingWork(messages []ConversationMessage) []string {
	var results []string
	keywords := []string{"todo", "next", "pending", "follow up", "remaining"}
	for i := len(messages) - 1; i >= 0; i-- {
		text := firstTextBlock(messages[i])
		if text == "" {
			continue
		}
		lowered := strings.ToLower(text)
		matched := false
		for _, kw := range keywords {
			if strings.Contains(lowered, kw) {
				matched = true
				break
			}
		}
		if matched {
			results = append(results, truncateSummary(text, 160))
			if len(results) >= 3 {
				break
			}
		}
	}
	// Reverse
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results
}

func collectKeyFiles(messages []ConversationMessage) []string {
	fileSet := make(map[string]bool)
	var files []string

	for _, m := range messages {
		for _, b := range m.Content {
			var content string
			switch v := b.(type) {
			case *TextBlock:
				content = v.Text
			case *ToolUseBlock:
				content = marshalBlockInput(v.Input)
			case *ToolResultBlock:
				content = v.Content
			case *ImageBlock:
				// no text content to extract from images
			}
			for _, f := range extractFileCandidates(content) {
				if !fileSet[f] {
					fileSet[f] = true
					files = append(files, f)
				}
			}
		}
	}

	sort.Strings(files)
	if len(files) > 8 {
		files = files[:8]
	}
	return files
}

func inferCurrentWork(messages []ConversationMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		text := firstTextBlock(messages[i])
		if text != "" && strings.TrimSpace(text) != "" {
			return truncateSummary(text, 200)
		}
	}
	return ""
}

func firstTextBlock(msg ConversationMessage) string {
	for _, b := range msg.Content {
		if tb, ok := b.(*TextBlock); ok && strings.TrimSpace(tb.Text) != "" {
			return tb.Text
		}
	}
	return ""
}

func hasInterestingExtension(candidate string) bool {
	ext := strings.ToLower(filepath.Ext(candidate))
	interesting := map[string]bool{
		".go": true, ".rs": true, ".ts": true, ".tsx": true,
		".js": true, ".jsx": true, ".json": true, ".md": true,
		".py": true, ".java": true, ".rb": true, ".yaml": true,
		".yml": true, ".toml": true, ".sql": true, ".sh": true,
		".css": true, ".html": true, ".proto": true,
	}
	return interesting[ext]
}

func extractFileCandidates(content string) []string {
	var files []string
	for _, token := range strings.Fields(content) {
		candidate := strings.Trim(token, ",.:;)(\"'`")
		if strings.Contains(candidate, "/") && hasInterestingExtension(candidate) {
			files = append(files, candidate)
		}
	}
	return files
}

func truncateSummary(content string, maxChars int) string {
	if utf8.RuneCountInString(content) <= maxChars {
		return content
	}
	runes := []rune(content)
	return string(runes[:maxChars]) + "…"
}

func estimateMessageTokens(msg *ConversationMessage) int {
	total := 0
	for _, b := range msg.Content {
		switch v := b.(type) {
		case *TextBlock:
			total += len(v.Text)/4 + 1
		case *ToolUseBlock:
			total += (len(v.Name) + len(marshalBlockInput(v.Input))) / 4
		case *ToolResultBlock:
			total += (len(v.ToolName) + len(v.Content))/4 + 1
		case *ImageBlock:
			// Images consume tokens proportional to their base64 data size
			total += len(v.Source.Data)/4 + 100
		}
	}
	return total
}

func extractTagBlock(content, tag string) string {
	start := "<" + tag + ">"
	end := "</" + tag + ">"
	startIdx := strings.Index(content, start)
	if startIdx < 0 {
		return ""
	}
	startIdx += len(start)
	endIdx := strings.Index(content[startIdx:], end)
	if endIdx < 0 {
		return ""
	}
	return content[startIdx : startIdx+endIdx]
}

func stripTagBlock(content, tag string) string {
	start := "<" + tag + ">"
	end := "</" + tag + ">"
	startIdx := strings.Index(content, start)
	endIdx := strings.Index(content, end)
	if startIdx < 0 || endIdx < 0 {
		return content
	}
	return content[:startIdx] + content[endIdx+len(end):]
}

func collapseBlankLines(content string) string {
	var result strings.Builder
	lastBlank := false
	for _, line := range strings.Split(content, "\n") {
		isBlank := strings.TrimSpace(line) == ""
		if isBlank && lastBlank {
			continue
		}
		result.WriteString(line)
		result.WriteByte('\n')
		lastBlank = isBlank
	}
	return result.String()
}

func extractExistingCompactedSummary(msg *ConversationMessage) string {
	if msg.Role != MsgRoleSystem {
		return ""
	}
	text := firstTextBlock(*msg)
	if text == "" {
		return ""
	}
	if !strings.HasPrefix(text, compactContinuationPreamble) {
		return ""
	}
	summary := strings.TrimPrefix(text, compactContinuationPreamble)
	if idx := strings.Index(summary, "\n\n"+compactRecentMessagesNote); idx >= 0 {
		summary = summary[:idx]
	}
	if idx := strings.Index(summary, "\n"+compactDirectResumeInstruction); idx >= 0 {
		summary = summary[:idx]
	}
	return strings.TrimSpace(summary)
}

func extractSummaryHighlights(summary string) []string {
	var lines []string
	inTimeline := false

	for _, line := range strings.Split(FormatCompactSummary(summary), "\n") {
		trimmed := strings.TrimRight(line, " ")
		if trimmed == "" || trimmed == "Summary:" || trimmed == "Conversation summary:" {
			continue
		}
		if trimmed == "- Key timeline:" {
			inTimeline = true
			continue
		}
		if inTimeline {
			continue
		}
		lines = append(lines, trimmed)
	}
	return lines
}

func extractSummaryTimeline(summary string) []string {
	var lines []string
	inTimeline := false

	for _, line := range strings.Split(FormatCompactSummary(summary), "\n") {
		trimmed := strings.TrimRight(line, " ")
		if trimmed == "- Key timeline:" {
			inTimeline = true
			continue
		}
		if !inTimeline {
			continue
		}
		if trimmed == "" {
			break
		}
		lines = append(lines, trimmed)
	}
	return lines
}

// truncateUnicode truncates a string to at most maxRunes unicode code points.
// If truncation occurs, "..." is appended.
func truncateUnicode(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// Backwards-compatible aliases
var _ = sort.Strings
