package widgets

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
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
	danger   bool
	action   string
	resource string
	termW    int
	termH    int
}

func NewConfirmDialog() ConfirmDialog { return ConfirmDialog{termW: 80, termH: 24} }

func (d ConfirmDialog) Show(action, resource string) ConfirmDialog {
	d.visible = true
	d.danger = false
	d.action = action
	d.resource = resource
	return d
}

// ShowDanger is Show with the dialog rendered in the destructive-action style
// (red title and border). Used for irreversible operations like kill.
func (d ConfirmDialog) ShowDanger(action, resource string) ConfirmDialog {
	d.visible = true
	d.danger = true
	d.action = action
	d.resource = resource
	return d
}

func (d ConfirmDialog) Hide() ConfirmDialog {
	d.visible = false
	return d
}

func (d ConfirmDialog) IsVisible() bool { return d.visible }

// SetSize records terminal dimensions so click coordinates can be mapped to
// the on-screen "[y] confirm" button zone.
func (d ConfirmDialog) SetSize(w, h int) ConfirmDialog {
	d.termW = w
	d.termH = h
	return d
}

const confirmButtonText = "[y] confirm"

func (d ConfirmDialog) renderTitle() string {
	style := appstyles.Warning
	if d.danger {
		style = appstyles.Error
	}
	return style.Bold(true).Render(fmt.Sprintf("  %s %q?", d.action, d.resource))
}

func (d ConfirmDialog) renderHint() string {
	return appstyles.Muted.Render("  " + confirmButtonText + "  [any] cancel")
}

func (d ConfirmDialog) renderBox() string {
	box := appstyles.DialogBox
	if d.danger {
		box = box.BorderForeground(appstyles.ColorFailed)
	}
	return box.Render(d.renderTitle() + "\n" + d.renderHint())
}

// confirmZone returns screen-space bounds [x0, y0, x1, y1) covering the
// "[y] confirm" text inside the rendered dialog box, after modalOverlay
// centers the box on the full terminal.
func (d ConfirmDialog) confirmZone() (int, int, int, int) {
	box := d.renderBox()
	boxW := lipgloss.Width(box)
	boxH := lipgloss.Height(box)
	boxX := (d.termW - boxW) / 2
	boxY := (d.termH - boxH) / 2
	if boxX < 0 {
		boxX = 0
	}
	if boxY < 0 {
		boxY = 0
	}
	// Layout inside the rendered box (DialogBox = RoundedBorder + Padding(1,2)):
	//   row 0: top border         row 3: hint line
	//   row 1: top padding        row 4: bottom padding
	//   row 2: title              row 5: bottom border
	// Hint string: "  [y] confirm  [any] cancel" — leading "  " (2 cols) puts
	// the [y] confirm token at content-X 2.
	hintY := boxY + 3
	hintX := boxX + 1 /*border*/ + 2 /*pad-left*/ + 2 /*leading spaces*/
	btnW := lipgloss.Width(confirmButtonText)
	return hintX, hintY, hintX + btnW, hintY + 1
}

func (d ConfirmDialog) clickIsConfirm(x, y int) bool {
	x0, y0, x1, y1 := d.confirmZone()
	return x >= x0 && x < x1 && y >= y0 && y < y1
}

func (d ConfirmDialog) Update(msg tea.Msg) (ConfirmDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
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
	case tea.MouseClickMsg:
		mouse := msg.Mouse()
		confirmed := d.clickIsConfirm(mouse.X, mouse.Y)
		d.visible = false
		return d, func() tea.Msg {
			return ConfirmResult{Action: d.action, Resource: d.resource, Confirmed: confirmed}
		}
	}
	return d, nil
}

func (d ConfirmDialog) View() string {
	if !d.visible {
		return ""
	}
	box := d.renderBox()
	w := lipgloss.Width(box) + 4
	h := lipgloss.Height(box) + 2
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(appstyles.ColorAbyss)))
}
