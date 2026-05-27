package modes

import (
	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// LogsController wraps panels.LogViewer. It owns the layered esc-peel and
// input-capture detection that previously leaked through accessors on the
// panel (HasActiveState / HandleEsc / IsCapturingInput) to the root model.
// The root delegates ESC and q/ctrl+c through HandleKey, and the controller
// answers consumed=true when it ate the key or consumed=false when the root
// should run its own fall-through (fullscreen peel, quit, etc.).
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

// HandleKey routes a keypress, hiding the panel's layered state from the
// root. The peel order is preserved: ESC peels exactly one layer of panel
// state; with no peelable state ESC returns consumed=false so the root can
// continue its fullscreen → mode-exit cascade. q / ctrl+c / F are forwarded
// to the panel only while an input capture (filter / search) is open;
// otherwise they fall through so the global keybindings still work.
func (c LogsController) HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool) {
	switch k.String() {
	case "esc":
		if c.panel.HasActiveState() {
			c.panel = c.panel.HandleEsc()
			return c, nil, true
		}
		return c, nil, false
	case "q", "ctrl+c", "F":
		if c.panel.IsCapturingInput() {
			p, cmd := c.panel.Update(k)
			c.panel = p
			return c, cmd, true
		}
		return c, nil, false
	}
	p, cmd := c.panel.Update(k)
	c.panel = p
	return c, cmd, true
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
