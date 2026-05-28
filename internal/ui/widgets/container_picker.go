package widgets

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// ContainerKind tags the role of a container in the pod spec so the picker
// can label init/ephemeral entries distinctly.
type ContainerKind int

const (
	ContainerRegular ContainerKind = iota
	ContainerInit
	ContainerEphemeral
)

// ContainerEntry is one selectable row in the picker.
type ContainerEntry struct {
	Name string
	Kind ContainerKind
}

// ContainerPickedMsg is sent when the user picks a container.
type ContainerPickedMsg struct {
	Pod       string
	Namespace string
	Container string
}

// ContainerPickerCancelMsg is sent when the user dismisses the picker.
type ContainerPickerCancelMsg struct{}

// ContainerPicker is a modal overlay that lists a pod's containers and
// lets the user pick one to attach into.
type ContainerPicker struct {
	visible   bool
	pod       string
	namespace string
	entries   []ContainerEntry
	cursor    int
	filter    string
}

func NewContainerPicker() ContainerPicker { return ContainerPicker{} }

// Show opens the picker with the pod's containers. cursor and filter reset
// each time so the user always starts at the first entry.
func (p ContainerPicker) Show(pod, namespace string, entries []ContainerEntry) ContainerPicker {
	p.visible = true
	p.pod = pod
	p.namespace = namespace
	p.entries = entries
	p.cursor = 0
	p.filter = ""
	return p
}

func (p ContainerPicker) Hide() ContainerPicker { p.visible = false; return p }
func (p ContainerPicker) IsVisible() bool       { return p.visible }

func (p ContainerPicker) filtered() []ContainerEntry {
	if p.filter == "" {
		return p.entries
	}
	var out []ContainerEntry
	for _, e := range p.entries {
		if strings.Contains(e.Name, p.filter) {
			out = append(out, e)
		}
	}
	return out
}

func (p ContainerPicker) clampCursor(list []ContainerEntry) int {
	if len(list) == 0 {
		return 0
	}
	if p.cursor >= len(list) {
		return len(list) - 1
	}
	return p.cursor
}

func (p ContainerPicker) Update(msg tea.Msg) (ContainerPicker, tea.Cmd) {
	if !p.visible {
		return p, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return p, nil
	}

	switch key.String() {
	case "esc":
		p.visible = false
		return p, func() tea.Msg { return ContainerPickerCancelMsg{} }

	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
		}

	case "down", "ctrl+n":
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
		p.cursor = p.clampCursor(filtered)
		if len(filtered) == 0 {
			return p, nil
		}
		picked := filtered[p.cursor].Name
		pod, ns := p.pod, p.namespace
		p.visible = false
		return p, func() tea.Msg {
			return ContainerPickedMsg{Pod: pod, Namespace: ns, Container: picked}
		}

	default:
		if len(key.Text) > 0 {
			p.filter += key.Text
			p.cursor = 0
		}
	}

	return p, nil
}

const (
	containerPickerWidth    = 52
	containerPickerMaxItems = 12
)

func (p ContainerPicker) View() string {
	if !p.visible {
		return ""
	}

	filtered := p.filtered()
	cursor := p.clampCursor(filtered)

	var sb strings.Builder

	title := appstyles.Primary.Bold(true).Render(" Container — " + p.pod)
	sb.WriteString(title + "\n")
	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", containerPickerWidth-2)) + "\n")

	if len(p.entries) == 0 {
		sb.WriteString(appstyles.Muted.Render(" (no containers in pod)") + "\n")
	} else if len(filtered) == 0 {
		sb.WriteString(appstyles.Muted.Render(" no matches") + "\n")
	}

	start := 0
	if len(filtered) > containerPickerMaxItems && cursor >= containerPickerMaxItems {
		start = cursor - containerPickerMaxItems + 1
	}
	end := start + containerPickerMaxItems
	if end > len(filtered) {
		end = len(filtered)
	}

	for i := start; i < end; i++ {
		e := filtered[i]
		selected := i == cursor

		var prefix, nameStr, tagStr string
		if selected {
			prefix = appstyles.Primary.Render("▶ ")
			nameStr = appstyles.Primary.Bold(true).Render(e.Name)
		} else {
			prefix = "  "
			nameStr = e.Name
		}
		switch e.Kind {
		case ContainerInit:
			tagStr = appstyles.Muted.Render("[init]")
		case ContainerEphemeral:
			tagStr = appstyles.Muted.Render("[ephemeral]")
		}

		gap := containerPickerWidth - 2 -
			lipgloss.Width(prefix) -
			lipgloss.Width(e.Name) -
			lipgloss.Width(tagStr) - 1
		if gap < 1 {
			gap = 1
		}
		sb.WriteString(prefix + nameStr + strings.Repeat(" ", gap) + tagStr + "\n")
	}

	sb.WriteString(appstyles.Muted.Render(strings.Repeat("─", containerPickerWidth-2)) + "\n")

	filterDisplay := p.filter
	if filterDisplay == "" {
		filterDisplay = appstyles.Muted.Render("type to filter…")
	}
	sb.WriteString(" > " + filterDisplay + "▌\n")

	if len(filtered) > 0 {
		sb.WriteString(appstyles.Muted.Render(" [↑↓] nav  [enter] attach  [esc] cancel"))
	} else {
		sb.WriteString(appstyles.Muted.Render(" [esc] cancel"))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(appstyles.ColorPrimary).
		Padding(0, 1).
		Width(containerPickerWidth).
		Render(sb.String())

	return lipgloss.Place(containerPickerWidth+8, containerPickerMaxItems+10,
		lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(appstyles.ColorAbyss)))
}
