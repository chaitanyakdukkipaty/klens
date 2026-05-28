package klenstests

import (
	"os"
	"path/filepath"
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

// feedLogLine drives one LogLineMsg through the panel via Update and returns
// the resulting viewer. Used by buffer/filter/wrap tests to seed content
// without going through the streamer.
func feedLogLine(t *testing.T, v panels.LogViewer, group, pod, text string) panels.LogViewer {
	t.Helper()
	msg := k8s.LogLineMsg{Lines: []k8s.LogLine{{Pod: pod, Group: group, Text: text}}}
	next, _ := v.Update(msg)
	return next
}

// TestLogViewerPauseBuffersIncomingLines — pressing `s` enters paused mode;
// subsequent LogLineMsg batches are buffered, not rendered. Resuming flushes
// them. The visible state changes (pause indicator + post-resume line count)
// are the load-bearing observable contract.
func TestLogViewerPauseBuffersIncomingLines(t *testing.T) {
	v := panels.NewLogViewer(120, 24).SetPods([]string{"api-0"})

	// One line before pause — should land in the live buffer.
	v = feedLogLine(t, v, "", "api-0", "line-before-pause")
	if !strings.Contains(v.View(), "line-before-pause") {
		t.Fatalf("pre-pause line missing from view")
	}

	// Toggle pause.
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 's', Text: "s"}})

	// Lines arriving while paused must not appear in the view.
	v = feedLogLine(t, v, "", "api-0", "line-during-pause")
	view := v.View()
	if strings.Contains(view, "line-during-pause") {
		t.Fatalf("paused viewer rendered buffered line; view contained 'line-during-pause'")
	}
	if !strings.Contains(view, "■ paused") {
		t.Fatalf("paused indicator missing from view; got:\n%s", view)
	}
	if !strings.Contains(view, "+1") {
		t.Fatalf("pending-count indicator missing; expected '+1' in:\n%s", view)
	}

	// Resume — the buffered line must now be visible.
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 's', Text: "s"}})
	view = v.View()
	if !strings.Contains(view, "line-during-pause") {
		t.Fatalf("resumed viewer dropped buffered line; view:\n%s", view)
	}
	if strings.Contains(view, "■ paused") {
		t.Fatalf("resumed viewer still shows paused indicator; view:\n%s", view)
	}
}

// TestLogViewerPauseBufferCapped — pendingLines is capped at maxLogLines so
// a long pause against a chatty pod doesn't grow the buffer without bound.
// We can't easily observe the cap directly (the field is unexported and the
// constant is 10000), but the resumed line count must equal exactly
// maxLogLines even after we feed more than that while paused. We assert via
// the "+N" indicator on the paused header.
func TestLogViewerPauseBufferCapped(t *testing.T) {
	v := panels.NewLogViewer(120, 24).SetPods([]string{"api-0"})
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 's', Text: "s"}})

	const cap = 10000
	// Build one giant batch — exercising the drop-oldest path inside
	// LogLineMsg handling rather than feeding ~10k discrete messages.
	lines := make([]k8s.LogLine, cap+50)
	for i := range lines {
		lines[i] = k8s.LogLine{Pod: "api-0", Text: "pl"}
	}
	next, _ := v.Update(k8s.LogLineMsg{Lines: lines})
	v = next

	view := v.View()
	// The indicator must show the capped count, never higher.
	if !strings.Contains(view, "+10000") {
		t.Fatalf("expected '+10000' (cap) in paused indicator; view:\n%s", view)
	}
	if strings.Contains(view, "+10050") {
		t.Fatalf("paused buffer exceeded cap; view:\n%s", view)
	}
}

// TestLogViewerPauseDoesNotDisturbLastLineAt — `lastLineAt` tracks upstream
// activity, so the "quiet 30s" hint reflects stream silence and not display
// silence. Lines arriving while paused must still bump it. We assert the
// indicator does NOT show "quiet" after a brief paused-line feed.
func TestLogViewerPauseDoesNotShowQuietRightAfterFeed(t *testing.T) {
	v := panels.NewLogViewer(120, 24).SetPods([]string{"api-0"})
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 's', Text: "s"}})
	v = feedLogLine(t, v, "", "api-0", "x")
	// View should be the paused indicator only — no "quiet Ns" appended.
	if strings.Contains(v.View(), "quiet ") {
		t.Fatalf("unexpected 'quiet' marker right after a paused feed; view:\n%s", v.View())
	}
}

