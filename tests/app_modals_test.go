package klenstests

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/app"
)

// fakeModalState is shared mutable state across the test modals so we can
// observe which one received a message.
type fakeModalState struct {
	visible      bool
	keyPressOnly bool
	receivedKind string // empty until Update runs
}

// makeFake returns a Modal whose closures read from / write to s.
func makeFake(name string, s *fakeModalState) app.Modal {
	mod := app.Modal{
		Name:      name,
		IsVisible: func() bool { return s.visible },
		Update: func(msg tea.Msg) tea.Cmd {
			switch msg.(type) {
			case tea.KeyPressMsg:
				s.receivedKind = "key"
			default:
				s.receivedKind = "other"
			}
			return nil
		},
	}
	if s.keyPressOnly {
		mod.Handles = func(msg tea.Msg) bool {
			_, ok := msg.(tea.KeyPressMsg)
			return ok
		}
	}
	return mod
}

// TestModalStackDispatchOrder — the first visible modal in the stack
// intercepts. Modals further down do not run, even when also visible.
func TestModalStackDispatchOrder(t *testing.T) {
	a := &fakeModalState{visible: true}
	b := &fakeModalState{visible: true}
	stack := app.ModalStack{makeFake("a", a), makeFake("b", b)}

	_, handled := stack.Dispatch(tea.KeyPressMsg{})
	if !handled {
		t.Fatal("expected dispatch to find a visible modal")
	}
	if a.receivedKind != "key" {
		t.Errorf("modal a: expected to receive key, got %q", a.receivedKind)
	}
	if b.receivedKind != "" {
		t.Errorf("modal b: should not receive when a is visible-and-first; got %q", b.receivedKind)
	}
}

// TestModalStackSkipsHiddenModals — a hidden modal at the head doesn't block
// dispatch to the next visible one.
func TestModalStackSkipsHiddenModals(t *testing.T) {
	hidden := &fakeModalState{visible: false}
	visible := &fakeModalState{visible: true}
	stack := app.ModalStack{makeFake("hidden", hidden), makeFake("visible", visible)}

	if _, handled := stack.Dispatch(tea.KeyPressMsg{}); !handled {
		t.Fatal("expected dispatch to fall through to the visible modal")
	}
	if hidden.receivedKind != "" {
		t.Errorf("hidden modal received a message")
	}
	if visible.receivedKind != "key" {
		t.Errorf("visible modal: expected key, got %q", visible.receivedKind)
	}
}

// TestModalStackHandlesPredicateGatesNonKeyMessages — pickers use
// keyPressOnly: when a non-key message arrives, they don't intercept and the
// dispatch falls through. This preserves the original namespace/cluster
// picker behaviour where informer updates flow under an open picker.
func TestModalStackHandlesPredicateGatesNonKeyMessages(t *testing.T) {
	picker := &fakeModalState{visible: true, keyPressOnly: true}
	stack := app.ModalStack{makeFake("picker", picker)}

	// Non-key message — picker should NOT intercept.
	type fakeMsg struct{}
	if _, handled := stack.Dispatch(fakeMsg{}); handled {
		t.Error("picker intercepted a non-key message; should have fallen through")
	}
	if picker.receivedKind != "" {
		t.Errorf("picker received non-key message: %q", picker.receivedKind)
	}

	// Key message — picker should intercept.
	if _, handled := stack.Dispatch(tea.KeyPressMsg{}); !handled {
		t.Error("picker failed to intercept key message")
	}
	if picker.receivedKind != "key" {
		t.Errorf("picker: expected key, got %q", picker.receivedKind)
	}
}

// TestModalStackEmpty — an empty stack always falls through.
func TestModalStackEmpty(t *testing.T) {
	stack := app.ModalStack{}
	if _, handled := stack.Dispatch(tea.KeyPressMsg{}); handled {
		t.Error("empty stack reported handled=true")
	}
}

// TestModalStackAnyVisible — true when any modal is visible.
func TestModalStackAnyVisible(t *testing.T) {
	a := &fakeModalState{visible: false}
	b := &fakeModalState{visible: true}
	stack := app.ModalStack{makeFake("a", a), makeFake("b", b)}

	if !stack.AnyVisible() {
		t.Error("AnyVisible returned false despite b being visible")
	}

	b.visible = false
	if stack.AnyVisible() {
		t.Error("AnyVisible returned true with all hidden")
	}
}
