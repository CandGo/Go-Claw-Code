package tui

import (
	"strings"
	"testing"
)

func TestColorThemeDefaults(t *testing.T) {
	theme := DefaultColorTheme()
	if theme.Heading == "" {
		t.Error("Heading color should not be empty")
	}
	if theme.SpinnerActive == "" {
		t.Error("SpinnerActive color should not be empty")
	}
}

func TestTerminalRendererHeadings(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("# Heading 1\n\n## Heading 2\n\n### Heading 3")
	if !strings.Contains(output, "Heading 1") {
		t.Error("should contain heading text")
	}
	if !strings.Contains(output, "Heading 2") {
		t.Error("should contain h2 text")
	}
	if !strings.Contains(output, "Heading 3") {
		t.Error("should contain h3 text")
	}
}

func TestTerminalRendererBoldItalic(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("This is **bold** and *italic* text.")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "bold") {
		t.Error("should contain bold text")
	}
	if !strings.Contains(plain, "italic") {
		t.Error("should contain italic text")
	}
}

func TestTerminalRendererInlineCode(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("Use `console.log` here.")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "console.log") {
		t.Error("should contain inline code")
	}
}

func TestTerminalRendererLinks(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("See [Claw](https://example.com) now.")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "Claw") {
		t.Error("should contain link text")
	}
	if !strings.Contains(plain, "https://example.com") {
		t.Error("should contain link URL")
	}
}

func TestTerminalRendererCodeBlock(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("```rust\nfn main() {\n    println!(\"hi\");\n}\n```")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "╭─ rust") {
		t.Errorf("should contain code block border with language, got: %s", plain)
	}
	if !strings.Contains(plain, "fn main()") {
		t.Error("should contain code content")
	}
	if !strings.Contains(plain, "╰─") {
		t.Error("should contain closing border")
	}
}

func TestTerminalRendererUnorderedList(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("- item one\n- item two\n- item three")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "• item one") {
		t.Errorf("should contain bullet items, got: %s", plain)
	}
}

func TestTerminalRendererOrderedList(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("1. first\n2. second\n3. third")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "1. first") {
		t.Errorf("should contain ordered items, got: %s", plain)
	}
	if !strings.Contains(plain, "2. second") {
		t.Error("should contain second ordered item")
	}
	if !strings.Contains(plain, "3. third") {
		t.Error("should contain third ordered item")
	}
}

func TestTerminalRendererBlockquote(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("> This is a quote")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "This is a quote") {
		t.Error("should contain quote text")
	}
}

func TestTerminalRendererHorizontalRule(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("above\n\n---\n\nbelow")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "above") || !strings.Contains(plain, "below") {
		t.Error("should contain text above and below hr")
	}
}

func TestTerminalRendererTable(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("| Name | Value |\n| ---- | ----- |\n| alpha | 1 |\n| beta | 22 |")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "Name") {
		t.Errorf("should contain table header, got: %s", plain)
	}
	if !strings.Contains(plain, "alpha") {
		t.Error("should contain table body")
	}
	if !strings.Contains(plain, "beta") {
		t.Error("should contain second table row")
	}
}

func TestTerminalRendererTaskList(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("- [x] done\n- [ ] pending")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "[x] done") {
		t.Errorf("should contain checked task, got: %s", plain)
	}
	if !strings.Contains(plain, "[ ] pending") {
		t.Error("should contain unchecked task")
	}
}

func TestTerminalRendererImage(t *testing.T) {
	r := NewTerminalRenderer()
	output := r.RenderMarkdown("![alt text](https://example.com/img.png)")
	plain := stripAnsi(output)
	if !strings.Contains(plain, "[image:https://example.com/img.png]") {
		t.Errorf("should contain image reference, got: %s", plain)
	}
}

func TestMarkdownStreamStatePush(t *testing.T) {
	r := NewTerminalRenderer()
	state := NewMarkdownStreamState()

	// Push incomplete text (no blank line boundary)
	result := state.Push(r, "# Heading")
	if result != "" {
		t.Error("should return empty for incomplete block")
	}

	// Push completion boundary
	result = state.Push(r, "\n\nParagraph text\n\n")
	if result == "" {
		t.Error("should return rendered text after boundary")
	}
	plain := stripAnsi(result)
	if !strings.Contains(plain, "Heading") {
		t.Error("should contain heading")
	}
}

