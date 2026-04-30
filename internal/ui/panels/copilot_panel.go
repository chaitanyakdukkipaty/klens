package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	aimod "github.com/chaitanyak/klens/internal/ai"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

type copilotState int

const (
	copilotInput copilotState = iota
	copilotWaiting
	copilotReview
	copilotExecuting
)

// CopilotRequestMsg triggers an AI plan request.
type CopilotRequestMsg struct {
	Intent  string
	Context aimod.ClusterContext
}

// CopilotExecuteMsg triggers execution of approved steps.
type CopilotExecuteMsg struct {
	Steps []aimod.CopilotStep
}

// CopilotPanel is the AI copilot input and plan execution panel.
type CopilotPanel struct {
	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model
	width    int
	height   int
	focused  bool
	state    copilotState
	steps    []aimod.CopilotStep
	cursor   int
	thinking string
}

func NewCopilotPanel(w, h int) CopilotPanel {
	ta := textarea.New()
	ta.Placeholder = "Ask anything… e.g. 'show logs for pods matching api'"
	ta.SetWidth(w - 6)
	ta.SetHeight(3)
	ta.Focus()
	ta.ShowLineNumbers = false

	vp := viewport.New(w-4, h-10)
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#CE93D8"))

	return CopilotPanel{
		input:    ta,
		viewport: vp,
		spinner:  sp,
		width:    w,
		height:   h,
	}
}

func (p CopilotPanel) SetSize(w, h int) CopilotPanel {
	p.width = w
	p.height = h
	p.input.SetWidth(w - 6)
	p.viewport.Width = w - 4
	p.viewport.Height = h - 10
	return p
}

func (p CopilotPanel) SetFocused(f bool) CopilotPanel { p.focused = f; return p }

func (p CopilotPanel) Update(msg tea.Msg) (CopilotPanel, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		if p.state == copilotWaiting {
			var cmd tea.Cmd
			p.spinner, cmd = p.spinner.Update(msg)
			return p, cmd
		}

	case aimod.CopilotResponseMsg:
		p.thinking = msg.Thinking
		if msg.Err != nil {
			p.thinking = "Error: " + msg.Err.Error()
			p.state = copilotInput
		} else {
			p.steps = msg.Steps
			for i := range p.steps {
				p.steps[i].Status = aimod.StepPending
			}
			p.cursor = 0
			p.state = copilotReview
			p.rebuildViewport()
		}

	case tea.KeyMsg:
		switch p.state {
		case copilotInput:
			switch msg.String() {
			case "enter":
				intent := strings.TrimSpace(p.input.Value())
				if intent != "" {
					p.state = copilotWaiting
					p.input.Reset()
					return p, tea.Batch(
						p.spinner.Tick,
						func() tea.Msg {
							return CopilotRequestMsg{Intent: intent}
						},
					)
				}
			case "esc":
				// signal to parent to close copilot
				return p, func() tea.Msg { return CopilotClosedMsg{} }
			default:
				var cmd tea.Cmd
				p.input, cmd = p.input.Update(msg)
				return p, cmd
			}

		case copilotReview:
			switch msg.String() {
			case "up", "k":
				if p.cursor > 0 {
					p.cursor--
					p.rebuildViewport()
				}
			case "down", "j":
				if p.cursor < len(p.steps)-1 {
					p.cursor++
					p.rebuildViewport()
				}
			case "x": // skip step
				if p.cursor < len(p.steps) {
					p.steps[p.cursor].Status = aimod.StepSkipped
					p.rebuildViewport()
				}
			case "enter": // execute all pending steps
				return p, func() tea.Msg {
					return CopilotExecuteMsg{Steps: p.steps}
				}
			case "esc":
				p.state = copilotInput
				p.steps = nil
			}

		case copilotExecuting:
			if msg.String() == "esc" {
				p.state = copilotInput
			}
		}
	}
	return p, nil
}

// UpdateStepStatus updates the status of a single step from outside the panel
// (e.g. during execution driven by the app model).
func (p CopilotPanel) UpdateStepStatus(idx int, status aimod.StepStatus, msg string) CopilotPanel {
	if idx < 0 || idx >= len(p.steps) {
		return p
	}
	p.steps[idx].Status = status
	p.steps[idx].StatusMessage = msg
	p.state = copilotExecuting
	// If every step has reached a terminal state, return to input so the user
	// can ask a follow-up without having to manually dismiss the panel.
	allDone := true
	for _, s := range p.steps {
		if s.Status != aimod.StepDone && s.Status != aimod.StepFailed && s.Status != aimod.StepSkipped {
			allDone = false
			break
		}
	}
	if allDone {
		p.state = copilotInput
	}
	p.rebuildViewport()
	return p
}

// CopilotClosedMsg signals the parent to hide the copilot panel.
type CopilotClosedMsg struct{}

func (p *CopilotPanel) rebuildViewport() {
	var sb strings.Builder
	if p.thinking != "" {
		sb.WriteString(styles.Muted.Render(p.thinking) + "\n\n")
	}
	for i, step := range p.steps {
		icon := step.Status.Icon()
		desc := step.Describe()

		var style lipgloss.Style
		switch step.Status {
		case aimod.StepDone:
			style = styles.Success
		case aimod.StepFailed:
			style = styles.Error
		case aimod.StepSkipped:
			style = styles.Muted
		case aimod.StepRunning:
			style = styles.Primary
		default:
			if step.IsDestructive {
				style = styles.Warning
			} else {
				style = lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC"))
			}
		}

		prefix := "  "
		if i == p.cursor {
			prefix = styles.Primary.Render("▶ ")
		}

		line := fmt.Sprintf("%s%s %s", prefix, icon, desc)
		if step.IsDestructive {
			line += styles.Warning.Render(" [DESTRUCTIVE]")
		}
		if step.StatusMessage != "" {
			line += "  " + styles.Muted.Render(step.StatusMessage)
		}
		sb.WriteString(style.Render(line) + "\n")
	}
	p.viewport.SetContent(sb.String())
}

func (p CopilotPanel) View() string {
	border := styles.CopilotBorder
	if !p.focused {
		border = styles.NormalBorder
	}

	title := styles.CopilotTitle.Render("✦ AI Copilot")

	var body string
	switch p.state {
	case copilotInput:
		help := styles.Muted.Render("  enter to send  esc to close")
		body = title + "\n" + help + "\n\n" + p.input.View()

	case copilotWaiting:
		body = title + "\n  " + p.spinner.View() + styles.Muted.Render(" Thinking…")

	case copilotReview:
		help := styles.Muted.Render("  ↑↓ navigate  x skip  enter execute all  esc cancel")
		body = title + "\n" + help + "\n\n" + p.viewport.View()

	case copilotExecuting:
		help := styles.Muted.Render("  esc to cancel")
		body = title + "\n  " + styles.Primary.Render("Executing steps…") + "  " + help + "\n\n" + p.viewport.View()
	}

	return border.Width(p.width - 2).Height(p.height - 2).Render(body)
}
