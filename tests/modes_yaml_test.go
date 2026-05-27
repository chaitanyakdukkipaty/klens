package klenstests

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/ui/modes"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// TestYAMLViewControllerRefusesEditAndRollback — "e" and "ctrl+z" need
// root-owned state (readOnly flag and the rollback stash). The controller
// must refuse them so the root's handleYAMLViewKeys fires.
func TestYAMLViewControllerRefusesEditAndRollback(t *testing.T) {
	c := modes.NewYAMLViewController(panels.NewYAMLViewer(80, 24))
	for _, key := range []string{"e", "ctrl+z"} {
		var msg tea.KeyPressMsg
		switch key {
		case "e":
			msg = tea.KeyPressMsg{Code: 'e', Text: "e"}
		case "ctrl+z":
			msg = tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}
		}
		_, _, consumed := c.HandleKey(msg)
		if consumed {
			t.Fatalf("YAMLViewController consumed %q; should defer to root", key)
		}
	}
}

// TestYAMLViewControllerConsumesScrollKeys — keys that drive the viewport
// (g/G/jk) are always consumed.
func TestYAMLViewControllerConsumesScrollKeys(t *testing.T) {
	c := modes.NewYAMLViewController(panels.NewYAMLViewer(80, 24))
	for _, key := range []rune{'g', 'G', 'j', 'k'} {
		_, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: key, Text: string(key)})
		if !consumed {
			t.Fatalf("scroll key %q was not consumed", key)
		}
	}
}

// TestYAMLEditControllerEscPathBranchesOnInsertMode — in Insert mode ESC
// transitions to Normal (panel consumes); in Normal mode ESC falls through
// to the root so the existing mode-exit cascade fires.
func TestYAMLEditControllerEscPathBranchesOnInsertMode(t *testing.T) {
	c := modes.NewYAMLEditController(panels.NewYAMLEditor(80, 24).LoadYAML("Pod", "demo", "default", "kind: Pod\n"))

	// Normal mode → ESC refused.
	_, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if consumed {
		t.Fatal("ESC in Normal mode consumed; should defer to root")
	}

	// Enter Insert mode via "i", then ESC should be consumed (panel
	// transitions back to Normal).
	next, _, _ := c.HandleKey(tea.KeyPressMsg{Code: 'i', Text: "i"})
	c = next.(modes.YAMLEditController)
	_, _, consumed = c.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !consumed {
		t.Fatal("ESC in Insert mode was not consumed; would exit edit mode")
	}
}

// TestYAMLEditControllerFConsumedInInsert — F is fullscreen toggle in
// Normal mode (refused) but literal text in Insert mode (consumed).
func TestYAMLEditControllerFConsumedInInsert(t *testing.T) {
	c := modes.NewYAMLEditController(panels.NewYAMLEditor(80, 24).LoadYAML("Pod", "demo", "default", "kind: Pod\n"))

	_, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: 'F', Text: "F"})
	if consumed {
		t.Fatal("F in Normal mode consumed; root would never see it")
	}

	next, _, _ := c.HandleKey(tea.KeyPressMsg{Code: 'i', Text: "i"})
	c = next.(modes.YAMLEditController)
	_, _, consumed = c.HandleKey(tea.KeyPressMsg{Code: 'F', Text: "F"})
	if !consumed {
		t.Fatal("F in Insert mode was not consumed; would trigger fullscreen mid-edit")
	}
}

// TestYAMLEditControllerQConsumedAlways — q never quits the app from the
// editor (the panel absorbs it to protect in-flight edits). This matches the
// unconditional `if m.mode == ModeEditor` route in the original root.
func TestYAMLEditControllerQConsumedAlways(t *testing.T) {
	c := modes.NewYAMLEditController(panels.NewYAMLEditor(80, 24).LoadYAML("Pod", "demo", "default", "kind: Pod\n"))
	_, _, consumed := c.HandleKey(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !consumed {
		t.Fatal("q in editor (Normal mode) was not consumed; would quit the app")
	}
}
