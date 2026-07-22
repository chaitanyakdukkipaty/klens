package app

// Terminal dock: model-side state and input routing for embedded exec
// sessions (internal/k8s/termsession). The dock band renders between the
// middle section and the status bar; the TerminalDock panel (internal/ui/
// panels) is a pure value rebuilt from this state on every render.

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/chaitanyak/klens/internal/k8s/termsession"
	"github.com/chaitanyak/klens/internal/ui/panels"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

// dockKillWindow is how long after a forwarded ctrl+c a second press is
// interpreted as "kill the terminal tab" instead of another SIGINT.
const dockKillWindow = 1500 * time.Millisecond

// dockWheelStep is how many lines one wheel tick scrolls the dock.
const dockWheelStep = 3

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
	body, scroll := "", ""
	if s := m.activeSessionPtr(); s != nil {
		// RenderView falls back to the live fast path when following with no
		// selection, so this stays as cheap as the old Render() in the common
		// case and only does windowed work while scrolling or selecting.
		body = s.RenderView(styles.ColorSelection)
		if !s.Following() {
			pos, total := s.ScrollInfo()
			scroll = fmt.Sprintf("▲ %d/%d", pos, total)
		}
	}
	return panels.TerminalDock{
		Width:     dr.Width,
		Height:    dr.Height,
		Tabs:      tabs,
		Active:    m.activeSession,
		Focused:   m.dockFocus,
		Maximized: m.dockMaximized,
		Body:      body,
		Scroll:    scroll,
	}
}

// dockScrolling reports whether the active session is in scrollback (frozen)
// mode, which routes keyboard input to the scroll handler instead of the shell.
func (m Model) dockScrolling() bool {
	s := m.activeSessionPtr()
	return s != nil && !s.Following()
}

