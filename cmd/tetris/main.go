package main

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── 常量 ───────────────────────────────────────────────────────

const (
	boardWidth  = 10
	boardHeight = 20
)

// ─── 方块定义 ───────────────────────────────────────────────────

// Tetromino 定义：使用 4x4 矩阵表示每种方块的旋转状态
type Tetromino struct {
	shape  [][]bool
	color  lipgloss.Color
	symbol string
}

var pieces = [][][][]bool{
	// I
	{
		{{false, false, false, false}, {true, true, true, true}, {false, false, false, false}, {false, false, false, false}},
		{{false, false, true, false}, {false, false, true, false}, {false, false, true, false}, {false, false, true, false}},
		{{false, false, false, false}, {false, false, false, false}, {true, true, true, true}, {false, false, false, false}},
		{{false, true, false, false}, {false, true, false, false}, {false, true, false, false}, {false, true, false, false}},
	},
	// O
	{
		{{true, true}, {true, true}},
		{{true, true}, {true, true}},
		{{true, true}, {true, true}},
		{{true, true}, {true, true}},
	},
	// T
	{
		{{false, true, false}, {true, true, true}, {false, false, false}},
		{{false, true, false}, {false, true, true}, {false, true, false}},
		{{false, false, false}, {true, true, true}, {false, true, false}},
		{{false, true, false}, {true, true, false}, {false, true, false}},
	},
	// S
	{
		{{false, true, true}, {true, true, false}, {false, false, false}},
		{{false, true, false}, {false, true, true}, {false, false, true}},
		{{false, false, false}, {false, true, true}, {true, true, false}},
		{{true, false, false}, {true, true, false}, {false, true, false}},
	},
	// Z
	{
		{{true, true, false}, {false, true, true}, {false, false, false}},
		{{false, false, true}, {false, true, true}, {false, true, false}},
		{{false, false, false}, {true, true, false}, {false, true, true}},
		{{false, true, false}, {true, true, false}, {true, false, false}},
	},
	// J
	{
		{{true, false, false}, {true, true, true}, {false, false, false}},
		{{false, true, true}, {false, true, false}, {false, true, false}},
		{{false, false, false}, {true, true, true}, {false, false, true}},
		{{false, true, false}, {false, true, false}, {true, true, false}},
	},
	// L
	{
		{{false, false, true}, {true, true, true}, {false, false, false}},
		{{false, true, false}, {false, true, false}, {false, true, true}},
		{{false, false, false}, {true, true, true}, {true, false, false}},
		{{true, true, false}, {false, true, false}, {false, true, false}},
	},
}

var pieceColors = []lipgloss.Color{
	"51",  // I - Cyan
	"226", // O - Yellow
	"165", // T - Magenta
	"82",  // S - Green
	"196", // Z - Red
	"39",  // J - Blue
	"214", // L - Orange
}

var pieceSymbols = []string{"I", "O", "T", "S", "Z", "J", "L"}

// ─── 游戏状态 ───────────────────────────────────────────────────

type cell struct {
	filled bool
	color  lipgloss.Color
}

type pieceState struct {
	idx      int // 方块类型索引 0-6
	rotation int // 旋转状态 0-3
	x, y     int // 左上角位置
}

type model struct {
	board     [][]cell
	current   pieceState
	next      int
	score     int
	level     int
	lines     int
	gameOver  bool
	paused    bool
	tickMs    int
	rand      *rand.Rand
	startTime time.Time
}

// ─── 初始化 ─────────────────────────────────────────────────────

func newModel() model {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	m := model{
		board:     newBoard(),
		rand:      r,
		tickMs:    500,
		level:     1,
		startTime: time.Now(),
	}
	m.current = m.randomPiece()
	m.next = r.Intn(len(pieces))
	return m
}

func newBoard() [][]cell {
	b := make([][]cell, boardHeight)
	for y := range b {
		b[y] = make([]cell, boardWidth)
	}
	return b
}

func (m *model) randomPiece() pieceState {
	idx := m.rand.Intn(len(pieces))
	return pieceState{idx: idx, rotation: 0, x: boardWidth/2 - 2, y: 0}
}

// ─── 碰撞检测 ───────────────────────────────────────────────────

