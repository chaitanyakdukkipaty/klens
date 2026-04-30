package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	k8slogs "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

const maxLogLines = 10000

// LogViewer displays merged streaming logs from multiple pods.
type LogViewer struct {
	viewport    viewport.Model
	width       int
	height      int
	focused     bool
	pods        []string
	lines       []k8slogs.LogLine
	filterInput string
	filterOn    bool
	filter      string
	autoScroll  bool
}

func NewLogViewer(w, h int) LogViewer {
	vp := viewport.New(w-2, h-6)
	return LogViewer{
		viewport:   vp,
		width:      w,
		height:     h,
		autoScroll: true,
	}
}

func (v LogViewer) SetSize(w, h int) LogViewer {
	v.width = w
	v.height = h
	v.viewport.Width = w - 2
	v.viewport.Height = h - 6
	return v
}

func (v LogViewer) SetFocused(f bool) LogViewer { v.focused = f; return v }

func (v LogViewer) SetPods(pods []string) LogViewer {
	v.pods = pods
	v.lines = nil
	v.filter = ""
	v.filterInput = ""
	v.autoScroll = true
	return v
}

func (v LogViewer) Update(msg tea.Msg) (LogViewer, tea.Cmd) {
	switch msg := msg.(type) {
	case k8slogs.LogLineMsg:
		for _, line := range msg.Lines {
			v.lines = append(v.lines, line)
		}
		// Trim ring buffer
		if len(v.lines) > maxLogLines {
			v.lines = v.lines[len(v.lines)-maxLogLines:]
		}
		v.rebuildViewport()
		if v.autoScroll {
			v.viewport.GotoBottom()
		}

	case tea.KeyMsg:
		if v.filterOn {
			switch msg.String() {
			case "enter", "esc":
				v.filterOn = false
				v.filter = v.filterInput
				v.rebuildViewport()
			case "ctrl+c":
				v.filterOn = false
				v.filterInput = ""
				v.filter = ""
				v.rebuildViewport()
			case "backspace":
				if len(v.filterInput) > 0 {
					v.filterInput = v.filterInput[:len(v.filterInput)-1]
					v.rebuildViewport()
				}
			default:
				if len(msg.String()) == 1 {
					v.filterInput += msg.String()
					v.rebuildViewport()
				}
			}
			return v, nil
		}
		switch msg.String() {
		case "/":
			v.filterOn = true
			v.filterInput = v.filter
		case "G":
			v.viewport.GotoBottom()
			v.autoScroll = true
		case "g":
			v.viewport.GotoTop()
			v.autoScroll = false
		default:
			var cmd tea.Cmd
			v.viewport, cmd = v.viewport.Update(msg)
			// If user scrolls up, disable auto-scroll
			if msg.String() == "up" || msg.String() == "k" || msg.String() == "pgup" {
				v.autoScroll = false
			}
			return v, cmd
		}
	}
	return v, nil
}

func (v *LogViewer) rebuildViewport() {
	var sb strings.Builder
	low := strings.ToLower(v.filter)
	for _, l := range v.lines {
		if low != "" && !strings.Contains(strings.ToLower(l.Text), low) {
			continue
		}
		sb.WriteString(renderLogLine(l))
		sb.WriteByte('\n')
	}
	v.viewport.SetContent(sb.String())
}

func renderLogLine(l k8slogs.LogLine) string {
	colorIdx := l.ColorIdx
	if colorIdx >= len(styles.LogPrefixColors) {
		colorIdx = colorIdx % len(styles.LogPrefixColors)
	}
	color := styles.LogPrefixColors[colorIdx]
	prefix := lipgloss.NewStyle().Foreground(color).Bold(true).Render(fmt.Sprintf("[%s] ", l.Pod))
	if l.IsSystem {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Italic(true).Render(l.Text)
	}
	return prefix + l.Text
}

func (v LogViewer) View() string {
	border := styles.NormalBorder
	if v.focused {
		border = styles.FocusedBorder
	}

	podNames := strings.Join(v.pods, ", ")
	if len(podNames) > v.width-20 {
		podNames = podNames[:v.width-23] + "…"
	}
	title := styles.Title.Render("Logs: ") + styles.Primary.Render(podNames)

	filterBar := ""
	if v.filterOn {
		filterBar = "\n" + styles.Primary.Render("filter: ") + v.filterInput + styles.Muted.Render("█")
	} else if v.filter != "" {
		filterBar = "\n" + styles.Primary.Render("filter: ") + styles.Warning.Render(v.filter)
	}

	help := styles.Muted.Render("  ↑↓ scroll  / filter  g top  G bottom  esc back")
	lineCount := fmt.Sprintf("  %d lines", len(v.lines))

	header := title + "  " + styles.Muted.Render(lineCount) + filterBar + "\n" + help

	return border.Width(v.width - 2).Height(v.height - 2).Render(header + "\n\n" + v.viewport.View())
}
