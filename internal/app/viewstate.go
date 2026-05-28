package app

// ViewState bundles the three coupled UI flags — current content Mode, which
// panel has Focus, and whether the active panel is FullScreen.
//
// The pure-value transitions (ResetToTable, EnterContentMode, ToggleFullScreen,
// FocusNav, FocusContent) encode the invariants that used to be scattered as
// inline assignments across model.Update. They are methods on a value type
// so transition logic is testable without constructing a full Bubbletea
// model.
//
// Model holds Mode/Focus/FullScreen as individual fields for back-compat with
// the dozens of read sites; the m.resetToTable / m.enterContentMode methods
// in this file route writes through ViewState so the rules stay in one place.
type ViewState struct {
	Mode       ContentMode
	Focus      FocusTarget
	FullScreen bool
}

// ResetToTable returns the operator to the resource table:
// mode → Table, focus → Nav, fullScreen → off. This is the "escape home"
// invariant invoked after esc, namespace switch, and cluster switch.
func (v ViewState) ResetToTable() ViewState {
	return ViewState{Mode: ModeTable, Focus: FocusNav, FullScreen: false}
}

// EnterContentMode switches to a non-table mode (YAML/Editor/Logs/XRay/
// Metrics). Focus moves to the content panel; fullScreen is preserved so a
// user can transition between content modes without losing fullscreen.
func (v ViewState) EnterContentMode(m ContentMode) ViewState {
	v.Mode = m
	v.Focus = FocusContent
	return v
}

// ToggleFullScreen flips the fullscreen flag, but only for content modes.
// Fullscreen-on-table has no meaning (the table already is the main surface)
// so the transition is suppressed.
func (v ViewState) ToggleFullScreen() ViewState {
	if v.Mode == ModeTable {
		return v
	}
	v.FullScreen = !v.FullScreen
	return v
}

// ExitFullScreen forces fullScreen off, preserving mode and focus. Called
// when leaving a content mode.
func (v ViewState) ExitFullScreen() ViewState {
	v.FullScreen = false
	return v
}

// FocusNav and FocusContent move keyboard focus between the nav panel and
// the content panel. Mode and fullScreen are unchanged.
func (v ViewState) FocusNav() ViewState     { v.Focus = FocusNav; return v }
func (v ViewState) FocusContent() ViewState { v.Focus = FocusContent; return v }

// IsTable reports whether the current mode is the resource table.
// Convenience for the dozens of `m.view.Mode == ModeTable` reads.
func (v ViewState) IsTable() bool { return v.Mode == ModeTable }

// modelViewState returns a ViewState snapshot of m. Internal helper for the
// transition methods below.
func modelViewState(m Model) ViewState {
	return ViewState{Mode: m.mode, Focus: m.focus, FullScreen: m.fullScreen}
}

// applyViewState writes a ViewState back into m.
func applyViewState(m Model, v ViewState) Model {
	m.mode = v.Mode
	m.focus = v.Focus
	m.fullScreen = v.FullScreen
	return m
}

// resetToTable is the model-side wrapper around ViewState.ResetToTable —
// the "escape home" transition.
func (m Model) resetToTable() Model {
	return applyViewState(m, modelViewState(m).ResetToTable())
}

// enterContentMode switches the model to a non-table mode and moves focus
// to content. Used by the y/e/l/t/m action handlers.
func (m Model) enterContentMode(mode ContentMode) Model {
	return applyViewState(m, modelViewState(m).EnterContentMode(mode))
}

// toggleFullScreen flips fullscreen for content modes.
func (m Model) toggleFullScreen() Model {
	return applyViewState(m, modelViewState(m).ToggleFullScreen())
}
