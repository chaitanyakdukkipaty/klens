package modes

import (
	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// LogsController wraps panels.LogViewer. The layered esc-peel and the
// input-capture detection that used to drive routing live inside the panel
// itself now — the controller is a thin forwarder so the root sees the same
// Controller surface as every other mode. There used to be HasActiveState /
// HandleEsc / IsCapturingInput accessors that leaked the viewer's state to
// the root (and then to this controller); those accessors have been removed
// and the panel's HandleKey makes the consume/refuse call.
//
// The log streamer lifecycle stays on the root for now — see
// docs/architecture/02-mode-controllers.md "Risks → logStreamer ownership"
// for the rationale for keeping that crossing where it is.
type LogsController struct {
	panel panels.LogViewer
}

func NewLogsController(p panels.LogViewer) LogsController {
	return LogsController{panel: p}
}

// Panel returns the wrapped panel for mouse routing in the root model. Mouse
// gestures are spatial, not key-shaped, so the root keeps a centralised mouse
// dispatch; SetPanel writes the updated panel back into the controller.
func (c LogsController) Panel() panels.LogViewer            { return c.panel }
func (c LogsController) SetPanel(p panels.LogViewer) LogsController { c.panel = p; return c }

func (c LogsController) View() string { return c.panel.View() }

func (c LogsController) SetSize(w, h int) Controller {
	c.panel = c.panel.SetSize(w, h)
	return c
}

// SetPodGroups seeds the viewer with one tab per log group (or a single
// untabbed group when there is only one). Concrete return type so the
// root avoids a type-assert.
func (c LogsController) SetPodGroups(groups []k8s.LogGroup) LogsController {
	c.panel = c.panel.SetPodGroups(groups)
	return c
}

func (c LogsController) IsDragging() bool { return c.panel.IsDragging() }

func (c LogsController) AutoScrollStep() LogsController {
	c.panel = c.panel.AutoScrollStep()
	return c
}

func (c LogsController) Update(msg tea.Msg) (Controller, tea.Cmd) {
	p, cmd := c.panel.Update(msg)
	c.panel = p
	return c, cmd
}

// HandleKey forwards the key to the panel's own HandleKey, which owns the
// peel order and the input-capture decision. The controller stays out of
// it so adding a new layer (e.g. column-mode) is a panel-only change.
func (c LogsController) HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool) {
	p, cmd, consumed := c.panel.HandleKey(k)
	c.panel = p
	return c, cmd, consumed
}

// ConsumeStatusMsg drains the panel's transient status (drag-to-copy "copied
// N chars", search "no matches", etc.) and returns the updated controller
// alongside the text. The status field is mutated, so the caller must
// reassign the returned controller back into the root.
func (c LogsController) ConsumeStatusMsg() (LogsController, string) {
	s := c.panel.ConsumeStatusMsg()
	return c, s
}

// HandleMouseDown / HandleMouseDrag / HandleMouseUp / HandleClickAt mirror
// the panel's mouse API. They keep the controller as the routing surface so
// the root never re-reads m.logsCtrl.Panel() between calls.
func (c LogsController) HandleMouseDown(x, y int) (LogsController, bool) {
	p, ok := c.panel.HandleMouseDown(x, y)
	c.panel = p
	return c, ok
}

func (c LogsController) HandleMouseDrag(x, y int) LogsController {
	c.panel = c.panel.HandleMouseDrag(x, y)
	return c
}

func (c LogsController) HandleMouseUp(x, y int) (LogsController, string) {
	p, status := c.panel.HandleMouseUp(x, y)
	c.panel = p
	return c, status
}

func (c LogsController) HandleClickAt(x, y int) (LogsController, bool) {
	p, hit := c.panel.HandleClickAt(x, y)
	c.panel = p
	return c, hit
}
