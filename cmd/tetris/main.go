package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Constants
const (
	boardWidth  = 10
	boardHeight = 20
)

// Piece definitions — each rotation state is a list of [row, col] offsets
var pieces = map[byte][][][2]int{
	'I': {
		{{0, 0}, {0, 1}, {0, 2}, {0, 3}},
		{{0, 0}, {1, 0}, {2, 0}, {3, 0}},
		{{0, 0}, {0, 1}, {0, 2}, {0, 3}},
		{{0, 0}, {1, 0}, {2, 0}, {3, 0}},
	},
	'O': {
		{{0, 0}, {0, 1}, {1, 0}, {1, 1}},
		{{0, 0}, {0, 1}, {1, 0}, {1, 1}},
		{{0, 0}, {0, 1}, {1, 0}, {1, 1}},
		{{0, 0}, {0, 1}, {1, 0}, {1, 1}},
	},
	'T': {
		{{0, 1}, {1, 0}, {1, 1}, {1, 2}},
		{{0, 0}, {1, 0}, {1, 1}, {2, 0}},
		{{0, 0}, {0, 1}, {0, 2}, {1, 1}},
		{{0, 1}, {1, 0}, {1, 1}, {2, 1}},
	},
	'S': {
		{{0, 1}, {0, 2}, {1, 0}, {1, 1}},
		{{0, 0}, {1, 0}, {1, 1}, {2, 1}},
		{{0, 1}, {0, 2}, {1, 0}, {1, 1}},
		{{0, 0}, {1, 0}, {1, 1}, {2, 1}},
	},
	'Z': {
		{{0, 0}, {0, 1}, {1, 1}, {1, 2}},
		{{0, 1}, {1, 0}, {1, 1}, {2, 0}},
		{{0, 0}, {0, 1}, {1, 1}, {1, 2}},
		{{0, 1}, {1, 0}, {1, 1}, {2, 0}},
	},
	'J': {
		{{0, 0}, {1, 0}, {1, 1}, {1, 2}},
		{{0, 0}, {0, 1}, {1, 0}, {2, 0}},
		{{0, 0}, {0, 1}, {0, 2}, {1, 2}},
		{{0, 1}, {1, 1}, {2, 0}, {2, 1}},
	},
	'L': {
		{{0, 2}, {1, 0}, {1, 1}, {1, 2}},
		{{0, 0}, {1, 0}, {2, 0}, {2, 1}},
		{{0, 0}, {0, 1}, {0, 2}, {1, 0}},
		{{0, 0}, {0, 1}, {1, 1}, {2, 1}},
	},
}

var pieceColors = map[byte]string{
	'I': "36", // cyan
	'O': "33", // yellow
	'T': "35", // magenta
	'S': "32", // green
	'Z': "31", // red
	'J': "34", // blue
	'L': "93", // bright yellow
}

var pieceNames = []byte{'I', 'O', 'T', 'S', 'Z', 'J', 'L'}

// currentPiece holds the active falling piece state
type currentPiece struct {
	kind     byte
	rotation int
	row      int
	col      int
}

// Game model
type model struct {
	board     [][]byte // 0 = empty, 'I'/'O'/... = filled
	cur       currentPiece
	next      byte
	score     int
	lines     int
	level     int
	gameOver  bool
	paused    bool
	rng       *rand.Rand
	lastTick  time.Time
	tickSpeed time.Duration
	started   bool
}

func newModel() model {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	board := make([][]byte, boardHeight)
	for i := range board {
		board[i] = make([]byte, boardWidth)
	}
	m := model{
		board:     board,
		rng:       rng,
		tickSpeed: 500 * time.Millisecond,
	}
	m.next = pieceNames[rng.Intn(len(pieceNames))]
	m.spawnPiece()
	return m
}

func (m *model) spawnPiece() {
	m.cur = currentPiece{
		kind:     m.next,
		rotation: 0,
		row:      0,
		col:      boardWidth/2 - 1,
	}
	m.next = pieceNames[m.rng.Intn(len(pieceNames))]
	if !m.isValid(m.cur) {
		m.gameOver = true
	}
}

func (m *model) blocks(p currentPiece) [][2]int {
	return pieces[p.kind][p.rotation]
}

