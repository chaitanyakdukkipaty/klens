// Package modes hosts per-mode controllers — one per non-table content surface
// (yaml view, yaml edit, logs, topology, metrics). The root model holds the
// active controller and forwards messages to it; controllers consume keys
// themselves and emit explicit messages (see internal/app/messages.go) when
// they want the root to take action.
//
// This file declares the shared Controller contract. Concrete controllers
// live in this package one-per-file.
package modes

import (
	tea "charm.land/bubbletea/v2"
)

// Controller is the seam between the root model and a single content mode.
//
// Bubbletea value semantics are preserved: every method returns a fresh
// Controller. The root never introspects controller state — when a controller
// is finished it emits app.CloseModeMsg from Update.
type Controller interface {
	// Update receives any tea.Msg the root forwards (mouse, paste, async
	// data, ticks). Returns the updated controller and any cmd to run.
	Update(msg tea.Msg) (Controller, tea.Cmd)

	// View renders the controller's content surface.
	View() string

	// SetSize updates the content area dimensions. Mirrors the panel
	// SetSize convention used elsewhere in the codebase.
	SetSize(w, h int) Controller

	// HandleKey routes a keypress. When consumed=true the root stops
	// further routing of this key. When false, the root falls through
	// to its own global handler so cross-mode keybindings still work.
	HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool)
}
