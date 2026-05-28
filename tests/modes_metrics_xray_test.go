package klenstests

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/ui/modes"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// TestMetricsControllerKeyConsumed — the controller greedily consumes every
// key in metrics mode (mirrors the historical behaviour: the root's global
// switch already peeled q/ctrl+r/ctrl+n/ctrl+k/esc/F before we got here).
func TestMetricsControllerKeyConsumed(t *testing.T) {
	c := modes.NewMetricsController(panels.NewMetricsPanel(60, 22))
	_, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: 'g'})
	if !consumed {
		t.Fatal("MetricsController.HandleKey returned consumed=false")
	}
}

// TestMetricsControllerSetSizePreservesType — SetSize returns the Controller
// interface but the concrete type stays MetricsController so the root can
// type-assert without losing data.
func TestMetricsControllerSetSizePreservesType(t *testing.T) {
	c := modes.NewMetricsController(panels.NewMetricsPanel(60, 22))
	resized := c.SetSize(120, 40)
	if _, ok := resized.(modes.MetricsController); !ok {
		t.Fatalf("SetSize returned %T, want modes.MetricsController", resized)
	}
}

// TestXRayControllerKeyConsumed — same contract as metrics.
func TestXRayControllerKeyConsumed(t *testing.T) {
	c := modes.NewXRayController(panels.NewXRayPanel(60, 22))
	_, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: 'G'})
	if !consumed {
		t.Fatal("XRayController.HandleKey returned consumed=false")
	}
}

// TestXRayControllerSetTreeReturnsConcrete — the data setter returns the
// concrete controller (not the interface) so callers can chain without
// type-asserting.
func TestXRayControllerSetTreeReturnsConcrete(t *testing.T) {
	c := modes.NewXRayController(panels.NewXRayPanel(60, 22))
	// SetTree accepts nil for "no data available" — covers the path where
	// the user opens xray against a kind that doesn't build a tree.
	next := c.SetTree("Pod", "demo", nil)
	if next.View() == "" {
		t.Fatal("XRay View returned empty string after SetTree")
	}
}
