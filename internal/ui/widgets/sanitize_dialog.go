package widgets

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// SanitizePhrase is the exact text the user must type to confirm a sanitize.
const SanitizePhrase = "Yes"

// SanitizeRequest is sent when the sanitize dialog closes. Confirmed is true
// only when the user typed SanitizePhrase exactly and pressed enter.
type SanitizeRequest struct {
	Namespace string
	Confirmed bool
}

// SanitizeDialog is a modal that deletes all pods in a completed/error state
// once the user types the exact confirmation phrase.
type SanitizeDialog struct {
	visible   bool
	namespace string
	input     string
}

func NewSanitizeDialog() SanitizeDialog { return SanitizeDialog{} }

func (d SanitizeDialog) Show(namespace string) SanitizeDialog {
	d.visible = true
	d.namespace = namespace
	d.input = ""
	return d
}

func (d SanitizeDialog) Hide() SanitizeDialog {
	d.visible = false
	d.input = ""
	return d
}

func (d SanitizeDialog) IsVisible() bool { return d.visible }

func (d SanitizeDialog) Update(msg tea.Msg) (SanitizeDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	switch keyMsg.String() {
	case "enter":
		confirmed := d.input == SanitizePhrase
		ns := d.namespace
		d = d.Hide()
		return d, func() tea.Msg {
			return SanitizeRequest{Namespace: ns, Confirmed: confirmed}
		}
	case "esc":
		ns := d.namespace
		d = d.Hide()
		return d, func() tea.Msg {
			return SanitizeRequest{Namespace: ns, Confirmed: false}
		}
	case "backspace":
		if len(d.input) > 0 {
			// Trim one rune off the end (Yes Please! is ASCII; rune-safe trim
			// keeps future i18n changes from corrupting state.)
			r := []rune(d.input)
			d.input = string(r[:len(r)-1])
		}
	case "ctrl+u":
		d.input = ""
	default:
		k := keyMsg.String()
		if len(k) == 1 && len(d.input) < len(SanitizePhrase)+4 {
			d.input += k
		}
	}
	return d, nil
}

func (d SanitizeDialog) View() string {
	if !d.visible {
		return ""
	}
	title := appstyles.Primary.Bold(true).Render("<Sanitize>")
	line1 := appstyles.Muted.Render("Sanitize deletes all pods in completed/error state")
	line2 := appstyles.Muted.Render("Please enter ") +
		appstyles.Warning.Bold(true).Render(SanitizePhrase) +
		appstyles.Muted.Render(" to proceed.")
	cursor := "█"
	if d.input == SanitizePhrase {
		// Hide the cursor and color the phrase to signal a match.
		cursor = ""
	}
	inputLine := appstyles.Bold.Render("Confirm: ") +
		appstyles.Primary.Render(d.input+cursor)
	hint := appstyles.Muted.Render("[enter] OK   [esc] Cancel")

	body := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.PlaceHorizontal(50, lipgloss.Center, title),
		"",
		lipgloss.PlaceHorizontal(50, lipgloss.Center, line1),
		lipgloss.PlaceHorizontal(50, lipgloss.Center, line2),
		"",
		inputLine,
		"",
		lipgloss.PlaceHorizontal(50, lipgloss.Center, hint),
	)
	box := appstyles.DialogBox.Render(body)
	return lipgloss.Place(60, 12, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(appstyles.ColorAbyss)))
}
