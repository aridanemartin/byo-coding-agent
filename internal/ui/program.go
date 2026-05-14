package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/betta-tech/byo-coding-agent/internal/subagent"
)

// AgentRunner is what the program calls when the user submits a line. It
// covers both slash commands and normal agent turns — the harness wires it
// up in main, so the program doesn't need to know about commands or agents.
type AgentRunner func(ctx context.Context, input string) error

// ── messages ──────────────────────────────────────────────────────────────

// AppendMsg appends text to the scrollback. Sent by the stdout-pipe forwarder.
type AppendMsg string

// ApprovalRequest is sent (via program.Send) when the agent needs y/n from
// the user. The program shows the prompt and writes the answer to Reply.
type ApprovalRequest struct {
	Prompt string
	Reply  chan bool
}

// internal messages
type agentDoneMsg struct{ err error }

// ── model ─────────────────────────────────────────────────────────────────

type modelState int

const (
	stateIdle modelState = iota
	stateRunning
	stateAwaitingApproval
)

type harness struct {
	runner AgentRunner

	width, height int

	viewport viewport.Model
	input    textinput.Model
	spinner  spinner.Model

	state          modelState
	approvalPrompt string
	approvalReply  chan bool
	followBottom   bool // auto-scroll viewport to bottom on new content

	// output must be a pointer: Bubble Tea passes the model by value through
	// Update, and strings.Builder panics when copied (copyCheck on its
	// internal self-pointer). The pointer survives the copy intact.
	output *strings.Builder
}

func newHarness(runner AgentRunner, banner string) harness {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("36")).Bold(true)
	ti.CharLimit = 0
	ti.Focus()

	vp := viewport.New(80, 20)
	vp.SetContent(banner)

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("36")).Bold(true)

	m := harness{
		runner:       runner,
		input:        ti,
		viewport:     vp,
		spinner:      sp,
		followBottom: true,
		output:       &strings.Builder{},
	}
	m.output.WriteString(banner)
	return m
}

// NewProgram builds the Bubble Tea program. Wire the returned program to
// a Confirm callback and a stdout-pipe forwarder before calling Run.
func NewProgram(runner AgentRunner) *tea.Program {
	banner := BannerText(TermWidth())
	return tea.NewProgram(
		newHarness(runner, banner),
		tea.WithAltScreen(),
		tea.WithMouseAllMotion(),
	)
}

func (m harness) Init() tea.Cmd {
	return textinput.Blink
}

func (m harness) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		inputHeight := 5 // spinner line + box (3) + hint (1) — always reserved
		m.viewport.Width = msg.Width
		m.viewport.Height = max(msg.Height-inputHeight, 3)
		m.input.Width = max(msg.Width-6, 20)
		if m.followBottom {
			m.viewport.GotoBottom()
		}
		return m, nil

	case tea.KeyMsg:
		if m.state == stateAwaitingApproval {
			return m.updateApproval(msg)
		}
		return m.updateInput(msg)

	case AppendMsg:
		m.output.WriteString(string(msg))
		m.viewport.SetContent(m.output.String())
		if m.followBottom {
			m.viewport.GotoBottom()
		}
		return m, nil

	case agentDoneMsg:
		m.state = stateIdle
		if msg.err != nil {
			m.output.WriteString(Dimmed(fmt.Sprintf("error: %v", msg.err)) + "\n")
			m.viewport.SetContent(m.output.String())
			if m.followBottom {
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case ApprovalRequest:
		m.state = stateAwaitingApproval
		m.approvalPrompt = msg.Prompt
		m.approvalReply = msg.Reply
		return m, nil

	case spinner.TickMsg:
		// Only re-issue ticks while the agent is running. When idle, we
		// stop ticking — the spinner pauses on its current frame and
		// won't be rendered anyway (View checks state).
		if m.state != stateRunning {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	// Forward unhandled events to the viewport (handles scroll keys).
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	m.followBottom = m.viewport.AtBottom()
	return m, cmd
}

func (m harness) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		if m.state != stateIdle {
			return m, nil
		}
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		// Echo the prompt the user submitted, so the scrollback shows it.
		m.output.WriteString(Dimmed("❯ ") + text + "\n")
		m.viewport.SetContent(m.output.String())
		m.viewport.GotoBottom()
		m.followBottom = true
		m.input.SetValue("")
		m.state = stateRunning
		// Kick the spinner alongside the agent run so it animates while
		// we wait. The TickMsg handler self-perpetuates while running.
		return m, tea.Batch(m.runOnce(text), m.spinner.Tick)

	case tea.KeyCtrlD, tea.KeyCtrlC:
		return m, tea.Quit

	case tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.followBottom = m.viewport.AtBottom()
		return m, cmd
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m harness) updateApproval(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var answer bool
	switch msg.String() {
	case "y", "Y":
		answer = true
	case "n", "N", "esc", "ctrl+c", "ctrl+d", "enter":
		answer = false
	default:
		return m, nil
	}
	// Echo the decision into scrollback so the user can see what they answered.
	verdict := "denied"
	if answer {
		verdict = "approved"
	}
	m.output.WriteString(Dimmed(fmt.Sprintf("  ↳ %s", verdict)) + "\n")
	m.viewport.SetContent(m.output.String())
	m.viewport.GotoBottom()

	if m.approvalReply != nil {
		m.approvalReply <- answer
		m.approvalReply = nil
	}
	m.approvalPrompt = ""
	m.state = stateRunning
	return m, nil
}

func (m harness) runOnce(text string) tea.Cmd {
	return func() tea.Msg {
		err := m.runner(context.Background(), text)
		return agentDoneMsg{err: err}
	}
}

// ── view ──────────────────────────────────────────────────────────────────

func (m harness) View() string {
	return lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		m.inputArea(),
	)
}

// activeSubagentSummary returns a short string like " · research" or
// " · research ×2, codereview" when subagents are running. Empty otherwise.
// Tacked onto the spinner line so the user can see what's in flight.
func activeSubagentSummary() string {
	active := subagent.Active()
	if len(active) == 0 {
		return ""
	}
	parts := make([]string, 0, len(active))
	for name, n := range active {
		if n == 1 {
			parts = append(parts, name)
		} else {
			parts = append(parts, fmt.Sprintf("%s ×%d", name, n))
		}
	}
	return " · " + strings.Join(parts, ", ")
}

func (m harness) inputArea() string {
	w := m.width - 2
	if w < 20 {
		w = 20
	}
	if m.state == stateAwaitingApproval {
		prompt := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true).Render(" ? ")
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("220")).
			Padding(0, 1).
			Width(w).
			Render(prompt + m.approvalPrompt + Dimmed(" (y/n)"))
		hint := Dimmed("  y: yes · n / esc / enter: no")
		return box + "\n" + hint
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("36")).
		Padding(0, 1).
		Width(w).
		Render(m.input.View())
	hint := Dimmed("  enter: send · pgup/pgdn: scroll · ctrl-d: exit")

	// One line above the input box is always reserved for the spinner —
	// empty when idle, animated when running. Keeps the layout stable.
	spinnerLine := ""
	if m.state == stateRunning {
		spinnerLine = "  " + m.spinner.View() + " " + Dimmed("thinking..."+activeSubagentSummary())
	}
	return spinnerLine + "\n" + box + "\n" + hint
}
