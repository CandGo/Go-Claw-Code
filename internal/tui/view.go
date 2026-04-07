package tui

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	chromaFormatters "github.com/alecthomas/chroma/v2/formatters"
	chromaLexers "github.com/alecthomas/chroma/v2/lexers"
	chromaStyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

// --- ColorTheme ---

// ColorTheme mirrors Rust ColorTheme with 11 named color fields.
type ColorTheme struct {
	Heading         lipgloss.Color
	Emphasis        lipgloss.Color
	Strong          lipgloss.Color
	InlineCode      lipgloss.Color
	Link            lipgloss.Color
	Quote           lipgloss.Color
	TableBorder     lipgloss.Color
	CodeBlockBorder lipgloss.Color
	SpinnerActive   lipgloss.Color
	SpinnerDone     lipgloss.Color
	SpinnerFailed   lipgloss.Color
}

// DefaultColorTheme returns the default theme matching Rust's base16-ocean.dark palette.
func DefaultColorTheme() ColorTheme {
	return ColorTheme{
		Heading:         lipgloss.Color("36"),   // Cyan
		Emphasis:        lipgloss.Color("201"),  // Magenta
		Strong:          lipgloss.Color("220"),  // Yellow
		InlineCode:      lipgloss.Color("82"),   // Green
		Link:            lipgloss.Color("39"),   // Blue
		Quote:           lipgloss.Color("243"),  // DarkGrey
		TableBorder:     lipgloss.Color("36"),   // DarkCyan
		CodeBlockBorder: lipgloss.Color("243"),  // DarkGrey
		SpinnerActive:   lipgloss.Color("39"),   // Blue
		SpinnerDone:     lipgloss.Color("82"),   // Green
		SpinnerFailed:   lipgloss.Color("196"),  // Red
	}
}

// --- Spinner ---

// Spinner mirrors Rust Spinner with braille animation frames.
type Spinner struct {
	frameIndex int
}