func (m *model) isValid(p currentPiece) bool {
	for _, b := range m.blocks(p) {
		r, c := p.row+b[0], p.col+b[1]
		if r < 0 || r >= boardHeight || c < 0 || c >= boardWidth {
			return false
		}
		if m.board[r][c] != 0 {
			return false
		}
	}
	return true
}

func (m *model) lock() {
	for _, b := range m.blocks(m.cur) {
		r, c := m.cur.row+b[0], m.cur.col+b[1]
		if r >= 0 && r < boardHeight && c >= 0 && c < boardWidth {
			m.board[r][c] = m.cur.kind
		}
	}
	m.clearLines()
	m.spawnPiece()
}

func (m *model) clearLines() {
	cleared := 0
	for r := boardHeight - 1; r >= 0; r-- {
		full := true
		for c := 0; c < boardWidth; c++ {
			if m.board[r][c] == 0 {
				full = false
				break
			}
		}
		if full {
			cleared++
			// Shift rows down
			for rr := r; rr > 0; rr-- {
				copy(m.board[rr], m.board[rr-1])
			}
			for c := 0; c < boardWidth; c++ {
				m.board[0][c] = 0
			}
			r++ // recheck this row
		}
	}
	if cleared > 0 {
		m.lines += cleared
		// Scoring: 100, 300, 500, 800
		scoreMap := []int{0, 100, 300, 500, 800}
		if cleared < len(scoreMap) {
			m.score += scoreMap[cleared] * (m.level + 1)
		}
		m.level = m.lines / 10
		// Speed up
		speed := 500 - m.level*40
		if speed < 50 {
			speed = 50
		}
		m.tickSpeed = time.Duration(speed) * time.Millisecond
	}
}

func (m *model) ghostRow() int {
	g := m.cur
	for {
		next := currentPiece{g.kind, g.rotation, g.row + 1, g.col}
		if !m.isValid(next) {
			return g.row
		}
		g = next
	}
}

func (m *model) rotate() {
	next := currentPiece{m.cur.kind, (m.cur.rotation + 1) % 4, m.cur.row, m.cur.col}
	if m.isValid(next) {
		m.cur = next
		return
	}
	// Wall kick: try shift left/right
	for _, offset := range []int{-1, 1, -2, 2} {
		kick := currentPiece{m.cur.kind, (m.cur.rotation + 1) % 4, m.cur.row, m.cur.col + offset}
		if m.isValid(kick) {
			m.cur = kick
			return
		}
	}
}

func (m *model) moveLeft() {
	next := currentPiece{m.cur.kind, m.cur.rotation, m.cur.row, m.cur.col - 1}
	if m.isValid(next) {
		m.cur = next
	}
}

func (m *model) moveRight() {
	next := currentPiece{m.cur.kind, m.cur.rotation, m.cur.row, m.cur.col + 1}
	if m.isValid(next) {
		m.cur = next
	}
}

func (m *model) moveDown() bool {
	next := currentPiece{m.cur.kind, m.cur.rotation, m.cur.row + 1, m.cur.col}
	if m.isValid(next) {
		m.cur = next
		return true
	}
	m.lock()
	return false
}

func (m *model) hardDrop() {
	dropped := 0
	for {
		next := currentPiece{m.cur.kind, m.cur.rotation, m.cur.row + 1, m.cur.col}
		if !m.isValid(next) {
			break
		}
		m.cur = next
		dropped++
	}
	m.score += dropped * 2
	m.lock()
}

// BubbleTea messages
type tickMsg time.Time

func (m model) Init() tea.Cmd {
	return tea.Batch(tickCmd(m.tickSpeed), tea.EnterAltScreen)
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.gameOver {
			if msg.String() == "r" || msg.String() == "R" {
				m = newModel()
				m.started = true
				return m, tickCmd(m.tickSpeed)
			}
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
			return m, nil
		}

		if !m.started {
			m.started = true
			return m, tickCmd(m.tickSpeed)
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "p":
			m.paused = !m.paused
		case "up", "w":
			if !m.paused {
				m.rotate()
			}
		case "left", "a":
			if !m.paused {
				m.moveLeft()
			}
		case "right", "d":
			if !m.paused {
				m.moveRight()
			}
		case "down", "s":
			if !m.paused {
				if m.moveDown() {
					m.score += 1
				}
			}
		case " ":
			if !m.paused {
				m.hardDrop()
			}
		}
		return m, nil

	case tickMsg:
		if m.gameOver || m.paused || !m.started {
			return m, tickCmd(m.tickSpeed)
		}
		m.moveDown()
		return m, tickCmd(m.tickSpeed)
	}
	return m, nil
}

