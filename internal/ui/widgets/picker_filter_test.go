package widgets

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// printableASCII is every character a user can type into a picker filter.
// No letter may be reserved as a navigation key (regression: j/k were).
func printableASCII() string {
	var b []byte
	for c := byte(' '); c <= '~'; c++ {
		b = append(b, c)
	}
	return string(b)
}

func typeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestNamespacePickerFilterAcceptsAllPrintable(t *testing.T) {
	want := printableASCII()
	p := NewNamespacePicker().Show([]string{"default", "kube-system"}, nil)
	for _, r := range want {
		p, _ = p.Update(typeKey(r))
	}
	if p.filter != want {
		t.Errorf("filter = %q, want %q", p.filter, want)
	}
}

func TestClusterPickerFilterAcceptsAllPrintable(t *testing.T) {
	want := printableASCII()
	p := NewClusterPicker().Show([]string{"dev", "prod"}, "dev")
	for _, r := range want {
		p, _ = p.Update(typeKey(r))
	}
	if p.filter != want {
		t.Errorf("filter = %q, want %q", p.filter, want)
	}
}
