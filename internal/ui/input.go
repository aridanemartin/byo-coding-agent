package ui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── styles ────────────────────────────────────────────────────────────────

var (
	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("36")). // cyan
			Padding(0, 1)
	hintStyle   = lipgloss.NewStyle().Faint(true)
	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("36")).Bold(true)
	confirmMark = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true) // yellow
)

// ── main message input ────────────────────────────────────────────────────

type chatInputModel struct {
	ti         textinput.Model
	history    []string
	histIdx    int    // -1 = not navigating; otherwise index from end (0 = most recent)
	bufferText string // text typed before the user started navigating history
	width      int
	submitted  string
	done       bool
}

func newChatInput(width int, history []string) chatInputModel {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.PromptStyle = promptStyle
	ti.CharLimit = 0
	ti.Width = max(width-6, 20) // leave room for borders + padding
	ti.Focus()
	return chatInputModel{
		ti:      ti,
		history: history,
		histIdx: -1,
		width:   width,
	}
}

func (m chatInputModel) Init() tea.Cmd { return textinput.Blink }

func (m chatInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.ti.Width = max(msg.Width-6, 20)
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			m.submitted = m.ti.Value()
			m.done = true
			return m, tea.Quit
		case tea.KeyCtrlD:
			if m.ti.Value() == "" {
				return m, tea.Quit
			}
		case tea.KeyCtrlC:
			m.ti.SetValue("")
			m.histIdx = -1
			return m, nil
		case tea.KeyUp:
			if len(m.history) == 0 {
				return m, nil
			}
			if m.histIdx == -1 {
				m.bufferText = m.ti.Value()
				m.histIdx = 0
			} else if m.histIdx < len(m.history)-1 {
				m.histIdx++
			}
			m.ti.SetValue(m.history[len(m.history)-1-m.histIdx])
			m.ti.CursorEnd()
			return m, nil
		case tea.KeyDown:
			if m.histIdx == -1 {
				return m, nil
			}
			if m.histIdx == 0 {
				m.histIdx = -1
				m.ti.SetValue(m.bufferText)
			} else {
				m.histIdx--
				m.ti.SetValue(m.history[len(m.history)-1-m.histIdx])
			}
			m.ti.CursorEnd()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	return m, cmd
}

func (m chatInputModel) View() string {
	w := m.width - 2
	if w < 20 {
		w = 20
	}
	box := boxStyle.Width(w).Render(m.ti.View())
	hint := hintStyle.Render("  enter: send · ↑↓: history · ctrl-d: exit")
	return box + "\n" + hint
}

// ReadChatInput shows the bordered input box and blocks until the user
// submits or hits ctrl-d. Returns (text, ok). ok=false on exit.
func ReadChatInput() (string, bool) {
	w := TermWidth()
	if w == 0 {
		w = 80
	}
	m := newChatInput(w, loadHistory())
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", false
	}
	result := final.(chatInputModel)
	if !result.done {
		return "", false
	}
	if result.submitted != "" {
		appendHistory(result.submitted)
	}
	return result.submitted, true
}

// ── confirm prompt ────────────────────────────────────────────────────────

type confirmModel struct {
	prompt string
	answer bool
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "y", "Y":
			m.answer = true
			return m, tea.Quit
		case "n", "N", "esc", "ctrl-c", "ctrl-d", "enter":
			m.answer = false
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	return confirmMark.Render(" ? ") + m.prompt + hintStyle.Render(" (y/n) ")
}

// Confirm shows a single-line y/n prompt and returns the user's answer.
// Default (Enter, Esc, Ctrl-C) is deny — safer for destructive tools.
func Confirm(prompt string) bool {
	final, err := tea.NewProgram(confirmModel{prompt: prompt}).Run()
	if err != nil {
		return false
	}
	return final.(confirmModel).answer
}

// ── history persistence ───────────────────────────────────────────────────

func historyPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".bettatech_harness_history")
}

func loadHistory() []string {
	path := historyPath()
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var lines []string
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		if s.Text() != "" {
			lines = append(lines, s.Text())
		}
	}
	return lines
}

func appendHistory(line string) {
	path := historyPath()
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}
