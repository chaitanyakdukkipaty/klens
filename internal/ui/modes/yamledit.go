package modes

import (
	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// YAMLEditController wraps panels.YAMLEditor. It owns the vim-style Insert /
// Normal / DiffConfirm state machine that previously leaked through
// IsInsertMode() to the root: the root used to ask the panel whether to
// suppress F (fullscreen toggle) and esc (exit mode) during text entry.
// Now the controller's HandleKey makes that decision and the root only sees
// consumed=true (panel ate the key as input) or consumed=false (the global
// keybinding should run).
type YAMLEditController struct {
	panel panels.YAMLEditor
}

func NewYAMLEditController(p panels.YAMLEditor) YAMLEditController {
	return YAMLEditController{panel: p}
}

func (c YAMLEditController) Panel() panels.YAMLEditor                       { return c.panel }
func (c YAMLEditController) SetPanel(p panels.YAMLEditor) YAMLEditController { c.panel = p; return c }

func (c YAMLEditController) View() string { return c.panel.View() }

func (c YAMLEditController) SetSize(w, h int) Controller {
	c.panel = c.panel.SetSize(w, h)
	return c
}

// LoadYAML seeds the editor for a fresh edit session (Normal mode, content
// loaded as the "original" baseline for diff preview).
func (c YAMLEditController) LoadYAML(kind, name, namespace, content string) YAMLEditController {
	c.panel = c.panel.LoadYAML(kind, name, namespace, content)
	return c
}

func (c YAMLEditController) Original() string { return c.panel.Original() }

func (c YAMLEditController) Update(msg tea.Msg) (Controller, tea.Cmd) {
	p, cmd := c.panel.Update(msg)
	c.panel = p
	return c, cmd
}

// HandleKey routes a keypress respecting the editor's modal state.
//   - In Insert mode, ESC transitions back to Normal (panel handles it) and
//     F / q / ctrl+c are literal characters absorbed by the textarea.
//   - In Normal / DiffConfirm / Applying, ESC falls through to the root so
//     the fullscreen-peel / mode-exit cascade runs, F goes to fullscreen,
//     and q / ctrl+c are still absorbed (the editor explicitly protects the
//     buffer from accidental quits — matches the historical
//     `if m.mode == ModeEditor` route in root.handleKey).
func (c YAMLEditController) HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool) {
	insert := c.panel.IsInsertMode()
	switch k.String() {
	case "esc":
		if insert {
			p, cmd := c.panel.Update(k)
			c.panel = p
			return c, cmd, true
		}
		return c, nil, false
	case "F":
		if insert {
			p, cmd := c.panel.Update(k)
			c.panel = p
			return c, cmd, true
		}
		return c, nil, false
	case "q", "ctrl+c":
		// Editor never lets q / ctrl+c reach the global quit handler — it
		// would destroy in-flight edits. Always route to the panel.
		p, cmd := c.panel.Update(k)
		c.panel = p
		return c, cmd, true
	}
	p, cmd := c.panel.Update(k)
	c.panel = p
	return c, cmd, true
}
