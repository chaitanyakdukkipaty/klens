package klenstests

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// keyEnter is the bubbletea encoding for the Enter key.
var keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}

// pressKeys threads a slice of keys through the panel's HandleKey, asserting
// each one is consumed unless wantRefused is true for that index.
func pressKeys(t *testing.T, v panels.LogViewer, keys []tea.KeyPressMsg) panels.LogViewer {
	t.Helper()
	for _, k := range keys {
		next, _, consumed := v.HandleKey(k)
		if !consumed {
			t.Fatalf("panel refused key %q during setup; would have leaked to root", k.String())
		}
		v = next
	}
	return v
}

// TestLogViewerEscWithoutStateRefuses — the panel exposes no state-introspection,
// so the only signal the root has is consumed=true/false. With nothing to peel
// the panel must refuse ESC so the root's fullscreen / mode-exit cascade runs.
func TestLogViewerEscWithoutStateRefuses(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPods([]string{"api-0"})
	_, _, consumed := v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if consumed {
		t.Fatal("ESC with no panel state consumed; root would never see it")
	}
}

// TestLogViewerEscPeelsFilterInputThenRefuses — opening the filter input and
// pressing ESC peels exactly the input-open layer; a second ESC has nothing
// left to peel and is refused.
func TestLogViewerEscPeelsFilterInputThenRefuses(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPods([]string{"api-0"})

	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: '/', Text: "/"}})

	// First ESC peels filterOn.
	next, _, consumed := v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !consumed {
		t.Fatal("ESC over open filter input not consumed")
	}
	v = next

	// Second ESC has nothing left.
	_, _, consumed = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if consumed {
		t.Fatal("ESC after peel still consuming; root cascade would be skipped")
	}
}

// TestLogViewerEscPeelsSearchInputThenRefuses — same shape as the filter
// variant but for ctrl+f search input.
func TestLogViewerEscPeelsSearchInputThenRefuses(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPods([]string{"api-0"})

	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 'f', Mod: tea.ModCtrl}})

	next, _, consumed := v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !consumed {
		t.Fatal("ESC over open search input not consumed")
	}
	v = next

	_, _, consumed = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if consumed {
		t.Fatal("ESC after search peel still consuming")
	}
}

// TestLogViewerEscPeelsPodSolo — in single-group mode with multiple pods, "1"
// solos the first pod. ESC peels that solo and the next ESC refuses.
func TestLogViewerEscPeelsPodSolo(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPods([]string{"api-0", "api-1"})

	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: '1', Text: "1"}})

	next, _, consumed := v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !consumed {
		t.Fatal("ESC over pod-solo not consumed")
	}
	v = next

	_, _, consumed = v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if consumed {
		t.Fatal("ESC after pod-solo peel still consuming")
	}
}

// TestLogViewerEscPeelOrderMultilayer — commit a filter and a search query
// while in pod-solo mode, then drive ESC repeatedly and assert the panel
// peels exactly four layers (pod-solo → search query → filter query → empty).
// The peel order is the load-bearing UX contract — users dismiss state in
// the order they added it.
func TestLogViewerEscPeelOrderMultilayer(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPods([]string{"api-0", "api-1"})

	// Layer 1: commit a filter (open with "/", type "x", commit with enter).
	v = pressKeys(t, v, []tea.KeyPressMsg{
		{Code: '/', Text: "/"},
		{Code: 'x', Text: "x"},
		keyEnter,
	})
	// Layer 2: commit a search query (open with ctrl+f, type "y", commit with enter).
	v = pressKeys(t, v, []tea.KeyPressMsg{
		{Code: 'f', Mod: tea.ModCtrl},
		{Code: 'y', Text: "y"},
		keyEnter,
	})
	// Layer 3: pod-solo via "1".
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: '1', Text: "1"}})

	// Three peels must all be consumed (one per layer).
	for i := 0; i < 3; i++ {
		next, _, consumed := v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		if !consumed {
			t.Fatalf("peel %d/3 unexpectedly refused; panel state machine lost a layer", i+1)
		}
		v = next
	}

	// Fourth ESC has nothing left and must refuse.
	_, _, consumed := v.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if consumed {
		t.Fatal("fourth ESC consumed; root mode-exit cascade would be skipped")
	}
}

// TestLogViewerMultiTabSwitching — with 2+ pod groups the panel renders tabs.
// "tab" cycles forward, "1"/"2" jump to absolute tab indices, "0" returns to
// the first tab. These are all consumed (no leak to root).
func TestLogViewerMultiTabSwitching(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPodGroups([]k8s.LogGroup{
		{Name: "deploy/api", Pods: []string{"api-0"}},
		{Name: "deploy/worker", Pods: []string{"worker-0"}},
	})

	// View should render both tab names.
	view := v.View()
	if !strings.Contains(view, "deploy/api") || !strings.Contains(view, "deploy/worker") {
		t.Fatalf("multi-tab View() missing tab names; got:\n%s", view)
	}

	// "tab" cycles forward — consumed.
	_, _, consumed := v.HandleKey(tea.KeyPressMsg{Code: tea.KeyTab})
	if !consumed {
		t.Fatal("tab key not consumed in multi-tab mode")
	}
	// "2" jumps to the second tab — consumed.
	_, _, consumed = v.HandleKey(tea.KeyPressMsg{Code: '2', Text: "2"})
	if !consumed {
		t.Fatal("digit key not consumed for tab jump")
	}
	// "0" returns to first tab — consumed.
	_, _, consumed = v.HandleKey(tea.KeyPressMsg{Code: '0', Text: "0"})
	if !consumed {
		t.Fatal("'0' not consumed for tab home")
	}
}

// TestLogViewerQRefusedOutsideInputCapture — without a filter/search input
// open, q must fall through to the root so the global quit fires.
func TestLogViewerQRefusedOutsideInputCapture(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPods([]string{"api-0"})
	_, _, consumed := v.HandleKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if consumed {
		t.Fatal("q consumed without input capture; would defeat global quit")
	}
}

// TestLogViewerQConsumedInsideFilterInput — once "/" opens the filter, q is
// literal text and the panel absorbs it so the global quit doesn't fire mid-input.
func TestLogViewerQConsumedInsideFilterInput(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPods([]string{"api-0"})
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: '/', Text: "/"}})
	_, _, consumed := v.HandleKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !consumed {
		t.Fatal("q inside filter input not consumed; would quit the app mid-type")
	}
}
