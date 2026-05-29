package modes

import (
	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// YAMLViewController wraps panels.YAMLViewer. The view mode has no modal
// input states — its only externally-meaningful keys are "e" (open editor)
// and "ctrl+z" (rollback), both of which need root-owned state and are
// refused here so the root's existing handlers fire.
type YAMLViewController struct {
	panel panels.YAMLViewer
}

func NewYAMLViewController(p panels.YAMLViewer) YAMLViewController {
	return YAMLViewController{panel: p}
}

func (c YAMLViewController) Panel() panels.YAMLViewer            { return c.panel }
func (c YAMLViewController) SetPanel(p panels.YAMLViewer) YAMLViewController { c.panel = p; return c }

func (c YAMLViewController) View() string { return c.panel.View() }

func (c YAMLViewController) SetSize(w, h int) Controller {
	c.panel = c.panel.SetSize(w, h)
	return c
}

func (c YAMLViewController) IsDragging() bool { return c.panel.IsDragging() }
func (c YAMLViewController) WheelAtBoundary(button tea.MouseButton) bool {
	return c.panel.WheelAtBoundary(button)
}
func (c YAMLViewController) AutoScrollStep() YAMLViewController {
	c.panel = c.panel.AutoScrollStep()
	return c
}

// RawYAML / ResourceInfo expose the panel's read-only accessors. They mirror
// the panel's API one-for-one so the root never reaches past the controller.
func (c YAMLViewController) RawYAML() string                       { return c.panel.RawYAML() }
func (c YAMLViewController) ResourceInfo() (kind, name, ns string) { return c.panel.ResourceInfo() }

func (c YAMLViewController) Update(msg tea.Msg) (Controller, tea.Cmd) {
	p, cmd := c.panel.Update(msg)
	c.panel = p
	return c, cmd
}

// HandleKey absorbs scroll / drag keys (g, G, hjkl, etc.) and refuses the
// two keys that need to escape to the root: "e" opens the editor (touches
// readOnly + yamlEdit) and "ctrl+z" replays the rollback stash.
func (c YAMLViewController) HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool) {
	switch k.String() {
	case "e", "ctrl+z":
		return c, nil, false
	}
	p, cmd := c.panel.Update(k)
	c.panel = p
	return c, cmd, true
}

// Mouse routing wrappers — root handles the spatial dispatch but writes
// updated panel state back through the controller.
func (c YAMLViewController) HandleMouseDown(x, y int) (YAMLViewController, bool) {
	p, ok := c.panel.HandleMouseDown(x, y)
	c.panel = p
	return c, ok
}

func (c YAMLViewController) HandleMouseDrag(x, y int) YAMLViewController {
	c.panel = c.panel.HandleMouseDrag(x, y)
	return c
}

func (c YAMLViewController) HandleMouseUp(x, y int) (YAMLViewController, string) {
	p, status := c.panel.HandleMouseUp(x, y)
	c.panel = p
	return c, status
}
