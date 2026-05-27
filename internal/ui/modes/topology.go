package modes

import (
	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// TopologyController wraps panels.TopologyPanel as a per-mode controller.
type TopologyController struct {
	panel panels.TopologyPanel
}

func NewTopologyController(p panels.TopologyPanel) TopologyController {
	return TopologyController{panel: p}
}

func (c TopologyController) Panel() panels.TopologyPanel { return c.panel }

func (c TopologyController) Update(msg tea.Msg) (Controller, tea.Cmd) {
	p, cmd := c.panel.Update(msg)
	c.panel = p
	return c, cmd
}

func (c TopologyController) View() string { return c.panel.View() }

func (c TopologyController) SetSize(w, h int) Controller {
	c.panel = c.panel.SetSize(w, h)
	return c
}

func (c TopologyController) HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool) {
	p, cmd := c.panel.Update(k)
	c.panel = p
	return c, cmd, true
}

// SetTree seeds the tree to render and returns the concrete controller so the
// root avoids a type-assert round-trip from the Controller interface.
func (c TopologyController) SetTree(kind, name string, root *k8s.TreeNode) TopologyController {
	c.panel = c.panel.SetTree(kind, name, root)
	return c
}