func (m *model) collides(p pieceState) bool {
	shape := pieces[p.idx][p.rotation]
	for dy, row := range shape {
		for dx, filled := range row {
			if !filled {
				continue
			}
			px, py := p.x+dx, p.y+dy
			if px < 0 || px >= boardWidth || py >= boardHeight {
				return true
			}
			if py >= 0 && m.board[py][px].filled {
				return true
			}
		}
	}
	return false
}

// ─── 移动 & 旋转 ───────────────────────────────────────────────

func (m *model) move(dx, dy int) bool {
	next := m.current
	next.x += dx
	next.y += dy
	if !m.collides(next) {
		m.current = next
		return true
	}
	return false
}

func (m *model) rotate() {
	next := m.current
	next.rotation = (next.rotation + 1) % len(pieces[next.idx])
	// 墙踢：尝试左右偏移
	for _, kick := range []int{0, -1, 1, -2, 2} {
		next.x = m.current.x + kick
		if !m.collides(next) {
			m.current = next
			return
		}
	}
}

func (m *model) hardDrop() int {
	dropped := 0
	for m.move(0, 1) {
		dropped++
	}
	return dropped
}

// ─── 锁定 & 消行 ───────────────────────────────────────────────

func (m *model) lock() {
	shape := pieces[m.current.idx][m.current.rotation]
	for dy, row := range shape {
		for dx, filled := range row {
			if !filled {
				continue
			}
			px, py := m.current.x+dx, m.current.y+dy
			if py >= 0 && py < boardHeight && px >= 0 && px < boardWidth {
				m.board[py][px] = cell{filled: true, color: pieceColors[m.current.idx]}
			}
		}
	}
	m.clearLines()
	m.spawnNext()
}

func (m *model) clearLines() {
	cleared := 0
	for y := boardHeight - 1; y >= 0; y-- {
		full := true
		for x := 0; x < boardWidth; x++ {
			if !m.board[y][x].filled {
				full = false
				break
			}
		}
		if full {
			// 把上面的行下移
			for yy := y; yy > 0; yy-- {
				copy(m.board[yy], m.board[yy-1])
			}
			m.board[0] = make([]cell, boardWidth)
			cleared++
			y++ // 重新检查当前行（因为上面的行掉下来了）
		}
	}
	if cleared > 0 {
		m.lines += cleared
		// 计分：1行100，2行300，3行500，4行800
		scores := []int{0, 100, 300, 500, 800}
		if cleared <= 4 {
			m.score += scores[cleared] * m.level
		}
		m.level = m.lines/10 + 1
		// 加速：每升一级减少 40ms，最低 80ms
		m.tickMs = 500 - (m.level-1)*40
		if m.tickMs < 80 {
			m.tickMs = 80
		}
	}
}

func (m *model) spawnNext() {
	m.current = pieceState{idx: m.next, rotation: 0, x: boardWidth/2 - 2, y: 0}
	m.next = m.rand.Intn(len(pieces))
	if m.collides(m.current) {
		m.gameOver = true
	}
}

// ─── 预览幽灵方块位置 ───────────────────────────────────────────

func (m *model) ghostY() int {
	ghost := m.current
	for {
		next := ghost
		next.y++
		if m.collides(next) {
			return ghost.y
		}
		ghost = next
	}
}

// ─── BubbleTea 消息 ────────────────────────────────────────────

type tickMsg time.Time

func (m model) tick() tea.Cmd {
	d := time.Duration(m.tickMs) * time.Millisecond
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ─── Init ───────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return m.tick()
}

// ─── Update ─────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "p":
			m.paused = !m.paused
			if !m.paused {
				return m, m.tick()
			}
			return m, nil
		case "r":
			if m.gameOver {
				m = newModel()
				return m, m.tick()
			}
		}

		if m.gameOver || m.paused {
			return m, nil
		}

		switch msg.String() {
		case "left", "a":
			m.move(-1, 0)
		case "right", "d":
			m.move(1, 0)
		case "down", "s":
			if !m.move(0, 1) {
				m.lock()
			} else {
				m.score += 1
			}
		case "up", "w":
			m.rotate()
		case " ":
			dropped := m.hardDrop()
			m.score += dropped * 2
			m.lock()
		}

	case tickMsg:
		if !m.gameOver && !m.paused {
			if !m.move(0, 1) {
				m.lock()
			}
			return m, m.tick()
		}
	}

	return m, nil
}

// ─── View ───────────────────────────────────────────────────────

func (m model) View() string {
	if m.gameOver {
		return m.renderGameOver()
	}
	if m.paused {
		return m.renderPaused()
	}
	return m.renderGame()
}

