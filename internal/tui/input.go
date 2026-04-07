package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ReadOutcome mirrors Rust ReadOutcome.
type ReadOutcome int

const (
	OutcomeSubmit ReadOutcome = iota
	OutcomeCancel
	OutcomeExit
)

// EditorMode mirrors Rust EditorMode.
type EditorMode int

const (
	ModePlain EditorMode = iota
	ModeInsert
	ModeNormal
	ModeVisual
	ModeCommand
)

func (m EditorMode) Indicator(vimEnabled bool) string {
	if !vimEnabled {
		return ""
	}
	switch m {
	case ModePlain:
		return "PLAIN"
	case ModeInsert:
		return "INSERT"
	case ModeNormal:
		return "NORMAL"
	case ModeVisual:
		return "VISUAL"
	case ModeCommand:
		return "COMMAND"
	default:
		return ""
	}
}

// YankBuffer mirrors Rust YankBuffer.
type YankBuffer struct {
	Text     string
	Linewise bool
}

// EditSession mirrors Rust EditSession — holds the state of an editing session.
type EditSession struct {
	Text            string
	Cursor          int
	Mode            EditorMode
	PendingOperator *rune
	VisualAnchor    *int
	CommandBuffer   string
	CommandCursor   int
	HistoryIndex    *int
	HistoryBackup   *string
}

// NewEditSession creates a new edit session.
func NewEditSession(vimEnabled bool) *EditSession {
	mode := ModePlain
	if vimEnabled {
		mode = ModeInsert
	}
	return &EditSession{
		Mode: mode,
	}
}

// ActiveText returns the currently active text buffer.
func (s *EditSession) ActiveText() string {
	if s.Mode == ModeCommand {
		return s.CommandBuffer
	}
	return s.Text
}

// CurrentLen returns the length of the active text.
func (s *EditSession) CurrentLen() int {
	return len(s.ActiveText())
}

// HasInput returns whether there is any input.
func (s *EditSession) HasInput() bool {
	return len(s.ActiveText()) > 0
}

// CurrentLine returns the current line content.
func (s *EditSession) CurrentLine() string {
	return s.ActiveText()
}

// SetTextFromHistory sets the text from a history entry.
func (s *EditSession) SetTextFromHistory(entry string) {
	s.Text = entry
	s.Cursor = len(s.Text)
	s.PendingOperator = nil
	s.VisualAnchor = nil
	if s.Mode != ModePlain && s.Mode != ModeInsert {
		s.Mode = ModeNormal
	}
}

// EnterInsertMode transitions to insert mode.
func (s *EditSession) EnterInsertMode() {
	s.Mode = ModeInsert
	s.PendingOperator = nil
	s.VisualAnchor = nil
}

// EnterNormalMode transitions to normal mode.
func (s *EditSession) EnterNormalMode() {
	s.Mode = ModeNormal
	s.PendingOperator = nil
	s.VisualAnchor = nil
}

// EnterVisualMode transitions to visual mode.
func (s *EditSession) EnterVisualMode() {
	s.Mode = ModeVisual
	s.PendingOperator = nil
	anchor := s.Cursor
	s.VisualAnchor = &anchor
}

// EnterCommandMode transitions to command mode.
func (s *EditSession) EnterCommandMode() {
	s.Mode = ModeCommand
	s.PendingOperator = nil
	s.VisualAnchor = nil
	s.CommandBuffer = ":"
	s.CommandCursor = len(s.CommandBuffer)
}

// ExitCommandMode leaves command mode back to normal mode.
func (s *EditSession) ExitCommandMode() {
	s.CommandBuffer = ""
	s.CommandCursor = 0
	s.EnterNormalMode()
}

// VisibleBuffer returns the text for display (with selection highlighting in visual mode).
func (s *EditSession) VisibleBuffer() string {
	if s.Mode != ModeVisual || s.VisualAnchor == nil {
		return s.ActiveText()
	}
	start, end := selectionBounds(s.Text, *s.VisualAnchor, s.Cursor)
	return renderSelectedText(s.Text, start, end)
}

// Prompt returns the display prompt with mode indicator.
func (s *EditSession) Prompt(basePrompt string, vimEnabled bool) string {
	indicator := s.Mode.Indicator(vimEnabled)
	if indicator != "" {
		return fmt.Sprintf("[%s] %s", indicator, basePrompt)
	}
	return basePrompt
}

