package app

// Terminal dock: model-side state and input routing for embedded exec
// sessions (internal/k8s/termsession). The dock band renders between the
// middle section and the status bar; the TerminalDock panel (internal/ui/
// panels) is a pure value rebuilt from this state on every render.

import (
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/chaitanyak/klens/internal/k8s/termsession"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// dockKillWindow is how long after a forwarded ctrl+c a second press is
// interpreted as "kill the terminal tab" instead of another SIGINT.
const dockKillWindow = 1500 * time.Millisecond

// dockVisible reports whether the dock band is on screen.
func (m Model) dockVisible() bool {
	return len(m.sessions) > 0 && !m.dockHidden
}

// dockHeightWanted is the dock band height for the current state, fed into
// layout.WithDockHeight by resizePanels.
func (m Model) dockHeightWanted() int {
	if !m.dockVisible() {
		return 0
	}
	return m.layout.DockHeightFor(m.dockMaximized)
}

// activeSessionPtr returns the active session, nil when none.
func (m Model) activeSessionPtr() *termsession.Session {
	if m.activeSession < 0 || m.activeSession >= len(m.sessions) {
		return nil
	}
	return m.sessions[m.activeSession]
}

// sessionIndex locates a session in the registry, -1 when absent (already
// closed by the user).
func (m Model) sessionIndex(s *termsession.Session) int {
	for i, cur := range m.sessions {
		if cur == s {
			return i
		}
	}
	return -1
}

// buildDock flattens the session registry into the render-time dock value.
func (m Model) buildDock() panels.TerminalDock {
	dr := m.layout.Dock()
	tabs := make([]panels.DockTab, len(m.sessions))
	for i, s := range m.sessions {
		st, _ := s.Info()
		tabs[i] = panels.DockTab{Title: s.Title(), Exited: st == termsession.StatusExited}
	}
	body := ""
	if s := m.activeSessionPtr(); s != nil {
		body = s.Render()
	}
	return panels.TerminalDock{
		Width:     dr.Width,
		Height:    dr.Height,
		Tabs:      tabs,
		Active:    m.activeSession,
		Focused:   m.dockFocus,
		Maximized: m.dockMaximized,
		Body:      body,
	}
}

// handleDockKey routes keyboard input while the dock has focus. A small
// escape set is consumed by the dock; everything else — including esc, tab,
// arrows, ctrl+c, ctrl+d — is forwarded to the active session's shell.
//
// Escape-set choices: ctrl+[ is the ESC byte and must never be bound; ctrl+t
// / ctrl+b / ctrl+p are readline-active and stay forwarded. The primary set
// is ctrl+] + shift+arrows because they reach the app on every terminal; the
// alt-chords are a bonus layer that only works where Alt sends Meta (on
// macOS that needs "Option as Meta" / "Esc+" enabled), so nothing essential
// lives on alt alone.
func (m Model) handleDockKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch msg.String() {
	// ctrl+] is a symmetric toggle: it showed+focused the dock on the way in
	// (global switch in handleKey), so here — focused — it hides it again.
	// Releasing focus while keeping the dock visible is a mouse gesture:
	// click anywhere outside the dock.
	case "ctrl+]", "alt+h":
		m.dockHidden = true
		m.dockFocus = false
		m.setStatusBarKind(m.nav.ActiveKind())
		return m.resizePanels(), nil
	// shift+↓ mirrors shift+↑: minimise when maximized, hide otherwise.
	case "shift+down":
		if m.dockMaximized {
			m.dockMaximized = false
			m.statusBar = m.statusBar.SetHelp(dockHelp(false))
			return m.resizePanels(), nil
		}
		m.dockHidden = true
		m.dockFocus = false
		m.setStatusBarKind(m.nav.ActiveKind())
		return m.resizePanels(), nil
	case "shift+up", "alt+m":
		m.dockMaximized = !m.dockMaximized
		m.statusBar = m.statusBar.SetHelp(dockHelp(m.dockMaximized))
		return m.resizePanels(), nil
	case "alt+w":
		return m.closeSession(m.activeSession)
	// ctrl+c stays SIGINT (interrupt the running command) — but a second
	// press inside dockKillWindow kills the tab. Hammering ctrl+c at a hung
	// session therefore lands on the right outcome: the session dies.
	case "ctrl+c":
		if time.Since(m.lastDockCtrlC) < dockKillWindow {
			m.lastDockCtrlC = time.Time{}
			pod := ""
			if s := m.activeSessionPtr(); s != nil {
				pod = s.Pod
			}
			var cmd tea.Cmd
			m, cmd = m.closeSession(m.activeSession)
			m.statusBar = m.statusBar.SetMessage("terminal killed: " + pod)
			return m, tea.Batch(cmd, clearStatusAfterDelay(3*time.Second))
		}
		m.lastDockCtrlC = time.Now()
		if s := m.activeSessionPtr(); s != nil {
			s.SendKey(uv.KeyPressEvent{Code: 'c', Mod: uv.ModCtrl})
		}
		m.statusBar = m.statusBar.SetMessage("ctrl+c again to kill the terminal")
		return m, clearStatusAfterDelay(2 * time.Second)
	case "shift+left", "alt+left":
		return m.switchSession(m.activeSession - 1), nil
	case "shift+right", "alt+right":
		return m.switchSession(m.activeSession + 1), nil
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9":
		n, _ := strconv.Atoi(msg.String()[len("alt+"):])
		return m.switchSession(n - 1), nil
	}

	s := m.activeSessionPtr()
	if s == nil {
		return m, nil
	}
	// Printable input goes through SendText: the emulator's SendKey encodes
	// Key.Code only (it drops Key.Text), which would lose shifted symbols
	// and non-latin layouts. Everything with a modifier or a special code
	// (enter, tab, esc, arrows, ctrl+*) goes through SendKey's encoder.
	if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt|tea.ModMeta) == 0 {
		s.SendText(msg.Text)
	} else {
		s.SendKey(uv.KeyPressEvent(uv.Key(tea.Key(msg))))
	}
	return m, nil
}