// TestLogViewerGlobalFilterAppliesAcrossGroups — viewer-level filter renders
// matching lines in every tab. Switching tabs preserves the filter.
func TestLogViewerGlobalFilterAppliesAcrossGroups(t *testing.T) {
	v := panels.NewLogViewer(120, 24).SetPodGroups([]k8s.LogGroup{
		{Name: "deploy/api", Pods: []string{"api-0"}},
		{Name: "deploy/worker", Pods: []string{"worker-0"}},
	})
	// Seed both groups: "ERROR foo" lines that should match, "INFO bar" should not.
	v = feedLogLine(t, v, "deploy/api", "api-0", "ERROR api-failure")
	v = feedLogLine(t, v, "deploy/api", "api-0", "INFO api-startup")
	v = feedLogLine(t, v, "deploy/worker", "worker-0", "ERROR worker-failure")
	v = feedLogLine(t, v, "deploy/worker", "worker-0", "INFO worker-startup")

	// Commit a viewer-wide filter for "ERROR".
	v = pressKeys(t, v, []tea.KeyPressMsg{
		{Code: '/', Text: "/"},
		{Code: 'E', Text: "E"},
		{Code: 'R', Text: "R"},
		{Code: 'R', Text: "R"},
		{Code: 'O', Text: "O"},
		{Code: 'R', Text: "R"},
		keyEnter,
	})

	// First group (deploy/api) is focused after SetPodGroups.
	view := v.View()
	if !strings.Contains(view, "ERROR api-failure") {
		t.Fatalf("focused group missing filtered line; view:\n%s", view)
	}
	if strings.Contains(view, "INFO api-startup") {
		t.Fatalf("filter leak: unmatched line rendered; view:\n%s", view)
	}

	// Switch to deploy/worker tab — filter must still be active there.
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: '2', Text: "2"}})
	view = v.View()
	if !strings.Contains(view, "ERROR worker-failure") {
		t.Fatalf("second group missing filtered line after tab switch; view:\n%s", view)
	}
	if strings.Contains(view, "INFO worker-startup") {
		t.Fatalf("filter not preserved across tab switch; view:\n%s", view)
	}
}

// TestLogViewerAutoscrollViewerWide — `a` toggles autoscroll; the indicator
// flips between "live" (when on) and "⏸ N%" (when off).
func TestLogViewerAutoscrollViewerWide(t *testing.T) {
	v := panels.NewLogViewer(120, 24).SetPods([]string{"api-0"})
	v = feedLogLine(t, v, "", "api-0", "tail-line")

	if !strings.Contains(v.View(), "● live") {
		t.Fatalf("expected 'live' indicator after fresh autoScroll feed; view:\n%s", v.View())
	}

	// Toggle off — expect the muted scroll indicator instead.
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 'a', Text: "a"}})
	if strings.Contains(v.View(), "● live") {
		t.Fatalf("'a' did not disable autoscroll; view still shows 'live':\n%s", v.View())
	}

	// Toggle back on — restore live indicator.
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 'a', Text: "a"}})
	if !strings.Contains(v.View(), "● live") {
		t.Fatalf("'a' did not re-enable autoscroll; view:\n%s", v.View())
	}
}

// TestLogViewerWrapTogglesIndicator — `w` flips the [wrap] hint in the header.
// We don't assert the precise wrap output (depends on terminal width math) —
// the toggle visibility is the public contract.
func TestLogViewerWrapTogglesIndicator(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPods([]string{"api-0"})
	if strings.Contains(v.View(), "[wrap]") {
		t.Fatalf("[wrap] indicator visible at start when wrap is off")
	}
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 'w', Text: "w"}})
	if !strings.Contains(v.View(), "[wrap]") {
		t.Fatalf("[wrap] indicator missing after 'w'; view:\n%s", v.View())
	}
}

// TestLogViewerWrapDoesNotBreakDragSelectIndexing — a wrapped log line spans
// multiple display rows; LineAtScreenY must still resolve every row to the
// same source-line index so drag-select across the wrapped rows collapses to
// one entry. We feed one long line, toggle wrap on, and verify a row near the
// top of the wrapped block and a row a few lines below both map back to
// source index 0.
func TestLogViewerWrapDoesNotBreakDragSelectIndexing(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPods([]string{"api-0"})
	// Build a line wide enough to wrap several times at 80 cols.
	long := strings.Repeat("abcdefghijklmnopqrst", 30) // 600 cols
	v = feedLogLine(t, v, "", "api-0", long)
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 'w', Text: "w"}})
	// LineAtScreenY uses the panel-local Y coordinate. The viewport content
	// starts a few rows in (header + help + blank); we sweep through the first
	// handful of content rows and assert each one resolves to source index 0.
	hits := 0
	for y := 0; y < 24; y++ {
		if idx, ok := v.LineAtScreenY(y); ok {
			if idx != 0 {
				t.Fatalf("wrapped row at y=%d resolved to source idx %d; expected 0", y, idx)
			}
			hits++
		}
	}
	if hits < 2 {
		t.Fatalf("expected at least 2 wrapped display rows for source idx 0, got %d hits", hits)
	}
}