// CursorLayout returns (cursorRow, cursorCol, totalLines) for the active buffer.
func (s *EditSession) CursorLayout(prompt string) (int, int, int) {
	activeText := s.ActiveText()
	cursor := s.Cursor
	if s.Mode == ModeCommand {
		cursor = s.CommandCursor
	}

	cursorPrefix := activeText[:cursor]
	cursorRow := strings.Count(cursorPrefix, "\n")
	var cursorCol int
	if idx := strings.LastIndex(cursorPrefix, "\n"); idx >= 0 {
		cursorCol = utf8.RuneCountInString(cursorPrefix[idx+1:])
	} else {
		cursorCol = utf8.RuneCountInString(prompt) + utf8.RuneCountInString(cursorPrefix)
	}
	totalLines := strings.Count(activeText, "\n") + 1
	return cursorRow, cursorCol, totalLines
}

// KeyAction mirrors Rust KeyAction.
type KeyAction int

const (
	ActionContinue KeyAction = iota
	ActionSubmit
	ActionCancel
	ActionExit
	ActionToggleVim
)

// submission mirrors Rust Submission.
type submission int

const (
	submitSubmit submission = iota
	submitToggleVim
)

// LineEditor mirrors Rust LineEditor — the main input editor.
type LineEditor struct {
	Prompt      string
	Completions []string
	History     []string
	YankBuffer  YankBuffer
	VimEnabled  bool
}

// NewLineEditor creates a new line editor.
func NewLineEditor(prompt string, completions []string) *LineEditor {
	return &LineEditor{
		Prompt:      prompt,
		Completions: completions,
	}
}

// PushHistory adds an entry to the history.
func (e *LineEditor) PushHistory(entry string) {
	if strings.TrimSpace(entry) == "" {
		return
	}
	e.History = append(e.History, entry)
}

// HandleKeyEvent processes a key event and returns the action to take.
// Mirrors Rust LineEditor::handle_key_event.
func (e *LineEditor) HandleKeyEvent(session *EditSession, key Key) KeyAction {
	if key.Ctrl {
		switch key.Rune {
		case 'c', 'C':
			if session.HasInput() {
				return ActionCancel
			}
			return ActionExit
		case 'j', 'J':
			if session.Mode != ModeNormal && session.Mode != ModeVisual {
				e.insertActiveText(session, "\n")
			}
			return ActionContinue
		case 'd', 'D':
			if session.CurrentLen() == 0 {
				return ActionExit
			}
			e.deleteCharUnderCursor(session)
			return ActionContinue
		}
	}

	switch key.Code {
	case KeyEnter:
		if key.Shift {
			if session.Mode != ModeNormal && session.Mode != ModeVisual {
				e.insertActiveText(session, "\n")
			}
			return ActionContinue
		}
		return e.submitOrToggle(session)
	case KeyEsc:
		return e.handleEscape(session)
	case KeyBackspace:
		e.handleBackspace(session)
		return ActionContinue
	case KeyDelete:
		e.deleteCharUnderCursor(session)
		return ActionContinue
	case KeyLeft:
		e.moveLeft(session)
		return ActionContinue
	case KeyRight:
		e.moveRight(session)
		return ActionContinue
	case KeyUp:
		e.historyUp(session)
		return ActionContinue
	case KeyDown:
		e.historyDown(session)
		return ActionContinue
	case KeyHome:
		e.moveLineStart(session)
		return ActionContinue
	case KeyEnd:
		e.moveLineEnd(session)
		return ActionContinue
	case KeyTab:
		e.completeSlashCommand(session)
		return ActionContinue
	case KeyChar:
		e.handleChar(session, key.Rune)
		return ActionContinue
	}

	return ActionContinue
}

// Key represents a keyboard input event.
type Key struct {
	Code  KeyCode
	Rune  rune
	Ctrl  bool
	Shift bool
}

// KeyCode identifies the type of key.
type KeyCode int

const (
	KeyChar KeyCode = iota
	KeyEnter
	KeyEsc
	KeyBackspace
	KeyDelete
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyHome
	KeyEnd
	KeyTab
)

func (e *LineEditor) handleChar(session *EditSession, ch rune) {
	switch session.Mode {
	case ModePlain, ModeInsert, ModeCommand:
		e.insertActiveChar(session, ch)
	case ModeNormal:
		e.handleNormalChar(session, ch)
	case ModeVisual:
		e.handleVisualChar(session, ch)
	}
}

