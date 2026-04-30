package widgets

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// ClusterPickedMsg is sent when the user selects a context.
type ClusterPickedMsg struct{ Context string }

// ClusterPickerCancelMsg is sent when the user cancels without selecting.
type ClusterPickerCancelMsg struct{}

// ClusterPicker is a modal overlay for switching kubeconfig contexts.
type ClusterPicker struct {
	visible  bool
	contexts []string
	active   string
	cursor   int
	filter   string
}

func NewClusterPicker() ClusterPicker { return ClusterPicker{} }

// Show opens the picker with the provided context list and marks the active one.
func (p ClusterPicker) Show(contexts []string, active string) ClusterPicker {
	p.visible = true
	p.filter = ""
	sorted := append([]string(nil), contexts...)
	sort.Strings(sorted)
	p.contexts = sorted
	p.active = active
	// position cursor on the currently active context
	p.cursor = 0
	for i, c := range sorted {
		if c == active {
			p.cursor = i
			break
		}
	}
	return p
}

func (p ClusterPicker) Hide() ClusterPicker { p.visible = false; return p }
func (p ClusterPicker) IsVisible() bool     { return p.visible }

func (p ClusterPicker) filtered() []string {
	if p.filter == "" {
		return p.contexts
	}
	var out []string
	for _, c := range p.contexts {
		if strings.Contains(strings.ToLower(c), strings.ToLower(p.filter)) {
			out = append(out, c)
		}
	}
	return out
}

func clamp(v, max int) int {
	if v < 0 {
		return 0
	}
	if max == 0 {
		return 0
	}
	if v >= max {
		return max - 1
	}
	return v
}

func (p ClusterPicker) Update(msg tea.Msg) (ClusterPicker, tea.Cmd) {
	if !p.visible {
		return p, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}

	switch key.String() {
	case "esc":
		p.visible = false
		return p, func() tea.Msg { return ClusterPickerCancelMsg{} }

	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
		}

	case "down", "j":
		filtered := p.filtered()
		if p.cursor < len(filtered)-1 {
			p.cursor++
		}

	case "backspace", "ctrl+h":
		if len(p.filter) > 0 {
			runes := []rune(p.filter)
			p.filter = string(runes[:len(runes)-1])
			p.cursor = 0
		}

	case "enter":
		filtered := p.filtered()
		p.cursor = clamp(p.cursor, len(filtered))
		if len(filtered) > 0 {
			ctx := filtered[p.cursor]
			p.visible = false
			return p, func() tea.Msg { return ClusterPickedMsg{Context: ctx} }
		}

	default:
		if key.Type == tea.KeyRunes {
			p.filter += string(key.Runes)
			p.cursor = 0
		}
	}

	return p, nil
}

const (
	clusterPickerWidth    = 56
	clusterPickerMaxItems = 12
)

func (p ClusterPicker) View() string {
	if !p.visible {
		return ""
	}

	filtered := p.filtered()
	cursor := clamp(p.cursor, len(filtered))

	var sb strings.Builder

	title := appstyles.Primary.Bold(true).Render(" Cluster Context")
	sb.WriteString(title + "\n")
	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", clusterPickerWidth-2)) + "\n")

	if len(filtered) == 0 {
		sb.WriteString(appstyles.Muted.Render(" no matches") + "\n")
	}

	start := 0
	if len(filtered) > clusterPickerMaxItems && cursor >= clusterPickerMaxItems {
		start = cursor - clusterPickerMaxItems + 1
	}
	end := start + clusterPickerMaxItems
	if end > len(filtered) {
		end = len(filtered)
	}

	for i := start; i < end; i++ {
		c := filtered[i]
		selected := i == cursor
		isActive := c == p.active

		var prefix, nameStr, tagStr string
		if selected {
			prefix = appstyles.Primary.Render("▶ ")
			nameStr = appstyles.Primary.Bold(true).Render(c)
		} else {
			prefix = "  "
			nameStr = c
		}
		if isActive {
			tagStr = appstyles.Muted.Render("[active]")
		}

		gap := clusterPickerWidth - 2 -
			lipgloss.Width(prefix) -
			lipgloss.Width(c) -
			lipgloss.Width(tagStr) - 1
		if gap < 1 {
			gap = 1
		}
		sb.WriteString(prefix + nameStr + strings.Repeat(" ", gap) + tagStr + "\n")
	}

	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", clusterPickerWidth-2)) + "\n")

	filterDisplay := p.filter
	if filterDisplay == "" {
		filterDisplay = appstyles.Muted.Render("type to filter…")
	}
	sb.WriteString(" > " + filterDisplay + "▌\n")
	sb.WriteString(appstyles.Muted.Render(" [↑↓] nav  [enter] switch  [esc] cancel"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#00ADD8")).
		Padding(0, 1).
		Width(clusterPickerWidth).
		Render(sb.String())

	return lipgloss.Place(clusterPickerWidth+8, clusterPickerMaxItems+10,
		lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0D0D1A")))
}
