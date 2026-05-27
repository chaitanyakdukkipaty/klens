package klenstests

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/modes"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// makeLogsController returns a controller seeded with two pod groups so the
// log viewer has a real focused group to drive (SetPodGroups falls back to
// SetPods when there is exactly one group, which paints differently).
func makeLogsController() modes.LogsController {
	p := panels.NewLogViewer(80, 24)
	c := modes.NewLogsController(p).SetPodGroups([]k8s.LogGroup{
		{Name: "deploy/api", Pods: []string{"api-1"}},
		{Name: "deploy/worker", Pods: []string{"worker-1"}},
	})
	return c
}

// TestLogsControllerEscWithoutStateRefuses — ESC with no peelable layer
// returns consumed=false so the root can run its own fullscreen / mode-exit
// cascade. This is the load-bearing contract that lets us delete the
// HasActiveState() leak on the panel without changing user-facing behaviour.
func TestLogsControllerEscWithoutStateRefuses(t *testing.T) {
	c := makeLogsController()
	_, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if consumed {
		t.Fatal("HandleKey returned consumed=true on ESC with no panel state; should fall through to root")
	}
}

// TestLogsControllerEscPeelsFilter — opening a filter then pressing ESC peels
// exactly that layer and reports consumed=true so the root stops routing.
func TestLogsControllerEscPeelsFilter(t *testing.T) {
	c := makeLogsController()
	// Open the filter input via the panel's own "/" handler. After this the
	// panel has filterOn=true → HasActiveState() reports true.
	next, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !consumed {
		t.Fatal("'/' to open filter was not consumed")
	}
	c = next.(modes.LogsController)
	// ESC should peel filterOn and report consumed=true.
	next, _, consumed = c.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !consumed {
		t.Fatal("ESC over open filter was not consumed")
	}
	// A second ESC has nothing left to peel.
	c = next.(modes.LogsController)
	_, _, consumed = c.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if consumed {
		t.Fatal("ESC after peel still consuming; should defer to root")
	}
}

// TestLogsControllerQRefusedOutsideInput — q outside of filter/search input
// returns consumed=false so the root's global quit fires.
func TestLogsControllerQRefusedOutsideInput(t *testing.T) {
	c := makeLogsController()
	_, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if consumed {
		t.Fatal("'q' consumed outside input capture; should let root quit")
	}
}

// TestLogsControllerQConsumedInsideInput — once filter input is open, 'q' is
// literal text and the controller absorbs it instead of letting the root quit.
func TestLogsControllerQConsumedInsideInput(t *testing.T) {
	c := makeLogsController()
	next, _, _ := c.HandleKey(tea.KeyPressMsg{Code: '/', Text: "/"})
	c = next.(modes.LogsController)
	_, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !consumed {
		t.Fatal("'q' inside filter input was not consumed; would quit the app")
	}
}
