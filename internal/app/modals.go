package app

import (
	tea "charm.land/bubbletea/v2"
)

// Modal is a uniform handle on one entry in the modal stack.
//
// The widgets themselves (ConfirmDialog, NamespacePicker, ContextMenu, etc.)
// keep their value-style Update methods. Each Modal carries closures that
// observe and dispatch to one widget — concrete types stay where they are,
// no cross-widget interface needed.
type Modal struct {
	// Name identifies the modal in tests and debug output.
	Name string

	// IsVisible reports whether this modal is currently shown.
	IsVisible func() bool

	// Handles is an optional predicate: when set, only messages it accepts
	// are forwarded to Update. Used by the namespace and cluster pickers,
	// which intercept key events only and let other msgs flow past so the
	// app keeps reconciling cluster state under an open picker.
	//
	// If nil, the modal handles every message while visible (the default
	// for confirm/scale/context-menu, which fully block input).
	Handles func(tea.Msg) bool

	// Update dispatches the msg to the underlying widget. The closure is
	// expected to mutate the captured widget state and return any command
	// the widget produced.
	Update func(tea.Msg) tea.Cmd
}

// ModalStack is the ordered list of modals checked top-to-bottom each Update.
// The first visible modal that accepts the message intercepts it; if none
// match, the message falls through to the model's normal dispatch.
//
// Order matters: the head of the stack wins. In practice that mirrors the
// historical priority of the if-block ladder it replaces — the most recently
// shown modal naturally sits highest because it's the only one visible.
type ModalStack []Modal

// Dispatch routes msg to the first visible modal that handles it. Returns
// the widget's cmd and true on a hit; nil/false when no modal intercepts.
func (s ModalStack) Dispatch(msg tea.Msg) (tea.Cmd, bool) {
	for _, m := range s {
		if !m.IsVisible() {
			continue
		}
		if m.Handles != nil && !m.Handles(msg) {
			continue
		}
		return m.Update(msg), true
	}
	return nil, false
}

// AnyVisible reports whether any modal in the stack is currently shown.
// Useful when the model wants to suppress global behavior under any modal.
func (s ModalStack) AnyVisible() bool {
	for _, m := range s {
		if m.IsVisible() {
			return true
		}
	}
	return false
}

// keyPressOnly is the Handles predicate for pickers that intercept keys but
// let other messages (informer updates, ticks, async results) pass through.
func keyPressOnly(msg tea.Msg) bool {
	_, ok := msg.(tea.KeyPressMsg)
	return ok
}

// keyOrMouse intercepts keyboard and mouse input but lets informer updates,
// ticks, and async results flow past — for overlays the user may leave open.
func keyOrMouse(msg tea.Msg) bool {
	switch msg.(type) {
	case tea.KeyPressMsg, tea.MouseClickMsg, tea.MouseMotionMsg, tea.MouseWheelMsg, tea.MouseReleaseMsg:
		return true
	}
	return false
}

// modals returns the ordered modal stack for this Model. Each Modal's
// closures capture &m so that widget mutations performed during Update flow
// through to the local Model the caller will return.
func (m *Model) modals() ModalStack {
	return ModalStack{
		{
			Name:      "namespacePicker",
			IsVisible: m.namespacePicker.IsVisible,
			Handles:   keyOrMouse,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.namespacePicker, cmd = m.namespacePicker.Update(msg)
				return cmd
			},
		},
		{
			Name:      "clusterPicker",
			IsVisible: m.clusterPicker.IsVisible,
			Handles:   keyOrMouse,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.clusterPicker, cmd = m.clusterPicker.Update(msg)
				return cmd
			},
		},
		{
			Name:      "containerPicker",
			IsVisible: m.containerPicker.IsVisible,
			Handles:   keyPressOnly,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.containerPicker, cmd = m.containerPicker.Update(msg)
				return cmd
			},
		},
		{
			Name:      "confirm",
			IsVisible: m.confirm.IsVisible,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.confirm, cmd = m.confirm.Update(msg)
				return cmd
			},
		},
		{
			Name:      "scaleDialog",
			IsVisible: m.scaleDialog.IsVisible,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.scaleDialog, cmd = m.scaleDialog.Update(msg)
				return cmd
			},
		},
		{
			Name:      "sanitizeDialog",
			IsVisible: m.sanitizeDialog.IsVisible,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.sanitizeDialog, cmd = m.sanitizeDialog.Update(msg)
				return cmd
			},
		},
		{
			Name:      "contextMenu",
			IsVisible: m.contextMenu.IsVisible,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.contextMenu, cmd = m.contextMenu.Update(msg)
				return cmd
			},
		},
		{
			Name:      "pfDialog",
			IsVisible: m.pfDialog.IsVisible,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.pfDialog, cmd = m.pfDialog.Update(msg)
				return cmd
			},
		},
		{
			Name:      "appMenu",
			IsVisible: m.appMenu.IsVisible,
			Handles:   keyOrMouse,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.appMenu, cmd = m.appMenu.Update(msg)
				return cmd
			},
		},
		{
			Name:      "keysOverlay",
			IsVisible: m.keysOverlay.IsVisible,
			Handles:   keyOrMouse,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.keysOverlay, cmd = m.keysOverlay.Update(msg)
				return cmd
			},
		},
		{
			Name:      "settings",
			IsVisible: m.settings.IsVisible,
			Handles:   keyOrMouse,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.settings, cmd = m.settings.Update(msg)
				return cmd
			},
		},
		{
			Name:      "pfList",
			IsVisible: m.pfList.IsVisible,
			// PF list is long-lived (user might leave it open while watching
			// forwards). Only intercept key events so PortForwardClosedMsg,
			// MetricsTick, and informer updates can flow through to the model.
			Handles: keyPressOnly,
			Update: func(msg tea.Msg) tea.Cmd {
				var cmd tea.Cmd
				m.pfList, cmd = m.pfList.Update(msg)
				return cmd
			},
		},
	}
}
