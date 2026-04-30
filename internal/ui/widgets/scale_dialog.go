package widgets

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// ScaleResult is sent when the user responds to the scale dialog.
type ScaleResult struct {
	Kind      string
	Name      string
	Namespace string
	Replicas  int32
	Confirmed bool
}

// ScaleDialog is a modal dialog for entering a replica count.
type ScaleDialog struct {
	visible   bool
	kind      string
	name      string
	namespace string
	current   int32
	input     string
}

func NewScaleDialog() ScaleDialog { return ScaleDialog{} }

func (d ScaleDialog) Show(kind, name, namespace string, current int32) ScaleDialog {
	d.visible = true
	d.kind = kind
	d.name = name
	d.namespace = namespace
	d.current = current
	d.input = fmt.Sprintf("%d", current)
	return d
}

func (d ScaleDialog) IsVisible() bool { return d.visible }

func (d ScaleDialog) Update(msg tea.Msg) (ScaleDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}
	switch keyMsg.String() {
	case "enter":
		var replicas int32
		fmt.Sscanf(d.input, "%d", &replicas)
		d.visible = false
		kind, name, ns := d.kind, d.name, d.namespace
		return d, func() tea.Msg {
			return ScaleResult{Kind: kind, Name: name, Namespace: ns, Replicas: replicas, Confirmed: true}
		}
	case "esc":
		d.visible = false
		return d, func() tea.Msg { return ScaleResult{Confirmed: false} }
	case "backspace":
		if len(d.input) > 0 {
			d.input = d.input[:len(d.input)-1]
		}
	default:
		k := keyMsg.String()
		if len(k) == 1 && k >= "0" && k <= "9" && len(d.input) < 4 {
			d.input += k
		}
	}
	return d, nil
}

func (d ScaleDialog) View() string {
	if !d.visible {
		return ""
	}
	title := appstyles.Warning.Bold(true).Render(fmt.Sprintf("  Scale %s/%s", d.kind, d.name))
	current := appstyles.Muted.Render(fmt.Sprintf("  Current replicas: %d", d.current))
	inputLine := appstyles.Primary.Render(fmt.Sprintf("  New replicas: %s█", d.input))
	hint := appstyles.Muted.Render("  [0-9] enter count  [enter] confirm  [esc] cancel")
	box := appstyles.DialogBox.Render(title + "\n" + current + "\n" + inputLine + "\n" + hint)
	return lipgloss.Place(60, 10, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0D0D1A")))
}
