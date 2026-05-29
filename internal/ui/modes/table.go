package modes

import (
	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// TableController wraps panels.ResourceTable as a per-mode controller. The
// action keys (y/e/l/t/m/d/a/s/r/f/F) and focus-shift keys (enter, left)
// stay in the root's handleTableKeys because they touch model state the
// controller does not own (clusterMgr, watcher, confirm dialog, pendingOp).
// The controller is intentionally thin — it routes the keys that *do* go to
// the panel (jk, g/G, /, esc, space, etc.) and exposes accessors so the
// root never has to reach past the controller to read selection / filter
// state.
//
// The "Kind module" follow-up (docs/architecture/01-kind-module.md) is what
// would let the row builder move inside this controller. Until then, the
// root keeps owning row builds via listRows + WithRows.
type TableController struct {
	panel panels.ResourceTable
}

func NewTableController(p panels.ResourceTable) TableController {
	return TableController{panel: p}
}

func (c TableController) Panel() panels.ResourceTable { return c.panel }
func (c TableController) SetPanel(p panels.ResourceTable) TableController {
	c.panel = p
	return c
}

func (c TableController) View() string { return c.panel.View() }

func (c TableController) SetSize(w, h int) Controller {
	c.panel = c.panel.SetSize(w, h)
	return c
}

func (c TableController) Update(msg tea.Msg) (Controller, tea.Cmd) {
	return c.Step(msg)
}

// Step is the concrete-typed counterpart to Update. The table mode has
// ~7 call sites that forward a tea.Msg to the panel — going through the
// Controller interface would force a type assertion at each site. Step
// preserves the controller value type so `m.tableCtrl, cmd = m.tableCtrl.Step(msg)`
// compiles directly.
func (c TableController) Step(msg tea.Msg) (TableController, tea.Cmd) {
	p, cmd := c.panel.Update(msg)
	c.panel = p
	return c, cmd
}

// HandleKey routes a keypress. Because action keys live in the root, the
// only thing the table mode reliably owns is the filter-input state — when
// it's open the panel consumes every key, otherwise the root's
// handleTableKeys decides what to do based on the panel's read-only
// accessors below.
//
// This controller intentionally does NOT refuse action keys; the root
// chooses whether to call HandleKey at all (see model.handleTableKeys for
// the dispatch order).
func (c TableController) HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool) {
	p, cmd := c.panel.Update(k)
	c.panel = p
	return c, cmd, true
}

// --- Pass-through accessors (read-only state the root branches on) ---

func (c TableController) FilterActive() bool   { return c.panel.FilterActive() }
func (c TableController) HasFilter() bool      { return c.panel.HasFilter() }
func (c TableController) HasHScroll() bool     { return c.panel.HasHScroll() }
func (c TableController) WrapActive() bool     { return c.panel.WrapActive() }
func (c TableController) IsDragging() bool     { return c.panel.IsDragging() }
func (c TableController) SelectionCount() int  { return c.panel.SelectionCount() }
func (c TableController) WheelAtBoundary(button tea.MouseButton) bool {
	return c.panel.WheelAtBoundary(button)
}
func (c TableController) SelectedRow() *k8s.ResourceRow {
	return c.panel.SelectedRow()
}
func (c TableController) SelectedRows() []*k8s.ResourceRow { return c.panel.SelectedRows() }
func (c TableController) SelectedPods() []string           { return c.panel.SelectedPods() }

// --- Pass-through mutators (chainable concrete returns) ---

func (c TableController) ClearSelection() TableController {
	c.panel = c.panel.ClearSelection()
	return c
}

func (c TableController) SetFocused(f bool) TableController {
	c.panel = c.panel.SetFocused(f)
	return c
}

func (c TableController) SetSyncing(s bool) TableController {
	c.panel = c.panel.SetSyncing(s)
	return c
}

func (c TableController) SetKind(kind string) TableController {
	c.panel = c.panel.SetKind(kind)
	return c
}

func (c TableController) SetWrapColumn(idx int) TableController {
	c.panel = c.panel.SetWrapColumn(idx)
	return c
}

func (c TableController) ClearWrapColumn() TableController {
	c.panel = c.panel.ClearWrapColumn()
	return c
}

func (c TableController) SetTitleBadge(s string) TableController {
	c.panel = c.panel.SetTitleBadge(s)
	return c
}

func (c TableController) CursorToName(name string) (TableController, bool) {
	p, ok := c.panel.CursorToName(name)
	c.panel = p
	return c, ok
}

func (c TableController) WithRows(rows []k8s.ResourceRow) TableController {
	c.panel = c.panel.WithRows(rows)
	return c
}

func (c TableController) PatchValuesByName(rows []k8s.ResourceRow) TableController {
	c.panel = c.panel.PatchValuesByName(rows)
	return c
}

func (c TableController) AutoScrollStep() TableController {
	c.panel = c.panel.AutoScrollStep()
	return c
}

// --- Mouse routing wrappers ---

func (c TableController) HandleMouseDown(x, y int) (TableController, bool) {
	p, ok := c.panel.HandleMouseDown(x, y)
	c.panel = p
	return c, ok
}

func (c TableController) HandleMouseDrag(x, y int) TableController {
	c.panel = c.panel.HandleMouseDrag(x, y)
	return c
}

func (c TableController) HandleMouseUp(x, y int) (TableController, string) {
	p, status := c.panel.HandleMouseUp(x, y)
	c.panel = p
	return c, status
}

func (c TableController) HandleClickAt(y int, leftClick bool) (TableController, bool) {
	p, hit := c.panel.HandleClickAt(y, leftClick)
	c.panel = p
	return c, hit
}