// NewSpinner creates a new Spinner.
func NewSpinner() Spinner {
	return Spinner{}
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Tick renders one frame of the spinner animation to the writer.
// Mirrors Rust Spinner::tick.
func (s *Spinner) Tick(label string, theme ColorTheme, out io.Writer) {
	frame := spinnerFrames[s.frameIndex%len(spinnerFrames)]
	s.frameIndex++
	style := lipgloss.NewStyle().Foreground(theme.SpinnerActive)
	fmt.Fprint(out, "\r\033[K"+style.Render(frame+" "+label)+"\r")
}

// Finish renders a completion checkmark.
// Mirrors Rust Spinner::finish.
func (s *Spinner) Finish(label string, theme ColorTheme, out io.Writer) {
	s.frameIndex = 0
	style := lipgloss.NewStyle().Foreground(theme.SpinnerDone)
	fmt.Fprint(out, "\r\033[K"+style.Render("✔ "+label)+"\n")
}

// Fail renders a failure cross.
// Mirrors Rust Spinner::fail.
func (s *Spinner) Fail(label string, theme ColorTheme, out io.Writer) {
	s.frameIndex = 0
	style := lipgloss.NewStyle().Foreground(theme.SpinnerFailed)
	fmt.Fprint(out, "\r\033[K"+style.Render("✘ "+label)+"\n")
}

// --- TerminalRenderer ---

// TerminalRenderer mirrors Rust TerminalRenderer — encapsulates theme and rendering.
type TerminalRenderer struct {
	colorTheme ColorTheme
}

// NewTerminalRenderer creates a new renderer with the default theme.
func NewTerminalRenderer() *TerminalRenderer {
	return &TerminalRenderer{colorTheme: DefaultColorTheme()}
}

// ColorTheme returns the renderer's color theme.
func (r *TerminalRenderer) ColorTheme() ColorTheme {
	return r.colorTheme
}

// MarkdownToAlias renders markdown to ANSI-colored terminal output.
// Mirrors Rust TerminalRenderer::markdown_to_ansi.
func (r *TerminalRenderer) MarkdownToAnsi(markdown string) string {
	return r.RenderMarkdown(markdown)
}

// RenderMarkdown renders full markdown to ANSI-colored terminal output.
// Mirrors Rust TerminalRenderer::render_markdown.
func (r *TerminalRenderer) RenderMarkdown(markdown string) string {
	var output strings.Builder
	var state renderState
	inCodeBlock := false
	var codeLang, codeBuf string

	lines := strings.Split(markdown, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if strings.HasPrefix(strings.TrimSpace(line), "```") || strings.HasPrefix(strings.TrimSpace(line), "~~~") {
			trimmed := strings.TrimSpace(line)
			fence := "```"
			if strings.HasPrefix(trimmed, "~~~") {
				fence = "~~~"
			}
			if inCodeBlock {
				// End code block
				r.finishCodeBlock(&codeBuf, &codeLang, &output)
				inCodeBlock = false
				codeLang = ""
				codeBuf = ""
			} else {
				// Start code block
				inCodeBlock = true
				codeLang = strings.TrimPrefix(trimmed, fence)
				codeLang = strings.TrimSpace(codeLang)
				if codeLang == "" {
					codeLang = "code"
				}
				codeBuf = ""
				r.startCodeBlock(codeLang, &output)
			}
			continue
		}

		if inCodeBlock {
			if codeBuf != "" {
				codeBuf += "\n"
			}
			codeBuf += line
			continue
		}

		// Table detection and rendering
		if isTableRow(line) {
			// Collect consecutive table rows
			var tableRows []string
			for i < len(lines) && isTableRow(lines[i]) {
				tableRows = append(tableRows, lines[i])
				i++
			}
			i-- // back up one since loop will increment
			r.renderTable(tableRows, &output)
			output.WriteString("\n")
			continue
		}

		// Handle markdown line-by-line
		r.renderLine(line, &state, &output)
	}

	// Close unclosed code block
	if inCodeBlock {
		r.finishCodeBlock(&codeBuf, &codeLang, &output)
	}

	return strings.TrimRight(output.String(), " \t\n")
}

// StreamMarkdown writes rendered markdown to a writer.
// Mirrors Rust TerminalRenderer::stream_markdown.
func (r *TerminalRenderer) StreamMarkdown(markdown string, out io.Writer) {
	rendered := r.MarkdownToAnsi(markdown)
	fmt.Fprint(out, rendered)
	if !strings.HasSuffix(rendered, "\n") {
		fmt.Fprintln(out)
	}
}

// --- Internal rendering state ---

type renderState struct {
	emphasis    int
	strong      int
	heading     int
	quote       int
	listStack   []listKind
	orderedIdx  int
}

type listKind int

const (
	listUnordered listKind = iota
	listOrdered
)

// --- Internal rendering helpers ---

func (r *TerminalRenderer) renderLine(line string, state *renderState, output *strings.Builder) {
	trimmed := strings.TrimSpace(line)

	// Empty line
	if trimmed == "" {
		output.WriteString("\n")
		return
	}

	// Horizontal rule
	if isHRule(trimmed) {
		output.WriteString(lipgloss.NewStyle().Foreground(r.colorTheme.Quote).Render(strings.Repeat("─", 40)))
		output.WriteString("\n")
		return
	}

	// Heading
	if strings.HasPrefix(trimmed, "#") {
		level := 0
		for _, ch := range trimmed {
			if ch == '#' {
				level++
			} else {
				break
			}
		}
		state.heading = level
		text := strings.TrimSpace(trimmed[level:])
		text = r.renderInline(text)
		style := r.headingStyle(level)
		output.WriteString(style.Render(text))
		output.WriteString("\n\n")
		state.heading = 0
		return
	}

	// Blockquote (supports nesting with >)
	if strings.HasPrefix(trimmed, ">") {
		depth := 0
		rest := trimmed
		for strings.HasPrefix(rest, ">") {
			depth++
			rest = strings.TrimPrefix(rest, ">")
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, ">") {
				continue
			}
			break
		}
		state.quote = depth
		prefix := lipgloss.NewStyle().Foreground(r.colorTheme.Quote).Render(strings.Repeat("│ ", depth))
		text := r.renderInline(rest)
		output.WriteString(prefix + text)
		output.WriteString("\n")
		state.quote = 0
		return
	}

	// Unordered list
	if isUnorderedList(trimmed) {
		depth := countListIndent(line)
		marker := "• "
		prefix := strings.Repeat("  ", depth)
		text := r.renderInline(extractListItem(trimmed))
		output.WriteString(prefix + marker + text + "\n")
		return
	}

	// Ordered list
	if isOrderedList(trimmed) {
		depth := countListIndent(line)
		idx, content := extractOrderedListItem(trimmed)
		marker := fmt.Sprintf("%d. ", idx)
		prefix := strings.Repeat("  ", depth)
		text := r.renderInline(content)
		output.WriteString(prefix + marker + text + "\n")
		return
	}

	// Task list
	if isTaskList(trimmed) {
		checked := strings.Contains(trimmed, "[x]") || strings.Contains(trimmed, "[X]")
		box := "[ ] "
		if checked {
			box = "[x] "
		}
		text := r.renderInline(extractTaskContent(trimmed))
		output.WriteString("  " + box + text + "\n")
		return
	}

	// Regular text
	text := r.renderInline(line)
	output.WriteString(text + "\n")
}

