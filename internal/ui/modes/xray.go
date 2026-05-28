package modes

import (
	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// XRayController wraps panels.XRayPanel as a per-mode controller.
type XRayController struct {
	panel panels.XRayPanel
}

func NewXRayController(p panels.XRayPanel) XRayController {
	return XRayController{panel: p}
}

func (c XRayController) Panel() panels.XRayPanel { return c.panel }

func (c XRayController) Update(msg tea.Msg) (Controller, tea.Cmd) {
	p, cmd := c.panel.Update(msg)
	c.panel = p
	return c, cmd
}

func (c XRayController) View() string { return c.panel.View() }

func (c XRayController) SetSize(w, h int) Controller {
	c.panel = c.panel.SetSize(w, h)
	return c
}

func (c XRayController) HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool) {
	p, cmd := c.panel.Update(k)
	c.panel = p
	return c, cmd, true
}

// SetTree seeds the tree to render and returns the concrete controller so the
// root avoids a type-assert round-trip from the Controller interface.
func (c XRayController) SetTree(kind, name string, root *k8s.TreeNode) XRayController {
	c.panel = c.panel.SetTree(kind, name, root)
	return c
}
