package widgets

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// ConfirmResult is sent when the user responds to a confirmation dialog.
type ConfirmResult struct {
	Action    string
	Resource  string
	Confirmed bool
}

// ConfirmDialog is a modal confirmation overlay.
type ConfirmDialog struct {
	visible  bool
	action   string
	resource string
}

func NewConfirmDialog() ConfirmDialog { return ConfirmDialog{} }

func (d ConfirmDialog) Show(action, resource string) ConfirmDialog {
	d.visible = true
	d.action = action
	d.resource = resource
	return d
}

func (d ConfirmDialog) Hide() ConfirmDialog {
	d.visible = false
	return d
}

func (d ConfirmDialog) IsVisible() bool { return d.visible }

func (d ConfirmDialog) Update(msg tea.Msg) (ConfirmDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "enter":
			d.visible = false
			return d, func() tea.Msg {
				return ConfirmResult{Action: d.action, Resource: d.resource, Confirmed: true}
			}
		default:
			d.visible = false
			return d, func() tea.Msg {
				return ConfirmResult{Action: d.action, Resource: d.resource, Confirmed: false}
			}
		}
	}
	return d, nil
}

func (d ConfirmDialog) View() string {
	if !d.visible {
		return ""
	}
	title := appstyles.Warning.Bold(true).Render(fmt.Sprintf("  %s %q?", d.action, d.resource))
	hint := appstyles.Muted.Render("  [y] confirm  [any] cancel")
	box := appstyles.DialogBox.Render(title + "\n" + hint)
	return lipgloss.Place(60, 8, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("#0D0D1A")))
}