func (r *TerminalRenderer) headingStyle(level int) lipgloss.Style {
	switch level {
	case 1:
		return lipgloss.NewStyle().Bold(true).Foreground(r.colorTheme.Heading)
	case 2:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255")) // White
	case 3:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39")) // Blue
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("249")) // Grey
	}
}

func (r *TerminalRenderer) renderInline(s string) string {
	// Bold: **text**
	s = boldPattern.ReplaceAllStringFunc(s, func(match string) string {
		inner := boldPattern.FindStringSubmatch(match)[1]
		return lipgloss.NewStyle().Bold(true).Foreground(r.colorTheme.Strong).Render(inner)
	})

	// Italic: *text* (single asterisks, not inside bold)
	s = italicPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := italicPattern.FindStringSubmatch(match)
		if len(parts) >= 3 {
			return parts[1] + lipgloss.NewStyle().Italic(true).Foreground(r.colorTheme.Emphasis).Render(parts[2]) + parts[3]
		}
		return match
	})

	// Links: [text](url)
	s = linkPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := linkPattern.FindStringSubmatch(match)
		if len(parts) >= 3 {
			return lipgloss.NewStyle().Foreground(r.colorTheme.Link).Underline(true).Render("[" + parts[1] + "](" + parts[2] + ")")
		}
		return match
	})

	// Inline code: `text`
	s = inlineCodePattern.ReplaceAllStringFunc(s, func(match string) string {
		inner := inlineCodePattern.FindStringSubmatch(match)
		if len(inner) >= 2 {
			return lipgloss.NewStyle().Foreground(r.colorTheme.InlineCode).Render("`" + inner[1] + "`")
		}
		return match
	})

	// Images: ![alt](url)
	s = imagePattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := imagePattern.FindStringSubmatch(match)
		if len(parts) >= 3 {
			return lipgloss.NewStyle().Foreground(r.colorTheme.Link).Render("[image:" + parts[2] + "]")
		}
		return match
	})

	return s
}

func (r *TerminalRenderer) startCodeBlock(lang string, output *strings.Builder) {
	style := lipgloss.NewStyle().Bold(true).Foreground(r.colorTheme.CodeBlockBorder)
	output.WriteString(style.Render("╭─ " + lang))
	output.WriteString("\n")
}

func (r *TerminalRenderer) finishCodeBlock(codeBuf *string, codeLang *string, output *strings.Builder) {
	// Render code with background
	highlighted := r.highlightCode(*codeBuf, *codeLang)
	output.WriteString(highlighted)
	borderStyle := lipgloss.NewStyle().Bold(true).Foreground(r.colorTheme.CodeBlockBorder)
	output.WriteString(borderStyle.Render("╰─"))
	output.WriteString("\n\n")
}

