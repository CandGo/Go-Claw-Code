package tui

import (
	"testing"
)

func TestSlashCommandPrefix(t *testing.T) {
	tests := []struct {
		line string
		pos  int
		want string
	}{
		{"/he", 3, "/he"},
		{"/help me", 5, ""},
		{"hello", 5, ""},
		{"/help", 2, ""},
	}
	for _, tt := range tests {
		got := slashCommandPrefix(tt.line, tt.pos)
		if got != tt.want {
			t.Errorf("slashCommandPrefix(%q, %d) = %q, want %q", tt.line, tt.pos, got, tt.want)
		}
	}
}

func TestToggleSubmissionFlipsVimMode(t *testing.T) {
	editor := NewLineEditor("> ", []string{"/help", "/vim"})

	result := editor.handleSubmission("/vim")
	if result != submitToggleVim {
		t.Error("expected toggle vim")
	}

	editor.VimEnabled = true
	result = editor.handleSubmission("/vim")
	if result != submitToggleVim {
		t.Error("expected toggle vim again")
	}
}

func TestNormalModeMotionAndInsert(t *testing.T) {
	editor := NewLineEditor("> ", nil)
	editor.VimEnabled = true
	session := NewEditSession(true)
	session.Text = "hello"
	session.Cursor = len(session.Text)
	editor.handleEscape(session) // enter normal mode

	editor.handleChar(session, 'h')
	editor.handleChar(session, 'i') // enter insert mode
	editor.handleChar(session, '!') // insert character

	if session.Mode != ModeInsert {
		t.Errorf("mode = %v, want ModeInsert", session.Mode)
	}
	if session.Text != "hel!lo" {
		t.Errorf("text = %q, want %q", session.Text, "hel!lo")
	}
}

func TestYankAndPasteLine(t *testing.T) {
	editor := NewLineEditor("> ", nil)
	editor.VimEnabled = true
	session := NewEditSession(true)
	session.Text = "alpha\nbeta\ngamma"
	session.Cursor = 0
	editor.handleEscape(session) // enter normal mode

	// yy - yank current line
	editor.handleChar(session, 'y')
	editor.handleChar(session, 'y')
	// p - paste after
	editor.handleChar(session, 'p')

	if session.Text != "alpha\nalpha\nbeta\ngamma" {
		t.Errorf("text = %q, want %q", session.Text, "alpha\nalpha\nbeta\ngamma")
	}
}

func TestDeleteAndPasteLine(t *testing.T) {
	editor := NewLineEditor("> ", nil)
	editor.VimEnabled = true
	session := NewEditSession(true)
	session.Text = "alpha\nbeta\ngamma"
	session.Cursor = 0
	editor.handleEscape(session) // enter normal mode

	// j - move down to "beta"
	editor.handleChar(session, 'j')
	// dd - delete line
	editor.handleChar(session, 'd')
	editor.handleChar(session, 'd')
	// p - paste after
	editor.handleChar(session, 'p')

	if session.Text != "alpha\ngamma\nbeta\n" {
		t.Errorf("text = %q, want %q", session.Text, "alpha\ngamma\nbeta\n")
	}
}

func TestVisualModeTracksSelection(t *testing.T) {
	editor := NewLineEditor("> ", nil)
	editor.VimEnabled = true
	session := NewEditSession(true)
	session.Text = "alpha\nbeta"
	session.Cursor = 0
	editor.handleEscape(session)

	// v - visual mode
	editor.handleChar(session, 'v')
	// j - move down
	editor.handleChar(session, 'j')
	// l - move right
	editor.handleChar(session, 'l')

	if session.Mode != ModeVisual {
		t.Errorf("mode = %v, want ModeVisual", session.Mode)
	}
	if session.VisualAnchor == nil {
		t.Fatal("visual anchor should be set")
	}
	start, end := selectionBounds(session.Text, *session.VisualAnchor, session.Cursor)
	if start != 0 || end != 8 {
		t.Errorf("selection = (%d, %d), want (0, 8)", start, end)
	}
}

func TestCommandMode(t *testing.T) {
	editor := NewLineEditor("> ", nil)
	editor.VimEnabled = true
	session := NewEditSession(true)
	session.Text = "draft"
	session.Cursor = len(session.Text)
	editor.handleEscape(session)

	// : - enter command mode
	editor.handleChar(session, ':')
	editor.handleChar(session, 'q')
	editor.handleChar(session, '!')

	if session.Mode != ModeCommand {
		t.Errorf("mode = %v, want ModeCommand", session.Mode)
	}
	if session.CommandBuffer != ":q!" {
		t.Errorf("commandBuffer = %q, want %q", session.CommandBuffer, ":q!")
	}
}

func TestPushHistoryIgnoresBlank(t *testing.T) {
	editor := NewLineEditor("> ", nil)

	editor.PushHistory("   ")
	editor.PushHistory("/help")

	if len(editor.History) != 1 {
		t.Errorf("history length = %d, want 1", len(editor.History))
	}
	if editor.History[0] != "/help" {
		t.Errorf("history[0] = %q, want %q", editor.History[0], "/help")
	}
}

func TestTabCompletesSlashCommands(t *testing.T) {
	editor := NewLineEditor("> ", []string{"/help", "/hello"})
	session := NewEditSession(false)
	session.Text = "/he"
	session.Cursor = len(session.Text)

	editor.completeSlashCommand(session)

	if session.Text != "/help" {
		t.Errorf("text = %q, want %q", session.Text, "/help")
	}
	if session.Cursor != 5 {
		t.Errorf("cursor = %d, want 5", session.Cursor)
	}
}

func TestCtrlCCancelsWhenInputExists(t *testing.T) {
	editor := NewLineEditor("> ", nil)
	session := NewEditSession(false)
	session.Text = "draft"
	session.Cursor = len(session.Text)

	action := editor.HandleKeyEvent(session, Key{Code: KeyChar, Rune: 'c', Ctrl: true})
	if action != ActionCancel {
		t.Errorf("action = %v, want ActionCancel", action)
	}
}