func (m model) renderGame() string {
	// 构建带有当前方块和幽灵方块的显示板
	display := make([][]cell, boardHeight)
	for y := range m.board {
		display[y] = make([]cell, boardWidth)
		copy(display[y], m.board[y])
	}

	// 幽灵方块
	ghostY := m.ghostY()
	ghostShape := pieces[m.current.idx][m.current.rotation]
	for dy, row := range ghostShape {
		for dx, filled := range row {
			if filled {
				px, py := m.current.x+dx, ghostY+dy
				if py >= 0 && py < boardHeight && px >= 0 && px < boardWidth && !display[py][px].filled {
					display[py][px] = cell{filled: true, color: "240"}
				}
			}
		}
	}

	// 当前方块
	shape := pieces[m.current.idx][m.current.rotation]
	for dy, row := range shape {
		for dx, filled := range row {
			if filled {
				px, py := m.current.x+dx, m.current.y+dy
				if py >= 0 && py < boardHeight && px >= 0 && px < boardWidth {
					display[py][px] = cell{filled: true, color: pieceColors[m.current.idx]}
				}
			}
		}
	}

	// 渲染游戏板
	var board strings.Builder
	board.WriteString("  ╔" + strings.Repeat("══", boardWidth) + "╗\n")
	for y := 0; y < boardHeight; y++ {
		board.WriteString("  ║")
		for x := 0; x < boardWidth; x++ {
			c := display[y][x]
			if c.filled {
				blockStyle := lipgloss.NewStyle().
					Foreground(c.color).
					Background(darken(c.color)).
					Bold(true)
				board.WriteString(blockStyle.Render("██"))
			} else {
				board.WriteString("  ")
			}
		}
		board.WriteString("║\n")
	}
	board.WriteString("  ╚" + strings.Repeat("══", boardWidth) + "╝")

	// 侧边信息面板
	info := m.renderInfo()
	next := m.renderNext()

	// 组合布局
	boardLines := strings.Split(board.String(), "\n")
	infoLines := strings.Split(info, "\n")
	nextLines := strings.Split(next, "\n")

	maxLines := len(boardLines)
	if len(infoLines)+len(nextLines) > maxLines {
		maxLines = len(infoLines) + len(nextLines)
	}

	var result strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("51"))
	result.WriteString(titleStyle.Render("  ■ 俄罗斯方块 / TETRIS ■"))
	result.WriteString("\n\n")

	for i := 0; i < maxLines; i++ {
		line := ""
		if i < len(boardLines) {
			line = boardLines[i]
		}
		result.WriteString(line)

		// 侧边信息（和 next 预览）
		if i < len(infoLines) {
			result.WriteString("  " + infoLines[i])
		} else if i < len(infoLines)+len(nextLines) {
			result.WriteString("  " + nextLines[i-len(infoLines)])
		}
		result.WriteString("\n")
	}

	// 底部操作提示
	helpStyle := lipgloss.NewStyle().Faint(true)
	result.WriteString("\n")
	result.WriteString(helpStyle.Render("  ←→/AD 移动  ↑/W 旋转  ↓/S 软降  空格 硬降  P 暂停  Q 退出"))

	return result.String()
}

func (m model) renderInfo() string {
	var sb strings.Builder

	scoreStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("226"))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("249"))
	valueStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("255"))

	sb.WriteString(labelStyle.Render("┌─ INFO ───────┐") + "\n")
	sb.WriteString(labelStyle.Render("│ ") + scoreStyle.Render(fmt.Sprintf("分数 %7d", m.score)) + labelStyle.Render(" │") + "\n")
	sb.WriteString(labelStyle.Render("│ ") + valueStyle.Render(fmt.Sprintf("等级      %3d", m.level)) + labelStyle.Render(" │") + "\n")
	sb.WriteString(labelStyle.Render("│ ") + valueStyle.Render(fmt.Sprintf("消行      %3d", m.lines)) + labelStyle.Render(" │") + "\n")

	elapsed := time.Since(m.startTime)
	min := int(elapsed.Minutes())
	sec := int(elapsed.Seconds()) % 60
	sb.WriteString(labelStyle.Render("│ ") + valueStyle.Render(fmt.Sprintf("时间  %02d:%02d   ", min, sec)) + labelStyle.Render(" │") + "\n")
	sb.WriteString(labelStyle.Render("└──────────────┘"))

	return sb.String()
}