// highlightCode applies syntax highlighting using chroma with ANSI escape codes.
// Uses the "monokai" style and "terminal256" formatter for good terminal compatibility.
// Falls back to plain text for unrecognized languages.
func (r *TerminalRenderer) highlightCode(code, language string) string {
	// Try to find a lexer for the given language; fall back to plaintext.
	lexer := chromaLexers.Get(language)
	if lexer == nil {
		lexer = chromaLexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	// Use monokai style and terminal256 formatter.
	style := chromaStyles.Get("monokai")
	formatter := chromaFormatters.Get("terminal256")

	// Tokenize and format.
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		// On error, fall back to plain rendering with background.
		return r.plainCodeBlock(code)
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return r.plainCodeBlock(code)
	}

	highlighted := buf.String()

	// Apply background color to each line so the code block looks consistent.
	var lines []string
	for _, line := range strings.Split(highlighted, "\n") {
		lines = append(lines, applyCodeBackground(line))
	}

	result := strings.Join(lines, "\n")
	// Remove any trailing blank line from the split, since we add a newline in finishCodeBlock.
	result = strings.TrimRight(result, "\n")
	return result + "\n"
}

// plainCodeBlock renders code without syntax highlighting, just with background color.
func (r *TerminalRenderer) plainCodeBlock(code string) string {
	var lines []string
	for _, line := range strings.Split(code, "\n") {
		lines = append(lines, applyCodeBackground(line))
	}
	return strings.Join(lines, "\n") + "\n"
}

// applyCodeBackground applies xterm-236 background color to a line.
// Mirrors Rust apply_code_block_background.
func applyCodeBackground(line string) string {
	bg := "\x1b[48;5;236m"
	reset := "\x1b[0m"
	// Replace existing resets with background resets
	withBg := strings.ReplaceAll(line, reset, "\x1b[0;48;5;236m")
	return bg + withBg + reset
}

// --- Table rendering ---

// isTableRow checks if a line looks like a markdown table row.
func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "|") && strings.HasSuffix(trimmed, "|")
}

// isTableSeparator checks if a line is a table separator row.
func isTableSeparator(line string) bool {
	trimmed := strings.TrimSpace(line)
	// Remove outer pipes
	if strings.HasPrefix(trimmed, "|") {
		trimmed = trimmed[1:]
	}
	if strings.HasSuffix(trimmed, "|") {
		trimmed = trimmed[:len(trimmed)-1]
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return false
	}
	for _, ch := range trimmed {
		if ch != '-' && ch != ':' && ch != ' ' && ch != '|' {
			return false
		}
	}
	return strings.Contains(trimmed, "-")
}

// parseTableRow extracts cells from a table row line.
func parseTableRow(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	cells := strings.Split(trimmed, "|")
	result := make([]string, len(cells))
	for i, c := range cells {
		result[i] = strings.TrimSpace(c)
	}
	return result
}