func (e *LineEditor) handleNormalChar(session *EditSession, ch rune) {
	if session.PendingOperator != nil {
		operator := *session.PendingOperator
		session.PendingOperator = nil
		switch {
		case operator == 'd' && ch == 'd':
			e.deleteCurrentLine(session)
			return
		case operator == 'y' && ch == 'y':
			e.yankCurrentLine(session)
			return
		}
	}

	switch ch {
	case 'h':
		e.moveLeft(session)
	case 'j':
		e.moveDown(session)
	case 'k':
		e.moveUp(session)
	case 'l':
		e.moveRight(session)
	case 'd', 'y':
		session.PendingOperator = &ch
	case 'p':
		e.pasteAfter(session)
	case 'i':
		session.EnterInsertMode()
	case 'v':
		session.EnterVisualMode()
	case ':':
		session.EnterCommandMode()
	}
}

func (e *LineEditor) handleVisualChar(session *EditSession, ch rune) {
	switch ch {
	case 'h':
		e.moveLeft(session)
	case 'j':
		e.moveDown(session)
	case 'k':
		e.moveUp(session)
	case 'l':
		e.moveRight(session)
	case 'v':
		session.EnterNormalMode()
	}
}

func (e *LineEditor) handleEscape(session *EditSession) KeyAction {
	switch session.Mode {
	case ModePlain:
		return ActionContinue
	case ModeInsert:
		if session.Cursor > 0 {
			session.Cursor = previousBoundary(session.Text, session.Cursor)
		}
		session.EnterNormalMode()
		return ActionContinue
	case ModeNormal:
		return ActionContinue
	case ModeVisual:
		session.EnterNormalMode()
		return ActionContinue
	case ModeCommand:
		session.ExitCommandMode()
		return ActionContinue
	default:
		return ActionContinue
	}
}

func (e *LineEditor) handleBackspace(session *EditSession) {
	switch session.Mode {
	case ModeNormal, ModeVisual:
		e.moveLeft(session)
	case ModeCommand:
		if session.CommandCursor <= 1 {
			session.ExitCommandMode()
		} else {
			removePreviousChar(&session.CommandBuffer, &session.CommandCursor)
		}
	case ModePlain, ModeInsert:
		removePreviousChar(&session.Text, &session.Cursor)
	}
}

func (e *LineEditor) submitOrToggle(session *EditSession) KeyAction {
	line := session.CurrentLine()
	switch e.handleSubmission(line) {
	case submitSubmit:
		return ActionSubmit
	case submitToggleVim:
		return ActionToggleVim
	}
	return ActionContinue
}

func (e *LineEditor) handleSubmission(line string) submission {
	if strings.TrimSpace(line) == "/vim" {
		return submitToggleVim
	}
	return submitSubmit
}

func (e *LineEditor) insertActiveChar(session *EditSession, ch rune) {
	var buf [4]byte
	n := utf8.EncodeRune(buf[:], ch)
	e.insertActiveText(session, string(buf[:n]))
}

func (e *LineEditor) insertActiveText(session *EditSession, text string) {
	if session.Mode == ModeCommand {
		session.CommandBuffer = session.CommandBuffer[:session.CommandCursor] + text + session.CommandBuffer[session.CommandCursor:]
		session.CommandCursor += len(text)
	} else {
		session.Text = session.Text[:session.Cursor] + text + session.Text[session.Cursor:]
		session.Cursor += len(text)
	}
}

func (e *LineEditor) moveLeft(session *EditSession) {
	if session.Mode == ModeCommand {
		session.CommandCursor = previousCommandBoundary(session.CommandBuffer, session.CommandCursor)
	} else {
		session.Cursor = previousBoundary(session.Text, session.Cursor)
	}
}

func (e *LineEditor) moveRight(session *EditSession) {
	if session.Mode == ModeCommand {
		session.CommandCursor = nextBoundary(session.CommandBuffer, session.CommandCursor)
	} else {
		session.Cursor = nextBoundary(session.Text, session.Cursor)
	}
}

func (e *LineEditor) moveLineStart(session *EditSession) {
	if session.Mode == ModeCommand {
		session.CommandCursor = 1
	} else {
		session.Cursor = lineStart(session.Text, session.Cursor)
	}
}

func (e *LineEditor) moveLineEnd(session *EditSession) {
	if session.Mode == ModeCommand {
		session.CommandCursor = len(session.CommandBuffer)
	} else {
		session.Cursor = lineEnd(session.Text, session.Cursor)
	}
}

