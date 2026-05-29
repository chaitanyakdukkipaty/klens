package modes

import (
	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// MetricsController wraps panels.MetricsPanel as a per-mode controller. The
// panel still owns its viewport + chart rendering; the controller is the
// routing seam the root uses while ModeMetrics is active.
type MetricsController struct {
	panel panels.MetricsPanel
}

// NewMetricsController seeds a controller from a freshly-sized panel.
func NewMetricsController(p panels.MetricsPanel) MetricsController {
	return MetricsController{panel: p}
}

// Panel returns the wrapped panel for callers that read panel-only state
// (e.g. testing). Mutations go through the controller's own setters.
func (c MetricsController) Panel() panels.MetricsPanel { return c.panel }

func (c MetricsController) WheelAtBoundary(button tea.MouseButton) bool {
	return c.panel.WheelAtBoundary(button)
}

func (c MetricsController) Update(msg tea.Msg) (Controller, tea.Cmd) {
	p, cmd := c.panel.Update(msg)
	c.panel = p
	return c, cmd
}

func (c MetricsController) View() string { return c.panel.View() }

func (c MetricsController) SetSize(w, h int) Controller {
	c.panel = c.panel.SetSize(w, h)
	return c
}

// HandleKey forwards every key to the panel. Metrics mode has no global-key
// passthrough requirements of its own — the root's pre-controller switch
// already peels q / ctrl+r / ctrl+n / ctrl+o / esc / F before the key
// reaches the controller, so anything that arrives here is panel-bound.
func (c MetricsController) HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool) {
	p, cmd := c.panel.Update(k)
	c.panel = p
	return c, cmd, true
}

// SetResource and SetLimits expose the panel's data setters with the
// controller as the return type so the root never has to type-assert
// back from Controller when seeding a new metrics view.
func (c MetricsController) SetResource(name, namespace string, rm *k8s.ResourceMetrics) MetricsController {
	c.panel = c.panel.SetResource(name, namespace, rm)
	return c
}

func (c MetricsController) SetLimits(cpuReqM, cpuLimM, memReqB, memLimB int64) MetricsController {
	c.panel = c.panel.SetLimits(cpuReqM, cpuLimM, memReqB, memLimB)
	return c
}