// renderTable renders a full markdown table to box-drawing characters.
// Mirrors Rust render_table.
func (r *TerminalRenderer) renderTable(rows []string, output *strings.Builder) {
	if len(rows) == 0 {
		return
	}

	var headers []string
	var bodyRows [][]string

	// First row is headers
	headers = parseTableRow(rows[0])

	// Skip separator row(s), collect body
	startIdx := 1
	for startIdx < len(rows) && isTableSeparator(rows[startIdx]) {
		startIdx++
	}
	for i := startIdx; i < len(rows); i++ {
		bodyRows = append(bodyRows, parseTableRow(rows[i]))
	}

	// Calculate column widths
	colCount := len(headers)
	for _, row := range bodyRows {
		if len(row) > colCount {
			colCount = len(row)
		}
	}

	widths := make([]int, colCount)
	for i, h := range headers {
		if visibleWidth(h) > widths[i] {
			widths[i] = visibleWidth(h)
		}
	}
	for _, row := range bodyRows {
		for i, cell := range row {
			if i < colCount && visibleWidth(cell) > widths[i] {
				widths[i] = visibleWidth(cell)
			}
		}
	}

	border := lipgloss.NewStyle().Foreground(r.colorTheme.TableBorder).Render("│")

	// Render header row
	output.WriteString(border)
	for i, h := range headers {
		style := lipgloss.NewStyle().Bold(true).Foreground(r.colorTheme.Heading)
		padded := padRight(h, widths[i])
		output.WriteString(" " + style.Render(padded) + " " + border)
	}
	output.WriteString("\n")

	// Render separator
	output.WriteString(border)
	sepStyle := lipgloss.NewStyle().Foreground(r.colorTheme.TableBorder)
	for i := 0; i < colCount; i++ {
		output.WriteString(sepStyle.Render(strings.Repeat("─", widths[i]+2)))
		if i < colCount-1 {
			output.WriteString(sepStyle.Render("┼"))
		}
	}
	output.WriteString(border)
	output.WriteString("\n")

	// Render body rows
	for rowIdx, row := range bodyRows {
		output.WriteString(border)
		for i := 0; i < colCount; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			padded := padRight(cell, widths[i])
			output.WriteString(" " + padded + " " + border)
		}
		if rowIdx < len(bodyRows)-1 {
			output.WriteString("\n")
		}
	}
}

// --- MarkdownStreamState ---

// MarkdownStreamState mirrors Rust MarkdownStreamState for incremental streaming.
type MarkdownStreamState struct {
	pending string
}

// NewMarkdownStreamState creates a new streaming state.
func NewMarkdownStreamState() MarkdownStreamState {
	return MarkdownStreamState{}
}

// Push adds delta text and returns rendered markdown if a safe boundary is found.
// Mirrors Rust MarkdownStreamState::push.
func (s *MarkdownStreamState) Push(renderer *TerminalRenderer, delta string) string {
	s.pending += delta
	split := findStreamSafeBoundary(s.pending)
	if split == 0 {
		return ""
	}
	ready := s.pending[:split]
	s.pending = s.pending[split:]
	return renderer.MarkdownToAnsi(ready)
}

// Flush renders any remaining pending text.
// Mirrors Rust MarkdownStreamState::flush.
func (s *MarkdownStreamState) Flush(renderer *TerminalRenderer) string {
	if strings.TrimSpace(s.pending) == "" {
		s.pending = ""
		return ""
	}
	pending := s.pending
	s.pending = ""
	return renderer.MarkdownToAnsi(pending)
}

// findStreamSafeBoundary finds the last safe position to split streamed markdown.
// Mirrors Rust find_stream_safe_boundary.
func findStreamSafeBoundary(markdown string) int {
	inFence := false
	lastBoundary := 0

	offset := 0
	for _, line := range strings.SplitAfter(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			if !inFence {
				lastBoundary = offset + len(line)
			}
			offset += len(line)
			continue
		}

		if inFence {
			offset += len(line)
			continue
		}

		if trimmed == "" {
			lastBoundary = offset + len(line)
		}
		offset += len(line)
	}

	return lastBoundary
}

// --- Helper functions ---

// visibleWidth counts visible characters (excluding ANSI escape sequences).
// Mirrors Rust visible_width.
func visibleWidth(input string) int {
	return utf8.RuneCountInString(stripAnsi(input))
}

// stripAnsi removes ANSI escape sequences from a string.
// Mirrors Rust strip_ansi.
func stripAnsi(input string) string {
	var out strings.Builder
	runes := []rune(input)
	i := 0
	for i < len(runes) {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			// Skip ANSI sequence: ESC [ ... <letter>
			i += 2
			for i < len(runes) {
				if runes[i] >= 'A' && runes[i] <= 'Z' || runes[i] >= 'a' && runes[i] <= 'z' {
					i++
					break
				}
				i++
			}
		} else {
			out.WriteRune(runes[i])
			i++
		}
	}
	return out.String()
}