func (e *LineEditor) moveUp(session *EditSession) {
	if session.Mode == ModeCommand {
		return
	}
	session.Cursor = moveVertical(session.Text, session.Cursor, -1)
}

func (e *LineEditor) moveDown(session *EditSession) {
	if session.Mode == ModeCommand {
		return
	}
	session.Cursor = moveVertical(session.Text, session.Cursor, 1)
}

func (e *LineEditor) deleteCharUnderCursor(session *EditSession) {
	switch session.Mode {
	case ModeCommand:
		if session.CommandCursor < len(session.CommandBuffer) {
			end := nextBoundary(session.CommandBuffer, session.CommandCursor)
			session.CommandBuffer = session.CommandBuffer[:session.CommandCursor] + session.CommandBuffer[end:]
		}
	default:
		if session.Cursor < len(session.Text) {
			end := nextBoundary(session.Text, session.Cursor)
			session.Text = session.Text[:session.Cursor] + session.Text[end:]
		}
	}
}

func (e *LineEditor) deleteCurrentLine(session *EditSession) {
	lineStartIdx, lineEndIdx, deleteStartIdx := currentLineDeleteRange(session.Text, session.Cursor)
	e.YankBuffer.Text = session.Text[lineStartIdx:lineEndIdx]
	e.YankBuffer.Linewise = true
	session.Text = session.Text[:deleteStartIdx] + session.Text[lineEndIdx:]
	if deleteStartIdx < len(session.Text) {
		session.Cursor = deleteStartIdx
	} else {
		session.Cursor = len(session.Text)
	}
}

func (e *LineEditor) yankCurrentLine(session *EditSession) {
	lineStartIdx, lineEndIdx, _ := currentLineDeleteRange(session.Text, session.Cursor)
	e.YankBuffer.Text = session.Text[lineStartIdx:lineEndIdx]
	e.YankBuffer.Linewise = true
}

func (e *LineEditor) pasteAfter(session *EditSession) {
	if e.YankBuffer.Text == "" {
		return
	}

	if e.YankBuffer.Linewise {
		lineEndIdx := lineEnd(session.Text, session.Cursor)
		insertAt := len(session.Text)
		if lineEndIdx < len(session.Text) {
			insertAt = lineEndIdx + 1
		}
		insertion := e.YankBuffer.Text
		if insertAt == len(session.Text) && len(session.Text) > 0 && session.Text[len(session.Text)-1] != '\n' {
			insertion = "\n" + insertion
		}
		if insertAt < len(session.Text) && insertion[len(insertion)-1] != '\n' {
			insertion = insertion + "\n"
		}
		session.Text = session.Text[:insertAt] + insertion + session.Text[insertAt:]
		if insertion[0] == '\n' {
			session.Cursor = insertAt + 1
		} else {
			session.Cursor = insertAt
		}
		return
	}

	insertAt := nextBoundary(session.Text, session.Cursor)
	session.Text = session.Text[:insertAt] + e.YankBuffer.Text + session.Text[insertAt:]
	session.Cursor = insertAt + len(e.YankBuffer.Text)
}

func (e *LineEditor) completeSlashCommand(session *EditSession) {
	if session.Mode == ModeCommand {
		return
	}
	prefix := slashCommandPrefix(session.Text, session.Cursor)
	if prefix == "" {
		return
	}
	for _, candidate := range e.Completions {
		if strings.HasPrefix(candidate, prefix) && candidate != prefix {
			session.Text = candidate + session.Text[session.Cursor:]
			session.Cursor = len(candidate)
			return
		}
	}
}

func (e *LineEditor) historyUp(session *EditSession) {
	if session.Mode == ModeCommand || len(e.History) == 0 {
		return
	}

	var nextIndex int
	if session.HistoryIndex != nil {
		nextIndex = *session.HistoryIndex - 1
		if nextIndex < 0 {
			nextIndex = 0
		}
	} else {
		backup := session.Text
		session.HistoryBackup = &backup
		nextIndex = len(e.History) - 1
	}

	session.HistoryIndex = &nextIndex
	session.SetTextFromHistory(e.History[nextIndex])
}

func (e *LineEditor) historyDown(session *EditSession) {
	if session.Mode == ModeCommand || session.HistoryIndex == nil {
		return
	}

	idx := *session.HistoryIndex
	if idx+1 < len(e.History) {
		nextIndex := idx + 1
		session.HistoryIndex = &nextIndex
		session.SetTextFromHistory(e.History[nextIndex])
		return
	}

	session.HistoryIndex = nil
	restored := ""
	if session.HistoryBackup != nil {
		restored = *session.HistoryBackup
		session.HistoryBackup = nil
	}
	session.SetTextFromHistory(restored)
	if e.VimEnabled {
		session.EnterInsertMode()
	} else {
		session.Mode = ModePlain
	}
}