// dockBodyRows is the number of terminal rows visible in the dock body — the
// page size for pgup/pgdn scrolling.
func (m Model) dockBodyRows() int {
	dr := m.layout.Dock()
	_, h := panels.DockBodySize(dr.Width, dr.Height)
	return h
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
// handleDockChromeKey handles the dock's structural controls — hide, resize,
// switch tab, close — which must work whether the session is live or frozen in
// scrollback. handled reports whether the key belonged to the chrome set; when
// false the caller routes to the live or scroll key handlers. Footer help is
// refreshed through dockFooterHelp so the scroll-mode footer is preserved.
func (m Model) handleDockChromeKey(msg tea.KeyPressMsg) (bool, Model, tea.Cmd) {
	switch msg.String() {
	// ctrl+] is a symmetric toggle: it showed+focused the dock on the way in
	// (global switch in handleKey), so here — focused — it hides it again.
	// Releasing focus while keeping the dock visible is a mouse gesture:
	// click anywhere outside the dock.
	case "ctrl+]", "alt+h":
		m.dockHidden = true
		m.dockFocus = false
		m.setStatusBarKind(m.nav.ActiveKind())
		return true, m.resizePanels(), nil
	// shift+↓ mirrors shift+↑: minimise when maximized, hide otherwise.
	case "shift+down":
		if m.dockMaximized {
			m.dockMaximized = false
			m.statusBar = m.statusBar.SetHelp(m.dockFooterHelp())
			return true, m.resizePanels(), nil
		}
		m.dockHidden = true
		m.dockFocus = false
		m.setStatusBarKind(m.nav.ActiveKind())
		return true, m.resizePanels(), nil
	case "shift+up", "alt+m":
		m.dockMaximized = !m.dockMaximized
		m.statusBar = m.statusBar.SetHelp(m.dockFooterHelp())
		return true, m.resizePanels(), nil
	case "alt+w":
		nm, cmd := m.closeSession(m.activeSession)
		return true, nm, cmd
	case "shift+left", "alt+left":
		nm := m.switchSession(m.activeSession - 1)
		nm.statusBar = nm.statusBar.SetHelp(nm.dockFooterHelp())
		return true, nm, nil
	case "shift+right", "alt+right":
		nm := m.switchSession(m.activeSession + 1)
		nm.statusBar = nm.statusBar.SetHelp(nm.dockFooterHelp())
		return true, nm, nil
	case "alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9":
		n, _ := strconv.Atoi(msg.String()[len("alt+"):])
		nm := m.switchSession(n - 1)
		nm.statusBar = nm.statusBar.SetHelp(nm.dockFooterHelp())
		return true, nm, nil
	}
	return false, m, nil
}

// dockFooterHelp is the status-bar footer for the dock's current state: the
// scrollback footer when frozen, the live footer otherwise.
func (m Model) dockFooterHelp() []panels.HelpItem {
	if m.dockScrolling() {
		return m.dockScrollHelp()
	}
	return dockHelp(m.dockMaximized)
}

func (m Model) handleDockKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch msg.String() {
	// shift+pgup enters scrollback (frozen) mode; once in it, handleKey routes
	// to handleDockScrollKey instead. shift+pgdown at the live bottom is a
	// no-op. Plain pgup/pgdown stay forwarded to the shell (less, man, …).
	case "shift+pgup":
		if s := m.activeSessionPtr(); s != nil {
			s.ScrollBy(max(m.dockBodyRows()-1, 1))
			m.statusBar = m.statusBar.SetHelp(m.dockScrollHelp())
		}
		return m, nil
	case "shift+pgdown":
		return m, nil
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
	// Body click: middle-click closes the active tab; left-click anchors a
	// text selection (extended by drag, copied on release — see model.go).
	// Body-local cell: skip the left border (col) and the border+tab-bar rows.
	if click.Button == tea.MouseMiddle {
		return m.closeSession(m.activeSession)
	}
	if click.Button == tea.MouseLeft {
		if s := m.activeSessionPtr(); s != nil {
			s.SelectStart(lx-1, ly-2)
		}
	}
	return m, nil
}

// handleDockWheel routes a wheel tick that landed over the dock band. Over a
// normal shell it drives local scrollback (and takes focus); over an alt-screen
// app it forwards arrow keys so the app scrolls itself.
func (m Model) handleDockWheel(wheel tea.MouseWheelMsg) (Model, tea.Cmd) {
	s := m.activeSessionPtr()
	if s == nil {
		return m, nil
	}
	if !m.dockFocus {
		m.dockFocus = true
	}
	if s.AltScreen() {
		var code rune
		switch wheel.Button {
		case tea.MouseWheelUp:
			code = uv.KeyUp
		case tea.MouseWheelDown:
			code = uv.KeyDown
		default:
			return m, nil
		}
		for i := 0; i < dockWheelStep; i++ {
			s.SendKey(uv.KeyPressEvent{Code: code})
		}
		return m, nil
	}
	switch wheel.Button {
	case tea.MouseWheelUp:
		s.ScrollBy(dockWheelStep)
	case tea.MouseWheelDown:
		s.ScrollBy(-dockWheelStep)
	default:
		return m, nil
	}
	m.statusBar = m.statusBar.SetHelp(m.dockFooterHelp())
	return m, nil
}

// handleDockDrag extends an in-flight body selection. lx/ly are dock-local.
// Dragging above/below the body edge nudges the scrollback so the selection can
// extend past the visible window.
func (m Model) handleDockDrag(lx, ly int) (Model, tea.Cmd) {
	s := m.activeSessionPtr()
	if s == nil || !s.Selecting() {
		return m, nil
	}
	row := ly - 2 // border row + tab-bar row
	switch {
	case row < 0:
		s.ScrollBy(1)
		row = 0
	case row >= m.dockBodyRows():
		s.ScrollBy(-1)
		row = m.dockBodyRows() - 1
	}
	s.SelectExtend(lx-1, row)
	return m, nil
}

// handleDockRelease finishes a body selection, copying it when the drag moved.
func (m Model) handleDockRelease() (Model, tea.Cmd) {
	s := m.activeSessionPtr()
	if s == nil {
		return m, nil
	}
	if s.SelectEnd() {
		return m.copyDockSelection(s)
	}
	return m, nil
}

// handleDockScrollKey drives the dock while the active session is in scrollback
// (frozen) mode. Keys navigate history and copy the selection instead of
// reaching the shell; esc / scrolling back to the bottom resumes the live
// session. Unhandled keys are swallowed so they can't leak into the shell.
func (m Model) handleDockScrollKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	s := m.activeSessionPtr()
	if s == nil {
		return m, nil
	}
	page := max(m.dockBodyRows()-1, 1)
	switch msg.String() {
	case "esc":
		s.ResetView()
		m.statusBar = m.statusBar.SetHelp(dockHelp(m.dockMaximized))
		return m, nil
	case "up", "k":
		s.ScrollBy(1)
	case "down", "j":
		s.ScrollBy(-1)
	case "pgup", "shift+pgup", "ctrl+b":
		s.ScrollBy(page)
	case "pgdown", "shift+pgdown", "ctrl+f", " ":
		s.ScrollBy(-page)
	case "g", "home":
		s.ScrollHome()
	case "G", "end":
		s.ScrollEnd()
	case "y", "c":
		return m.copyDockSelection(s)
	case "enter":
		var cmd tea.Cmd
		if s.HasSelection() {
			m, cmd = m.copyDockSelection(s)
		}
		s.ResetView()
		m.statusBar = m.statusBar.SetHelp(dockHelp(m.dockMaximized))
		return m, cmd
	}
	// Crossing the bottom re-pins to following; reflect that in the footer.
	m.statusBar = m.statusBar.SetHelp(m.dockFooterHelp())
	return m, nil
}

// copyDockSelection writes the active session's selection to the clipboard.
func (m Model) copyDockSelection(s *termsession.Session) (Model, tea.Cmd) {
	text := s.SelectionText()
	if text == "" {
		m.statusBar = m.statusBar.SetMessage("no selection")
		return m, clearStatusAfterDelay(2 * time.Second)
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.statusBar = m.statusBar.SetMessage("copy failed: " + err.Error())
		return m, clearStatusAfterDelay(3 * time.Second)
	}
	n := strings.Count(text, "\n") + 1
	suffix := ""
	if n != 1 {
		suffix = "s"
	}
	m.statusBar = m.statusBar.SetMessage(fmt.Sprintf("copied %d line%s to clipboard", n, suffix))
	return m, clearStatusAfterDelay(3 * time.Second)
}

// dockScrollHelp is the status-bar footer while the dock is in scrollback mode.
// It keeps the maximize/minimise hint so resizing stays discoverable while
// frozen (the chrome keys work in both modes — see handleDockChromeKey).
func (m Model) dockScrollHelp() []panels.HelpItem {
	size := panels.HelpItem{Key: "shift+↑", Desc: "maximize"}
	if m.dockMaximized {
		size = panels.HelpItem{Key: "shift+↓", Desc: "minimise"}
	}
	return []panels.HelpItem{
		{Key: "↑↓/jk", Desc: "scroll"},
		{Key: "g/G", Desc: "top/bottom"},
		{Key: "drag/y", Desc: "copy"},
		size,
		{Key: "esc", Desc: "resume"},
	}
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
		{Key: "shift+pgup", Desc: "scroll"},
		{Key: "ctrl+c ×2", Desc: "kill"},
		{Key: "click out", Desc: "release"},
	}
}
