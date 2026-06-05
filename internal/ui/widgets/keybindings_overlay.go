package widgets

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/chaitanyak/klens/internal/ui/keymap"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// KeybindingsOverlay renders the keymap catalog as a scrollable overlay.
// Opened by `?` or the ☰ menu; j/k / wheel scroll, esc/q/? close.
type KeybindingsOverlay struct {
	visible bool
	scroll  int
	termW   int
	termH   int
}

func NewKeybindingsOverlay() KeybindingsOverlay { return KeybindingsOverlay{} }

func (k KeybindingsOverlay) Show() KeybindingsOverlay {
	k.visible = true
	k.scroll = 0
	return k
}

func (k KeybindingsOverlay) Hide() KeybindingsOverlay { k.visible = false; return k }
func (k KeybindingsOverlay) IsVisible() bool          { return k.visible }
func (k KeybindingsOverlay) SetSize(w, h int) KeybindingsOverlay {
	k.termW = w
	k.termH = h
	return k
}

const keysOverlayWidth = 62

// lines flattens the catalog into rendered lines (section titles + bindings).
func (k KeybindingsOverlay) lines() []string {
	inner := keysOverlayWidth - 4
	var out []string
	for si, sec := range keymap.Sections() {
		if si > 0 {
			out = append(out, "")
		}
		out = append(out, appstyles.Primary.Bold(true).Render(" "+sec.Title))
		for _, b := range sec.Bindings {
			key := appstyles.HelpKey.Render(b.Key)
			pad := 16 - lipgloss.Width(b.Key)
			if pad < 1 {
				pad = 1
			}
			line := "   " + key + strings.Repeat(" ", pad) + appstyles.HelpDesc.Render(b.Desc)
			if lipgloss.Width(line) > inner {
				line = line[:len(line)] // styled truncation is lossy; keep as-is
			}
			out = append(out, line)
		}
	}
	return out
}

// bodyHeight is how many catalog lines fit in the overlay at the current
// terminal size (border 2 + title row + footer row reserved).
func (k KeybindingsOverlay) bodyHeight() int {
	h := k.termH - 8
	if h < 5 {
		h = 5
	}
	return h
}

func (k KeybindingsOverlay) maxScroll() int {
	return max(0, len(k.lines())-k.bodyHeight())
}

func (k KeybindingsOverlay) scrollBy(d int) KeybindingsOverlay {
	k.scroll += d
	if k.scroll < 0 {
		k.scroll = 0
	}
	if m := k.maxScroll(); k.scroll > m {
		k.scroll = m
	}
	return k
}

func (k KeybindingsOverlay) Update(msg tea.Msg) (KeybindingsOverlay, tea.Cmd) {
	if !k.visible {
		return k, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "?":
			k.visible = false
		case "up", "k":
			k = k.scrollBy(-1)
		case "down", "j":
			k = k.scrollBy(1)
		case "pgup", "ctrl+u":
			k = k.scrollBy(-k.bodyHeight())
		case "pgdown", "ctrl+d", " ":
			k = k.scrollBy(k.bodyHeight())
		case "g":
			k.scroll = 0
		case "G":
			k.scroll = k.maxScroll()
		}
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			k = k.scrollBy(-3)
		case tea.MouseWheelDown:
			k = k.scrollBy(3)
		}
	case tea.MouseClickMsg:
		k.visible = false
	}
	return k, nil
}

func (k KeybindingsOverlay) View() string {
	lines := k.lines()
	body := k.bodyHeight()
	end := min(len(lines), k.scroll+body)
	visible := lines[k.scroll:end]

	title := appstyles.Title.Render("Keybindings")
	pos := ""
	if k.maxScroll() > 0 {
		pos = appstyles.Muted.Render("  (j/k scroll)")
	}
	footer := appstyles.Muted.Render(" esc close")

	content := title + pos + "\n\n" +
		strings.Join(visible, "\n") + "\n\n" + footer
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(appstyles.ColorPrimary).
		Padding(0, 1).
		Width(keysOverlayWidth).
		Render(content)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
