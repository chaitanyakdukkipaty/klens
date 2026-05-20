package widgets

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	k8sres "github.com/chaitanyak/klens/internal/k8s"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// PortForwardListAction is emitted when the user interacts with the list.
type PortForwardListAction struct {
	Stop      bool   // user pressed d/x on a row
	SessionID string // the row's session ID
	Closed    bool   // user dismissed the list
}

// PortForwardList is a modal listing active port-forward sessions. It is
// driven by a snapshot from the manager — the model passes a fresh slice via
// Show whenever the list is opened or after a row is stopped.
type PortForwardList struct {
	visible  bool
	sessions []*k8sres.PortForwardSession
	cursor   int
}

func NewPortForwardList() PortForwardList { return PortForwardList{} }

func (l PortForwardList) Show(sessions []*k8sres.PortForwardSession) PortForwardList {
	l.visible = true
	l.sessions = sessions
	if l.cursor >= len(sessions) {
		l.cursor = max(0, len(sessions)-1)
	}
	return l
}

func (l PortForwardList) Hide() PortForwardList {
	l.visible = false
	return l
}

func (l PortForwardList) IsVisible() bool { return l.visible }

// SetSessions refreshes the snapshot while the list is visible — used after
// the model stops a session so the row disappears immediately.
func (l PortForwardList) SetSessions(sessions []*k8sres.PortForwardSession) PortForwardList {
	l.sessions = sessions
	if l.cursor >= len(sessions) {
		l.cursor = max(0, len(sessions)-1)
	}
	return l
}

func (l PortForwardList) Update(msg tea.Msg) (PortForwardList, tea.Cmd) {
	if !l.visible {
		return l, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return l, nil
	}
	switch key.String() {
	case "esc", "ctrl+f", "q":
		l.visible = false
		return l, func() tea.Msg { return PortForwardListAction{Closed: true} }

	case "up", "k":
		if l.cursor > 0 {
			l.cursor--
		}
		return l, nil

	case "down", "j":
		if l.cursor < len(l.sessions)-1 {
			l.cursor++
		}
		return l, nil

	case "d", "x":
		if l.cursor < 0 || l.cursor >= len(l.sessions) {
			return l, nil
		}
		id := l.sessions[l.cursor].ID
		return l, func() tea.Msg { return PortForwardListAction{Stop: true, SessionID: id} }
	}
	return l, nil
}

func (l PortForwardList) View() string {
	if !l.visible {
		return ""
	}
	title := appstyles.Warning.Bold(true).Render(
		fmt.Sprintf("  Port Forwards (%d active)", len(l.sessions)))

	var body string
	if len(l.sessions) == 0 {
		body = appstyles.Muted.Render("  (none — press shift+f on a pod to start one)")
	} else {
		header := appstyles.Primary.Bold(true).Render(
			fmt.Sprintf("  %-3s %-12s %-12s %s", "", "LOCAL", "REMOTE", "RESOURCE"))
		lines := []string{header}
		for i, s := range l.sessions {
			mark := " "
			if i == l.cursor {
				mark = appstyles.Primary.Render("›")
			}
			line := fmt.Sprintf("  %s  %-12s %-12s %s",
				mark,
				fmt.Sprintf("127.0.0.1:%d", s.LocalPort),
				fmt.Sprintf(":%d", s.RemotePort),
				s.Kind+"/"+s.Resource,
			)
			if i == l.cursor {
				line = appstyles.Primary.Render(line)
			}
			lines = append(lines, line)
		}
		body = strings.Join(lines, "\n")
	}

	hint := appstyles.Muted.Render("  [↑↓/jk] navigate  [d] stop  [esc] close")

	box := appstyles.DialogBox.Render(strings.Join([]string{
		title, "", body, "", hint,
	}, "\n"))
	return lipgloss.Place(80, 16, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(appstyles.ColorAbyss)))
}
