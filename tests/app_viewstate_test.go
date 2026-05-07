package klenstests

import (
	"testing"

	"github.com/chaitanyak/klens/internal/app"
)

// TestResetToTable returns to the resource table from any state. The
// invariant is that no matter where the operator was — a fullscreen log
// viewer, an open YAML editor, focused on content — they end up on the
// table with focus on the nav panel and fullscreen off.
func TestResetToTable(t *testing.T) {
	cases := []app.ViewState{
		{Mode: app.ModeYAML, Focus: app.FocusContent, FullScreen: true},
		{Mode: app.ModeLogs, Focus: app.FocusContent, FullScreen: false},
		{Mode: app.ModeEditor, Focus: app.FocusNav, FullScreen: true},
		{Mode: app.ModeTable, Focus: app.FocusContent, FullScreen: false},
	}
	for _, in := range cases {
		got := in.ResetToTable()
		if got.Mode != app.ModeTable {
			t.Errorf("from %+v: mode=%v want ModeTable", in, got.Mode)
		}
		if got.Focus != app.FocusNav {
			t.Errorf("from %+v: focus=%v want FocusNav", in, got.Focus)
		}
		if got.FullScreen {
			t.Errorf("from %+v: fullScreen still on after reset", in)
		}
	}
}

// TestEnterContentMode moves focus to content and changes mode. FullScreen
// is preserved so the operator can hop between modes without losing it.
func TestEnterContentMode(t *testing.T) {
	in := app.ViewState{Mode: app.ModeTable, Focus: app.FocusNav, FullScreen: true}
	got := in.EnterContentMode(app.ModeYAML)
	if got.Mode != app.ModeYAML {
		t.Errorf("mode=%v want ModeYAML", got.Mode)
	}
	if got.Focus != app.FocusContent {
		t.Errorf("focus=%v want FocusContent", got.Focus)
	}
	if !got.FullScreen {
		t.Errorf("fullScreen lost when transitioning between content modes")
	}
}

// TestToggleFullScreenSuppressedOnTable — fullscreen has no meaning when the
// table is the active surface, so toggling is a no-op.
func TestToggleFullScreenSuppressedOnTable(t *testing.T) {
	in := app.ViewState{Mode: app.ModeTable, Focus: app.FocusContent, FullScreen: false}
	got := in.ToggleFullScreen()
	if got.FullScreen {
		t.Error("ToggleFullScreen flipped fullscreen on while in table mode")
	}
}

// TestToggleFullScreenInContentMode flips the flag for non-table modes.
func TestToggleFullScreenInContentMode(t *testing.T) {
	in := app.ViewState{Mode: app.ModeYAML, Focus: app.FocusContent, FullScreen: false}
	got := in.ToggleFullScreen()
	if !got.FullScreen {
		t.Error("ToggleFullScreen failed to flip fullscreen on")
	}
	got2 := got.ToggleFullScreen()
	if got2.FullScreen {
		t.Error("ToggleFullScreen failed to flip fullscreen off")
	}
}

// TestExitFullScreen preserves mode and focus, sets fullScreen to false.
func TestExitFullScreen(t *testing.T) {
	in := app.ViewState{Mode: app.ModeLogs, Focus: app.FocusContent, FullScreen: true}
	got := in.ExitFullScreen()
	if got.Mode != app.ModeLogs || got.Focus != app.FocusContent || got.FullScreen {
		t.Errorf("ExitFullScreen changed unrelated fields: %+v", got)
	}
}

// TestFocusTransitions only change focus; mode + fullScreen preserved.
func TestFocusTransitions(t *testing.T) {
	in := app.ViewState{Mode: app.ModeYAML, Focus: app.FocusContent, FullScreen: true}

	gotNav := in.FocusNav()
	if gotNav.Focus != app.FocusNav || gotNav.Mode != in.Mode || gotNav.FullScreen != in.FullScreen {
		t.Errorf("FocusNav perturbed mode/fullScreen: %+v", gotNav)
	}

	gotContent := in.FocusNav().FocusContent()
	if gotContent.Focus != app.FocusContent {
		t.Errorf("FocusContent failed: %+v", gotContent)
	}
}

// TestIsTable convenience predicate.
func TestIsTable(t *testing.T) {
	if !(app.ViewState{Mode: app.ModeTable}).IsTable() {
		t.Error("IsTable returned false for ModeTable")
	}
	if (app.ViewState{Mode: app.ModeYAML}).IsTable() {
		t.Error("IsTable returned true for ModeYAML")
	}
}
