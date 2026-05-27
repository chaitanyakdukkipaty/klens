package klenstests

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// TestYAMLEditorStateMachineNormalToApplying drives the editor through the
// full happy-path: load (Normal) → i (Insert) → esc (Normal) → ctrl+s
// (DiffConfirm) → y (Applying). Each transition is observed through the only
// supported seam — HandleKey + View() — not through any state accessor, since
// plan 5 deleted those.
func TestYAMLEditorStateMachineNormalToApplying(t *testing.T) {
	e := panels.NewYAMLEditor(80, 24).LoadYAML("Pod", "demo", "default", "kind: Pod\nname: demo\n")

	if !strings.Contains(e.View(), "NORMAL") {
		t.Fatal("editor did not start in Normal mode")
	}

	// Normal → Insert via "i".
	e, _, consumed := e.HandleKey(tea.KeyPressMsg{Code: 'i', Text: "i"})
	if !consumed {
		t.Fatal("'i' in Normal mode was not consumed")
	}
	if !strings.Contains(e.View(), "INSERT") {
		t.Fatal("editor did not enter Insert mode after 'i'")
	}

	// Insert → Normal via esc.
	e, _, consumed = e.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !consumed {
		t.Fatal("esc in Insert mode was not consumed")
	}
	if !strings.Contains(e.View(), "NORMAL") {
		t.Fatal("editor did not return to Normal after esc from Insert")
	}

	// Normal → DiffConfirm via ctrl+s.
	e, _, consumed = e.HandleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !consumed {
		t.Fatal("ctrl+s in Normal mode was not consumed")
	}
	if !strings.Contains(e.View(), "apply?") {
		t.Fatal("editor did not enter DiffConfirm after ctrl+s (expected 'apply?' prompt)")
	}

	// DiffConfirm → Applying via y; the cmd is the panel's applyCmd closure.
	e, cmd, consumed := e.HandleKey(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !consumed {
		t.Fatal("'y' in DiffConfirm was not consumed")
	}
	if cmd == nil {
		t.Fatal("DiffConfirm → Applying did not emit an apply cmd")
	}
	if !strings.Contains(e.View(), "Applying") {
		t.Fatal("editor did not transition to Applying after 'y'")
	}
}

// TestYAMLEditorDiffConfirmCancel — 'n' from DiffConfirm returns to Normal
// without emitting an apply cmd.
func TestYAMLEditorDiffConfirmCancel(t *testing.T) {
	e := panels.NewYAMLEditor(80, 24).LoadYAML("Pod", "demo", "default", "kind: Pod\n")

	// Enter DiffConfirm.
	e, _, _ = e.HandleKey(tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl})
	if !strings.Contains(e.View(), "apply?") {
		t.Fatal("ctrl+s did not open DiffConfirm")
	}

	// Cancel with 'n' — should return to Normal mode, no apply cmd.
	e, cmd, consumed := e.HandleKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if !consumed {
		t.Fatal("'n' in DiffConfirm was not consumed")
	}
	if cmd != nil {
		t.Fatal("'n' from DiffConfirm should not emit a cmd")
	}
	if !strings.Contains(e.View(), "NORMAL") {
		t.Fatal("editor did not return to Normal after 'n' from DiffConfirm")
	}
}

// TestYAMLEditorHandleKeyRefusesEscInNormal — Normal-mode ESC is the seam the
// root needs to run its mode-exit cascade; the panel must not consume it.
func TestYAMLEditorHandleKeyRefusesEscInNormal(t *testing.T) {
	e := panels.NewYAMLEditor(80, 24).LoadYAML("Pod", "demo", "default", "kind: Pod\n")
	_, _, consumed := e.HandleKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	if consumed {
		t.Fatal("ESC in Normal mode consumed; root would lose the mode-exit signal")
	}
}

// TestYAMLEditorHandleKeyRefusesFInNormal — F is the global fullscreen toggle
// and must not be eaten by the editor outside Insert mode.
func TestYAMLEditorHandleKeyRefusesFInNormal(t *testing.T) {
	e := panels.NewYAMLEditor(80, 24).LoadYAML("Pod", "demo", "default", "kind: Pod\n")
	_, _, consumed := e.HandleKey(tea.KeyPressMsg{Code: 'F', Text: "F"})
	if consumed {
		t.Fatal("F in Normal mode consumed; root would never see the fullscreen toggle")
	}
}

// TestYAMLEditorHandleKeyConsumesFInInsert — once in Insert mode F is a
// literal character and the editor absorbs it so the buffer isn't disrupted.
func TestYAMLEditorHandleKeyConsumesFInInsert(t *testing.T) {
	e := panels.NewYAMLEditor(80, 24).LoadYAML("Pod", "demo", "default", "kind: Pod\n")
	e, _, _ = e.HandleKey(tea.KeyPressMsg{Code: 'i', Text: "i"})
	_, _, consumed := e.HandleKey(tea.KeyPressMsg{Code: 'F', Text: "F"})
	if !consumed {
		t.Fatal("F in Insert mode not consumed; would trigger fullscreen mid-edit")
	}
}

// TestYAMLEditorHandleKeyConsumesQuitKeys — q and ctrl+c always route to the
// panel so an in-flight edit isn't lost to a global quit.
func TestYAMLEditorHandleKeyConsumesQuitKeys(t *testing.T) {
	e := panels.NewYAMLEditor(80, 24).LoadYAML("Pod", "demo", "default", "kind: Pod\n")
	for _, k := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		_, _, consumed := e.HandleKey(k)
		if !consumed {
			t.Fatalf("editor did not consume %q; would quit the app", k.String())
		}
	}
}