func (m model) renderNext() string {
	var sb strings.Builder

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("249"))
	sb.WriteString("\n")
	sb.WriteString(labelStyle.Render("┌─ NEXT ───────┐") + "\n")

	shape := pieces[m.next][0]
	color := pieceColors[m.next]
	blockStyle := lipgloss.NewStyle().Foreground(color).Background(darken(color)).Bold(true)
	emptyStyle := lipgloss.NewStyle()

	for _, row := range shape {
		sb.WriteString(labelStyle.Render("│ "))
		for _, filled := range row {
			if filled {
				sb.WriteString(blockStyle.Render("██"))
			} else {
				sb.WriteString(emptyStyle.Render("  "))
			}
		}
		sb.WriteString(labelStyle.Render("   │") + "\n")
	}
	sb.WriteString(labelStyle.Render("└──────────────┘"))

	return sb.String()
}

func (m model) renderGameOver() string {
	overlayStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("196"))

	scoreStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("226"))

	helpStyle := lipgloss.NewStyle().Faint(true)

	var sb strings.Builder
	sb.WriteString("\n\n")
	sb.WriteString(overlayStyle.Render("    ╔══════════════════════════╗"))
	sb.WriteString("\n")
	sb.WriteString(overlayStyle.Render("    ║                          ║"))
	sb.WriteString("\n")
	sb.WriteString(overlayStyle.Render("    ║    GAME  OVER !          ║"))
	sb.WriteString("\n")
	sb.WriteString(overlayStyle.Render("    ║                          ║"))
	sb.WriteString("\n")
	sb.WriteString(overlayStyle.Render("    ║  ") + scoreStyle.Render(fmt.Sprintf("最终分数: %d", m.score)) + overlayStyle.Render("       ║"))
	sb.WriteString("\n")
	sb.WriteString(overlayStyle.Render("    ║  ") + scoreStyle.Render(fmt.Sprintf("消除行数: %d", m.lines)) + overlayStyle.Render("       ║"))
	sb.WriteString("\n")
	sb.WriteString(overlayStyle.Render("    ║  ") + scoreStyle.Render(fmt.Sprintf("等等级:   %d", m.level)) + overlayStyle.Render("       ║"))
	sb.WriteString("\n")
	sb.WriteString(overlayStyle.Render("    ║                          ║"))
	sb.WriteString("\n")
	sb.WriteString(overlayStyle.Render("    ╚══════════════════════════╝"))
	sb.WriteString("\n\n")
	sb.WriteString(helpStyle.Render("    按 R 重新开始  |  按 Q 退出"))
	sb.WriteString("\n")

	return sb.String()
}

func (m model) renderPaused() string {
	pauseStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("51"))

	helpStyle := lipgloss.NewStyle().Faint(true)

	var sb strings.Builder
	sb.WriteString("\n\n\n")
	sb.WriteString(pauseStyle.Render("    ╔══════════════════════════╗"))
	sb.WriteString("\n")
	sb.WriteString(pauseStyle.Render("    ║                          ║"))
	sb.WriteString("\n")
	sb.WriteString(pauseStyle.Render("    ║      PAUSED  暂停        ║"))
	sb.WriteString("\n")
	sb.WriteString(pauseStyle.Render("    ║                          ║"))
	sb.WriteString("\n")
	sb.WriteString(pauseStyle.Render("    ╚══════════════════════════╝"))
	sb.WriteString("\n\n")
	sb.WriteString(helpStyle.Render("    按 P 继续  |  按 Q 退出"))
	sb.WriteString("\n")

	return sb.String()
}

// darken 返回一个更暗的背景色
func darken(c lipgloss.Color) lipgloss.Color {
	// 简单映射：返回比前景色更暗的颜色作为背景
	darkMap := map[lipgloss.Color]lipgloss.Color{
		"51":  "23",  // Cyan -> DarkCyan
		"226": "100", // Yellow -> DarkYellow
		"165": "90",  // Magenta -> DarkMagenta
		"82":  "28",  // Green -> DarkGreen
		"196": "88",  // Red -> DarkRed
		"39":  "25",  // Blue -> DarkBlue
		"214": "130", // Orange -> DarkOrange
		"240": "236", // Ghost
	}
	if dark, ok := darkMap[c]; ok {
		return dark
	}
	return "236"
}

// ─── main ───────────────────────────────────────────────────────

func main() {
	m := newModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