func TestMarkdownStreamStateFlush(t *testing.T) {
	r := NewTerminalRenderer()
	state := NewMarkdownStreamState()

	state.Push(r, "some pending text")
	result := state.Flush(r)
	if result == "" {
		t.Error("flush should return remaining text")
	}
	plain := stripAnsi(result)
	if !strings.Contains(plain, "pending") {
		t.Error("should contain flushed text")
	}
}

func TestMarkdownStreamStateCodeFence(t *testing.T) {
	r := NewTerminalRenderer()
	state := NewMarkdownStreamState()

	// Open code fence - no output yet
	result := state.Push(r, "```rust\nfn main() {}\n")
	if result != "" {
		t.Error("should not flush inside code fence")
	}

	// Close fence - should flush
	result = state.Push(r, "```\n")
	if result == "" {
		t.Error("should flush after closing fence")
	}
	plain := stripAnsi(result)
	if !strings.Contains(plain, "fn main()") {
		t.Error("should contain code content")
	}
}

func TestSpinnerTickAndFinish(t *testing.T) {
	theme := DefaultColorTheme()
	s := NewSpinner()
	var buf strings.Builder
	s.Tick("Working", theme, &buf)
	if !strings.Contains(buf.String(), "Working") {
		t.Error("tick should write label")
	}

	buf.Reset()
	s.Finish("Done", theme, &buf)
	if !strings.Contains(buf.String(), "Done") {
		t.Error("finish should write label")
	}
}

func TestSpinnerFail(t *testing.T) {
	theme := DefaultColorTheme()
	s := NewSpinner()
	var buf strings.Builder
	s.Fail("Error occurred", theme, &buf)
	if !strings.Contains(buf.String(), "Error occurred") {
		t.Error("fail should write label")
	}
}

func TestStripAnsi(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "hello"},
		{"\x1b[31mred\x1b[0m text", "red text"},
		{"\x1b[1;32;48;5;236mbold green\x1b[0m", "bold green"},
		{"plain", "plain"},
	}
	for _, tt := range tests {
		got := stripAnsi(tt.input)
		if got != tt.want {
			t.Errorf("stripAnsi(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestVisibleWidth(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"\x1b[31mred\x1b[0m", 3},
		{"中文", 2},
	}
	for _, tt := range tests {
		got := visibleWidth(tt.input)
		if got != tt.want {
			t.Errorf("visibleWidth(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestFindStreamSafeBoundary(t *testing.T) {
	// Empty string
	if got := findStreamSafeBoundary(""); got != 0 {
		t.Errorf("empty: got %d, want 0", got)
	}

	// Text with blank line
	if got := findStreamSafeBoundary("hello\n\nworld\n"); got == 0 {
		t.Error("should find boundary at blank line")
	}

	// Inside fence - no boundary
	md := "```rust\ncode\n"
	if got := findStreamSafeBoundary(md); got != 0 {
		t.Errorf("inside fence: got %d, want 0", got)
	}

	// Closed fence - boundary at end
	md = "```rust\ncode\n```\n"
	if got := findStreamSafeBoundary(md); got == 0 {
		t.Error("should find boundary after closing fence")
	}
}

func TestIsTableRow(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"| a | b |", true},
		{"no table", false},
		{"|single|", true},
		{"", false},
	}
	for _, tt := range tests {
		got := isTableRow(tt.input)
		if got != tt.want {
			t.Errorf("isTableRow(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsTableSeparator(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"| --- | --- |", true},
		{"|:---:|:---:|", true},
		{"| a | b |", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isTableSeparator(tt.input)
		if got != tt.want {
			t.Errorf("isTableSeparator(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestApplyCodeBackground(t *testing.T) {
	line := "hello"
	result := applyCodeBackground(line)
	if !strings.Contains(result, "hello") {
		t.Error("should contain original text")
	}
	if !strings.Contains(result, "\x1b[48;5;236m") {
		t.Error("should contain background color")
	}
}

func TestIsHRule(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"---", true},
		{"***", true},
		{"___", true},
		{"- not", false},
		{"", false},
		{"##", false},
	}
	for _, tt := range tests {
		got := isHRule(tt.input)
		if got != tt.want {
			t.Errorf("isHRule(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
