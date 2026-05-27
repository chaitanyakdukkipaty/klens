package klenstests

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/ui/modes"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// TestTableControllerHandleKeyAlwaysConsumes — the table controller is a
// thin pass-through to the panel: action-key dispatch lives in the root,
// so the controller's HandleKey just forwards. Anything that reaches it
// is panel-bound.
func TestTableControllerHandleKeyAlwaysConsumes(t *testing.T) {
	c := modes.NewTableController(panels.NewResourceTable(60, 24))
	_, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: 'j', Text: "j"})
	if !consumed {
		t.Fatal("TableController.HandleKey returned consumed=false; root's table dispatch would loop")
	}
}

// TestTableControllerStepReturnsConcrete — Step is the concrete-typed
// counterpart to Update used by the ~7 model.go sites that forward a
// tea.Msg into the panel. Without Step those sites would need a type
// assertion on every assignment.
func TestTableControllerStepReturnsConcrete(t *testing.T) {
	c := modes.NewTableController(panels.NewResourceTable(60, 24))
	next, _ := c.Step(tea.KeyPressMsg{Code: 'j', Text: "j"})
	// Compile-time check: next is statically TableController, no assertion.
	_ = next.SelectionCount()
}

// TestTableControllerFilterActiveExposed — the root branches on
// FilterActive() to decide whether ESC peels the filter input or exits
// the table. Exposing it through the controller keeps the root from
// reaching into the panel.
func TestTableControllerFilterActiveExposed(t *testing.T) {
	c := modes.NewTableController(panels.NewResourceTable(60, 24))
	if c.FilterActive() {
		t.Fatal("freshly-constructed table reports FilterActive=true")
	}
	// Open the filter via "/" key (panel's own handler).
	next, _ := c.Step(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !next.FilterActive() {
		t.Fatal("FilterActive=false after '/' key; root won't route keys into filter input")
	}
}
