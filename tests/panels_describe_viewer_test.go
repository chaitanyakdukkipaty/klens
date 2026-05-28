package klenstests

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/ui/modes"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// TestDescribeViewerFiltersContent — opening the filter input, typing a
// regex, and pressing enter narrows the rendered content to matching lines
// only. recompute() applies the filter; the resulting state exposes the
// match count via the filter summary in the view.
func TestDescribeViewerFiltersContent(t *testing.T) {
	v := panels.NewDescribeViewer(80, 24)
	body := "Name:\tnginx\nNamespace:\tdefault\nLabels:\tapp=web\nEvents:\nWarning  BackOff  pod restart\n"
	v, _ = v.Update(panels.DescribeFetchedMsg{
		Kind:      "Pod",
		Name:      "nginx",
		Namespace: "default",
		Text:      body,
	})
	// `/` opens input, type "Events", enter applies filter.
	v, _ = v.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "Events" {
		v, _ = v.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	v, _ = v.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	out := v.View()
	if !strings.Contains(out, "Events:") {
		t.Fatalf("filtered view missing matching line:\n%s", out)
	}
	if strings.Contains(out, "Name:\tnginx") {
		t.Fatalf("filtered view leaked non-matching line:\n%s", out)
	}
	if !v.HasActiveState() {
		t.Fatalf("applied filter should keep HasActiveState true")
	}
}

// TestDescribeViewerEscPeelsFilterThenExits — first esc cancels an open
// input, second esc clears the applied filter, third esc returns false
// (caller exits the mode).
func TestDescribeViewerEscPeelsFilterThenExits(t *testing.T) {
	v := panels.NewDescribeViewer(80, 24)
	v, _ = v.Update(panels.DescribeFetchedMsg{Kind: "Pod", Name: "p", Text: "one\ntwo\nthree\n"})

	// Open input, then esc — filter never applied.
	v, _ = v.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !v.HasActiveState() {
		t.Fatalf("open input should mark HasActiveState=true")
	}
	v, consumed := v.HandleEsc()
	if !consumed {
		t.Fatalf("first esc should peel open input")
	}
	if v.HasActiveState() {
		t.Fatalf("after first esc, no state should remain")
	}

	// Apply a filter, then esc twice — second esc should signal caller to exit.
	v, _ = v.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "two" {
		v, _ = v.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	v, _ = v.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	v, consumed = v.HandleEsc()
	if !consumed {
		t.Fatalf("esc on applied filter should peel it")
	}
	if v.HasActiveState() {
		t.Fatalf("after peeling filter, HasActiveState should be false")
	}
	_, consumed = v.HandleEsc()
	if consumed {
		t.Fatalf("esc with no state should return false so caller exits mode")
	}
}

// TestDescribeViewControllerHandlesAllKeys — the controller's HandleKey
// always returns consumed=true. The describe viewer owns its own filter
// input state machine, so no key should escape back to the root.
func TestDescribeViewControllerHandlesAllKeys(t *testing.T) {
	c := modes.NewDescribeViewController(panels.NewDescribeViewer(80, 24))
	for _, key := range []string{"/", "n", "N", "g", "G", "c", "ctrl+s", "j", "k"} {
		var msg tea.KeyPressMsg
		if strings.HasPrefix(key, "ctrl+") {
			r := rune(key[len("ctrl+")])
			msg = tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
		} else {
			msg = tea.KeyPressMsg{Code: rune(key[0]), Text: key}
		}
		_, _, consumed := c.HandleKey(msg)
		if !consumed {
			t.Errorf("controller refused key %q; describe view should consume every key", key)
		}
	}
}