// --- Helper functions (mirrors Rust free functions) ---

func previousBoundary(text string, cursor int) int {
	if cursor == 0 {
		return 0
	}
	_, size := utf8.DecodeLastRuneInString(text[:cursor])
	return cursor - size
}

func previousCommandBoundary(text string, cursor int) int {
	boundary := previousBoundary(text, cursor)
	if boundary < 1 {
		return 1
	}
	return boundary
}

func nextBoundary(text string, cursor int) int {
	if cursor >= len(text) {
		return len(text)
	}
	_, size := utf8.DecodeRuneInString(text[cursor:])
	return cursor + size
}

func removePreviousChar(text *string, cursor *int) {
	if *cursor == 0 {
		return
	}
	start := previousBoundary(*text, *cursor)
	*text = (*text)[:start] + (*text)[*cursor:]
	*cursor = start
}

func lineStart(text string, cursor int) int {
	idx := strings.LastIndex(text[:cursor], "\n")
	if idx >= 0 {
		return idx + 1
	}
	return 0
}

func lineEnd(text string, cursor int) int {
	idx := strings.Index(text[cursor:], "\n")
	if idx >= 0 {
		return cursor + idx
	}
	return len(text)
}

func moveVertical(text string, cursor int, delta int) int {
	starts := lineStarts(text)
	currentRow := strings.Count(text[:cursor], "\n")
	currentStart := starts[currentRow]
	currentCol := utf8.RuneCountInString(text[currentStart:cursor])

	maxRow := len(starts) - 1
	targetRow := currentRow + delta
	if targetRow < 0 {
		targetRow = 0
	}
	if targetRow > maxRow {
		targetRow = maxRow
	}
	if targetRow == currentRow {
		return cursor
	}

	targetStart := starts[targetRow]
	targetEnd := len(text)
	if targetRow+1 < len(starts) {
		targetEnd = starts[targetRow+1] - 1
	}
	return targetStart + byteIndexForCharColumn(text[targetStart:targetEnd], currentCol)
}

func lineStarts(text string) []int {
	starts := []int{0}
	for i, ch := range text {
		if ch == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

func byteIndexForCharColumn(text string, column int) int {
	current := 0
	for i := range text {
		if current == column {
			return i
		}
		current++
	}
	return len(text)
}

func currentLineDeleteRange(text string, cursor int) (lineStartIdx, lineEndIdx, deleteStartIdx int) {
	lineStartIdx = lineStart(text, cursor)
	lineEndCore := lineEnd(text, cursor)
	if lineEndCore < len(text) {
		lineEndIdx = lineEndCore + 1
	} else {
		lineEndIdx = lineEndCore
	}
	if lineEndIdx == len(text) && lineStartIdx > 0 {
		deleteStartIdx = lineStartIdx - 1
	} else {
		deleteStartIdx = lineStartIdx
	}
	return
}

func selectionBounds(text string, anchor, cursor int) (int, int) {
	if text == "" {
		return 0, 0
	}
	if cursor >= anchor {
		end := nextBoundary(text, cursor)
		a := anchor
		if a > len(text) {
			a = len(text)
		}
		if end > len(text) {
			end = len(text)
		}
		return a, end
	}
	end := nextBoundary(text, anchor)
	c := cursor
	if c > len(text) {
		c = len(text)
	}
	if end > len(text) {
		end = len(text)
	}
	return c, end
}

func renderSelectedText(text string, start, end int) string {
	var rendered strings.Builder
	inSelection := false

	for i, ch := range text {
		if !inSelection && i == start {
			rendered.WriteString("\x1b[7m")
			inSelection = true
		}
		if inSelection && i == end {
			rendered.WriteString("\x1b[0m")
			inSelection = false
		}
		rendered.WriteRune(ch)
	}

	if inSelection {
		rendered.WriteString("\x1b[0m")
	}

	return rendered.String()
}

func slashCommandPrefix(line string, pos int) string {
	if pos != len(line) {
		return ""
	}
	prefix := line[:pos]
	if strings.ContainsAny(prefix, " \t") || !strings.HasPrefix(prefix, "/") {
		return ""
	}
	return prefix
}