// switchSession activates the session at idx (clamped into range; wraps are
// deliberate non-goals — alt+left on the first tab stays put).
func (m Model) switchSession(idx int) Model {
	if len(m.sessions) == 0 {
		return m
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.sessions) {
		idx = len(m.sessions) - 1
	}
	m.activeSession = idx
	return m
}

// closeSession cancels and removes the session at idx. Closing the last tab
// hides the dock and returns input to the resource view.
func (m Model) closeSession(idx int) (Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.sessions) {
		return m, nil
	}
	m.sessions[idx].Close()
	m.sessions = append(m.sessions[:idx:idx], m.sessions[idx+1:]...)
	switch {
	case len(m.sessions) == 0:
		m.activeSession = -1
		m.dockFocus = false
		m.dockHidden = false
		m.dockMaximized = false
		m.setStatusBarKind(m.nav.ActiveKind())
	case m.activeSession > idx:
		m.activeSession--
	case m.activeSession >= len(m.sessions):
		m.activeSession = len(m.sessions) - 1
	}
	return m.resizePanels(), nil
}

// handleDockClick routes a mouse click that landed in the dock band.
// lx/ly are dock-local: (0,0) is the dock's outer top-left (border corner);
// the tab bar sits on row 1, the terminal body below it.
func (m Model) handleDockClick(click tea.MouseClickMsg, lx, ly int) (Model, tea.Cmd) {
	if !m.dockFocus {
		m.dockFocus = true
		m.statusBar = m.statusBar.SetHelp(dockHelp(m.dockMaximized))
	}
	d := m.buildDock()
	if ly == 1 { // tab bar row (row 0 is the top border)
		bx := lx - 1 // bar-local: skip the left border column
		if d.MaxButtonAt(bx) {
			m.dockMaximized = !m.dockMaximized
			m.statusBar = m.statusBar.SetHelp(dockHelp(m.dockMaximized))
			return m.resizePanels(), nil
		}
		if idx, closeHit, ok := d.TabAt(bx); ok {
			if closeHit || click.Button == tea.MouseMiddle {
				return m.closeSession(idx)
			}
			if click.Button == tea.MouseLeft {
				return m.switchSession(idx), nil
			}
			return m, nil
		}
		return m, nil
	}
	// Body click: middle-click closes the active tab; left-click only takes
	// focus (forwarding clicks into the remote shell is a follow-up).
	if click.Button == tea.MouseMiddle {
		return m.closeSession(m.activeSession)
	}
	return m, nil
}

// dockHelp is the status-bar footer while the dock has focus. The size
// hint tracks the dock state: maximize when docked, minimise when maximized.
func dockHelp(maximized bool) []panels.HelpItem {
	size := panels.HelpItem{Key: "shift+↑", Desc: "maximize"}
	if maximized {
		size = panels.HelpItem{Key: "shift+↓", Desc: "minimise"}
	}
	return []panels.HelpItem{
		{Key: "ctrl+]", Desc: "hide"},
		{Key: "shift+←/→", Desc: "switch"},
		size,
		{Key: "ctrl+c ×2", Desc: "kill"},
		{Key: "click out", Desc: "release"},
	}
}
