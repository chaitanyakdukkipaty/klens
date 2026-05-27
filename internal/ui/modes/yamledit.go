package modes

import (
	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// YAMLEditController wraps panels.YAMLEditor. The vim-style Insert / Normal /
// DiffConfirm state machine lives inside the panel itself — the controller is
// a thin forwarder so the root sees the same Controller surface as every
// other mode. There used to be an IsInsertMode() accessor that leaked the
// editor's state to the root (and then to this controller); that accessor
// has been removed and the panel's HandleKey makes the consume/refuse call.
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

// HandleKey forwards the key to the panel's own HandleKey, which owns the
// modal routing. The controller stays out of the decision so adding a new
// editor state (e.g. visual-select) is a panel-only change.
func (c YAMLEditController) HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool) {
	p, cmd, consumed := c.panel.HandleKey(k)
	c.panel = p
	return c, cmd, consumed
}