func (m model) View() string {
	var sb strings.Builder

	// Title
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("35")).Render("  🎮 俄 罗 斯 方 块")
	sb.WriteString(title)
	sb.WriteString("\n")

	// Build the board view with current piece and ghost
	display := make([][]byte, boardHeight)
	for r := range display {
		display[r] = make([]byte, boardWidth)
		copy(display[r], m.board[r])
	}

	// Ghost piece
	if !m.gameOver {
		ghostR := m.ghostRow()
		if ghostR != m.cur.row {
			for _, b := range m.blocks(m.cur) {
				r, c := ghostR+b[0], m.cur.col+b[1]
				if r >= 0 && r < boardHeight && c >= 0 && c < boardWidth && display[r][c] == 0 {
					display[r][c] = '.' // ghost marker
				}
			}
		}
	}

	// Current piece
	if !m.gameOver {
		for _, b := range m.blocks(m.cur) {
			r, c := m.cur.row+b[0], m.cur.col+b[1]
			if r >= 0 && r < boardHeight && c >= 0 && c < boardWidth {
				display[r][c] = m.cur.kind
			}
		}
	}

	// Render board + sidebar
	borderColor := lipgloss.NewStyle().Foreground(lipgloss.Color("36"))

	for r := 0; r < boardHeight; r++ {
		sb.WriteString(borderColor.Render("│"))
		for c := 0; c < boardWidth; c++ {
			ch := display[r][c]
			if ch == 0 {
				sb.WriteString("  ")
			} else if ch == '.' {
				sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("░░"))
			} else {
				color := pieceColors[ch]
				sb.WriteString(lipgloss.NewStyle().
					Foreground(lipgloss.Color(color)).
					Background(lipgloss.Color(color)).
					Render("██"))
			}
		}
		sb.WriteString(borderColor.Render("│"))

		// Sidebar info on specific rows
		switch r {
		case 1:
			sb.WriteString("  ")
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("36")).Render("下一个"))
		case 2, 3, 4, 5:
			sb.WriteString("  ")
			sb.WriteString(renderNextPiece(m.next, r-2))
		case 7:
			sb.WriteString("  ")
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("36")).Render("分  数"))
		case 8:
			sb.WriteString(fmt.Sprintf("  %s", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("93")).Render(fmt.Sprintf("%8d", m.score))))
		case 10:
			sb.WriteString("  ")
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("36")).Render("消  行"))
		case 11:
			sb.WriteString(fmt.Sprintf("  %s", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("93")).Render(fmt.Sprintf("%8d", m.lines))))
		case 13:
			sb.WriteString("  ")
			sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("36")).Render("等  级"))
		case 14:
			sb.WriteString(fmt.Sprintf("  %s", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("93")).Render(fmt.Sprintf("%8d", m.level+1))))
		}

		sb.WriteString("\n")
	}

	// Bottom border
	sb.WriteString(borderColor.Render("└"))
	sb.WriteString(strings.Repeat("──", boardWidth))
	sb.WriteString(borderColor.Render("┘"))

	// Controls or game over
	sb.WriteString("\n")
	if m.gameOver {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("  游 戏 结 束 ！"))
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("93")).Render(fmt.Sprintf(" 最终分数: %d", m.score)))
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("36")).Render("  [R]重来 [Q]退出"))
	} else if m.paused {
		sb.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("93")).Render("  ⏸ 暂 停 中  [P]继续"))
	} else {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("90")).Render("  ↑旋转 ↓↓左→右 空格硬降 P暂停 Q退出"))
	}

	return sb.String()
}

func renderNextPiece(kind byte, row int) string {
	rot := pieces[kind][0]
	line := ""
	for c := 0; c < 4; c++ {
		found := false
		for _, b := range rot {
			if b[0] == row && b[1] == c {
				found = true
				break
			}
		}
		if found {
			color := pieceColors[kind]
			line += lipgloss.NewStyle().
				Foreground(lipgloss.Color(color)).
				Background(lipgloss.Color(color)).
				Render("██")
		} else {
			line += "  "
		}
	}
	return line
}

func main() {
	m := newModel()
	p := tea.NewProgram(m, tea.WithFPS(30))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