func isHRule(s string) bool {
	if len(s) < 3 {
		return false
	}
	ch := s[0]
	if ch != '-' && ch != '*' && ch != '_' {
		return false
	}
	for _, c := range s {
		if c != rune(ch) && c != ' ' {
			return false
		}
	}
	return true
}

func isUnorderedList(s string) bool {
	if len(s) < 2 {
		return false
	}
	return (s[0] == '-' || s[0] == '*' || s[0] == '+') && s[1] == ' '
}

func isOrderedList(s string) bool {
	for i, ch := range s {
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '.' && i+1 < len(s) && s[i+1] == ' ' {
			return true
		}
		return false
	}
	return false
}

func isTaskList(s string) bool {
	return strings.Contains(s, "[ ]") || strings.Contains(s, "[x]") || strings.Contains(s, "[X]")
}

func extractListItem(s string) string {
	if len(s) < 2 {
		return s
	}
	return strings.TrimSpace(s[2:])
}

func extractOrderedListItem(s string) (int, string) {
	dotIdx := strings.Index(s, ".")
	if dotIdx < 0 {
		return 1, s
	}
	idx := 1
	fmt.Sscanf(s[:dotIdx], "%d", &idx)
	content := strings.TrimSpace(s[dotIdx+1:])
	return idx, content
}

func extractTaskContent(s string) string {
	bracket := strings.Index(s, "]")
	if bracket < 0 {
		return s
	}
	return strings.TrimSpace(s[bracket+1:])
}

func countListIndent(line string) int {
	count := 0
	for _, ch := range line {
		if ch == ' ' {
			count++
		} else {
			break
		}
	}
	return count / 2
}

func padRight(s string, width int) string {
	pad := width - visibleWidth(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

// --- Legacy compatibility functions ---

// Markdown patterns (kept for backward compatibility with existing code).
var (
	boldPattern       = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicPattern     = regexp.MustCompile(`(^|[^*])\*([^*]+?)\*($|[^*])`)
	inlineCodePattern = regexp.MustCompile("`(.*?)`")
	linkPattern       = regexp.MustCompile(`\[(.+?)\]\((.+?)\)`)
	imagePattern      = regexp.MustCompile(`!\[(.+?)\]\((.+?)\)`)
	headerPattern     = regexp.MustCompile(`^(#{1,6})\s+(.+)`)
	quotePattern      = regexp.MustCompile(`^>\s?(.+)`)
	ulPattern         = regexp.MustCompile(`^[\-\*\+]\s+(.+)`)
	olPattern         = regexp.MustCompile(`^\d+\.\s+(.+)`)
	hrPattern         = regexp.MustCompile(`^---+$`)
)

// Legacy style variables kept for backward compatibility with model.go etc.
var (
	userStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86"))

	assistantStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	toolStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("11"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	systemStyle = lipgloss.NewStyle().
			Faint(true).
			Italic(true).
			Foreground(lipgloss.Color("243"))

	dividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)

	headerStyleMarkdown = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("39"))

	linkStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Underline(true)

	quoteStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)

	listBulletStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("180"))

	codeBlockStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	inlineCodeStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("180")).
			Background(lipgloss.Color("236"))

	boldStyle = lipgloss.NewStyle().Bold(true)
)

// RenderMessage renders a message with appropriate styling.
func RenderMessage(msg Message, width int) string {
	renderer := NewTerminalRenderer()
	switch msg.Role {
	case "user":
		return userStyle.Render("> " + msg.Content)
	case "assistant":
		return renderer.RenderMarkdown(msg.Content)
	case "tool":
		return toolStyle.Render("  tool: " + msg.Content)
	case "system":
		return systemStyle.Render("  " + msg.Content)
	default:
		return msg.Content
	}
}

// RenderStatusBar renders the bottom status bar.
func RenderStatusBar(model, status string, width int) string {
	left := " " + model
	right := status + " "
	content := left + strings.Repeat(" ", maxInt(width-len(left)-len(right), 0)) + right
	return statusBarStyle.Render(content)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