// TestLogViewerCtrlSWritesFile — `ctrl+s` writes the focused group's visible
// lines to $KLENS_DUMP_DIR. The status message returned by ConsumeStatusMsg
// must include "saved to" plus the dump dir path.
func TestLogViewerCtrlSWritesFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("KLENS_DUMP_DIR", tmp)

	v := panels.NewLogViewer(120, 24).SetPods([]string{"api-0"})
	v = feedLogLine(t, v, "", "api-0", "savable-line")
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 's', Mod: tea.ModCtrl}})

	status := v.ConsumeStatusMsg()
	if !strings.HasPrefix(status, "saved to ") {
		t.Fatalf("expected 'saved to ' status, got %q", status)
	}
	files, err := filepath.Glob(filepath.Join(tmp, "logs-*.log"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one dump file in %s, found %d", tmp, len(files))
	}
	body, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read dump: %v", err)
	}
	if !strings.Contains(string(body), "savable-line") {
		t.Fatalf("dump file missing seeded line; got %q", string(body))
	}
}

// TestLogViewerCopyEmitsStatus — `c` (no drag) writes to clipboard and emits
// a "copied N lines" status. Clipboard write may fail on headless CI; in that
// case the status reflects "copy failed:" — we accept either prefix.
func TestLogViewerCopyEmitsStatus(t *testing.T) {
	v := panels.NewLogViewer(120, 24).SetPods([]string{"api-0"})
	v = feedLogLine(t, v, "", "api-0", "copyable")
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 'c', Text: "c"}})

	status := v.ConsumeStatusMsg()
	if !strings.HasPrefix(status, "copied ") && !strings.HasPrefix(status, "copy failed:") {
		t.Fatalf("expected 'copied ' or 'copy failed:' status, got %q", status)
	}
}

// TestLogViewerPKeyEmitsToggleMsg — `p` returns a LogPreviousToggleMsg as a
// tea.Cmd so the root model can re-create the streamer with previous=true.
func TestLogViewerPKeyEmitsToggleMsg(t *testing.T) {
	v := panels.NewLogViewer(80, 24).SetPods([]string{"api-0"})
	next, cmd, consumed := v.HandleKey(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if !consumed {
		t.Fatal("'p' not consumed by log viewer")
	}
	_ = next
	if cmd == nil {
		t.Fatal("'p' did not emit a tea.Cmd; root cannot toggle previous-container source")
	}
	msg := cmd()
	if _, ok := msg.(panels.LogPreviousToggleMsg); !ok {
		t.Fatalf("expected LogPreviousToggleMsg, got %T", msg)
	}
}

// TestLogViewerHelpHasNoGGEntry — the legacy "g/G top/bottom" entry must be
// gone from the help line. RenderHelpInline trims the leading letter when it
// matches the key (e.g. "[a]utoscroll"), so we test for the trimmed forms.
func TestLogViewerHelpHasNoGGEntry(t *testing.T) {
	v := panels.NewLogViewer(120, 24).SetPods([]string{"api-0"})
	view := v.View()
	if strings.Contains(view, "g/G") || strings.Contains(view, "top/bottom") {
		t.Fatalf("help line still advertises g/G; expected removal. view:\n%s", view)
	}
	// "[s]pause" — 's' doesn't share a leading letter with "pause" so the
	// word is preserved intact.
	if !strings.Contains(view, "pause") {
		t.Fatalf("help line missing new 'pause' (s) entry; view:\n%s", view)
	}
	// "[a]utoscroll" — leading 'a' is trimmed because key 'a' matches.
	if !strings.Contains(view, "utoscroll") {
		t.Fatalf("help line missing new 'autoscroll' (a) entry; view:\n%s", view)
	}
}

// TestLogViewerGAndGAreInert — pressing G must not re-enable autoscroll, and
// pressing g must not scroll-to-top. Both are removed from log mode in spec
// 0005. The viewport's default keymap would otherwise consume them and the
// AtBottom recheck would re-enable autoscroll as a side-effect, so the panel
// explicitly swallows them.
func TestLogViewerGAndGAreInert(t *testing.T) {
	v := panels.NewLogViewer(120, 24).SetPods([]string{"api-0"})
	v = feedLogLine(t, v, "", "api-0", "tail-line")
	// Disable autoscroll explicitly.
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 'a', Text: "a"}})
	if strings.Contains(v.View(), "● live") {
		t.Fatalf("autoscroll did not turn off after 'a'; view:\n%s", v.View())
	}
	// Press G — must NOT flip autoscroll back on.
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 'G', Text: "G"}})
	if strings.Contains(v.View(), "● live") {
		t.Fatalf("'G' re-enabled autoscroll; should be inert in log mode. view:\n%s", v.View())
	}
	// Press g — same expectation (the indicator stays in scroll-percent mode).
	v = pressKeys(t, v, []tea.KeyPressMsg{{Code: 'g', Text: "g"}})
	if strings.Contains(v.View(), "● live") {
		t.Fatalf("'g' flipped state; should be inert in log mode. view:\n%s", v.View())
	}
}
