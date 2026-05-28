package panels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/atotto/clipboard"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	k8slogs "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

// LogPreviousToggleMsg requests the root model to flip the streamer's
// "previous container" flag. The root tears down the current LogStreamer,
// builds a new one with Previous set to the inverse of its prior state, and
// re-seeds the viewer with empty lines. The viewer mirrors that flag in
// LogViewer.previous so the title can show "[previous]" while the stream is
// re-routed.
type LogPreviousToggleMsg struct{}

// klensChromaStyle is a minimal Chroma style aligned to the klens palette.
// Foreground-only — no background color so it doesn't fight the lipgloss panel backgrounds.
var klensChromaStyle = chroma.MustNewStyle("klens", chroma.StyleEntries{
	chroma.Comment:       "#888888", // Inactive Gray
	chroma.Keyword:       "#00ADD8", // Signal Blue — covers KeywordConstant by inheritance
	chroma.NameTag:       "#00ADD8", // JSON/YAML keys
	chroma.LiteralString: "#AAAAAA", // Body Text — covers all string subtypes
	chroma.LiteralNumber: "#AAAAAA", // Body Text — covers all number subtypes
	chroma.Punctuation:   "#888888", // Inactive Gray — structural chars recede
	chroma.Operator:      "#888888", // Inactive Gray — YAML colon/dash separators
})

const maxLogLines = 10000

// LogLayoutMode determines how the log viewer arranges its log groups.
type LogLayoutMode int

const (
	LayoutTabs LogLayoutMode = iota
	LayoutHorizontal
	LayoutVertical
)

// hitZone records a clickable region in panel-local coordinates.
type hitZone struct {
	x1, y1, x2, y2 int // inclusive
	idx            int // index into LogViewer.groups
}

// LogAutoScrollTickMsg drives drag-to-copy auto-scroll. The root model
// schedules the first tick on mouse-down and reschedules from its handler
// while a drag is in progress; mouse-up flips drag.Active=false, so the
// next tick sees no drag and the loop dies on its own.
type LogAutoScrollTickMsg struct{}

// LogAutoScrollTickCmd schedules one auto-scroll tick. 50 ms ≈ 20 lines/sec
// when the cursor sits outside the viewport — comparable to GUI drag-scroll
// feel, and a no-op when the cursor is back inside the viewport.
func LogAutoScrollTickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return LogAutoScrollTickMsg{}
	})
}

// logGroupState owns the per-group viewport, drag-select, match index, and
// pod-solo state. There is always at least one group — even when SetPods is
// used (no tabs), a synthetic group with empty name represents "all pods".
// Filter / search query state and the autoScroll flag have been promoted to
// LogViewer so a single query/toggle drives every group.
type logGroupState struct {
	group    string // "" when there are no tab groups
	viewport viewport.Model

	// searchMatches and searchCurrent are positions inside *this* group's
	// viewport — viewLine indices that depend on which lines belong to the
	// group. They stay per-group because n/N navigation needs to walk the
	// matches in this stripe's coordinate space.
	searchMatches []int
	searchCurrent int

	// podFilter solos a single pod within this group's pod set.
	// -1 = all pods. Only meaningful in single-group mode where
	// "this group's pod set" == LogViewer.pods.
	podFilter int

	lineCountStr string

	// drag holds drag-to-copy lifecycle state (indices into LogViewer.lines).
	// See DragSelection in drag.go.
	drag DragSelection

	// displayRows is built parallel to viewport content lines to translate
	// click coords back to LogViewer.lines indices. Panel-specific.
	displayRows []int
}

func newGroupState(name string) logGroupState {
	vp := viewport.New(viewport.WithWidth(1), viewport.WithHeight(1))
	vp.SetHorizontalStep(8)
	return logGroupState{
		group:         name,
		viewport:      vp,
		podFilter:     -1,
		searchCurrent: -1,
		lineCountStr:  "  0 lines",
		drag:          NewDragSelection(),
	}
}

// clearSelection resets in-app drag-select state. Call on actions that
// invalidate the previous selection (esc, layout cycle, group switch, filter
// change). Does not rebuild — callers do that anyway.
func (g *logGroupState) clearSelection() { g.drag.Reset() }

// LogViewer displays merged streaming logs from multiple pods, organized into
// one or more groups (tabs or split stripes). Filter, search query, autoscroll,
// pause, wrap, and the "previous container" flag live at the viewer level so a
// single query/toggle drives every group. Per-group state is limited to
// viewport position, drag selection, search-match positions inside that
// viewport, and pod-solo.
type LogViewer struct {
	width, height int
	focused       bool
	pods          []string

	// Master log buffer — shared across all groups, filtered per-group at render time.
	lines       []k8slogs.LogLine
	colorCache  []string // parallel to lines; Chroma-colorized JSON or "" for plain text
	indentCache []string // parallel to lines; json.MarshalIndent of valid JSON, "" otherwise
	lowerCache  []string // parallel to lines; pre-cached strings.ToLower(line.Text)
	jsonIndent  bool     // pretty-print JSON with indentation; toggle with J
	lastLineAt  time.Time

	tabGroups []string // empty when in single-group (no tabs) mode

	groups       []logGroupState // always len >= 1
	focusedGroup int             // index into groups
	layout       LogLayoutMode

	// Viewer-wide filter / search query state. Applied at render time to every
	// group. The match *positions* (searchMatches, searchCurrent) stay
	// per-group because each viewport has its own viewLine numbering.
	autoScroll  bool
	filter      string
	filterInput string
	filterOn    bool
	searchQuery string
	searchInput string
	searchOn    bool

	// paused freezes ingestion. While true, incoming LogLineMsg batches are
	// appended to pendingLines (capped at maxLogLines, drop-oldest) instead of
	// to lines. On resume the buffer is flushed into lines + rebuilt.
	paused       bool
	pendingLines []k8slogs.LogLine

	// wrap toggles ANSI-aware line wrapping at viewport width.
	wrap bool

	// previous mirrors the streamer's Previous flag so the title can show
	// "[previous]" while the stream is sourced from the previous container
	// instance. Toggled by the root when it handles LogPreviousToggleMsg.
	previous bool

	// Status messages surface transient one-shot feedback
	// (e.g. "split view requires 2-5 groups") via the parent status bar.
	// LogViewer does not own a status bar; we expose a getter and the model
	// pulls + clears as needed.
	statusMsg string
}

func NewLogViewer(w, h int) LogViewer {
	v := LogViewer{
		width:      w,
		height:     h,
		jsonIndent: false,
		autoScroll: true,
		groups:     []logGroupState{newGroupState("")},
		layout:     LayoutTabs,
	}
	v = v.SetSize(w, h)
	return v
}

// SetSize resizes all per-group viewports according to the current layout.
func (v LogViewer) SetSize(w, h int) LogViewer {
	v.width = w
	v.height = h
	v.applySizes()
	return v
}

// applySizes recalculates viewport sizes for every group based on layout.
// Called from SetSize and whenever layout / chrome visibility changes.
func (v *LogViewer) applySizes() {
	w, h := v.width, v.height
	switch v.layout {
	case LayoutTabs:
		// Single visible viewport. Same arithmetic as the pre-refactor code:
		// w-3 (border 2 + scrollbar 1), h-7 (border 2 + worst-case 5-line header).
		vw := max(1, w-3)
		vh := max(1, h-7)
		for i := range v.groups {
			v.groups[i].viewport.SetWidth(vw)
			v.groups[i].viewport.SetHeight(vh)
		}
	case LayoutHorizontal:
		n := len(v.groups)
		if n == 0 {
			return
		}
		// Outer border: 2 rows + 2 cols. Outer header: title (1) + blank (1) = 2 rows.
		usableH := max(1, h-2-2)
		stripeH := max(1, usableH/n)
		stripeW := max(1, w-2)
		// Each stripe has its own border (2 rows + 2 cols), title (1), help (1)
		// plus optional filter/search lines (added per-stripe at render time).
		// SetSize uses worst-case so the viewport doesn't overflow when bars appear.
		vw := max(1, stripeW-3)
		vh := max(1, stripeH-2-2)
		for i := range v.groups {
			v.groups[i].viewport.SetWidth(vw)
			v.groups[i].viewport.SetHeight(vh)
		}
	case LayoutVertical:
		n := len(v.groups)
		if n == 0 {
			return
		}
		usableW := max(1, w-2)
		stripeW := max(1, usableW/n)
		stripeH := max(1, h-2-2) // outer border + outer header
		vw := max(1, stripeW-3)
		vh := max(1, stripeH-2-2)
		for i := range v.groups {
			v.groups[i].viewport.SetWidth(vw)
			v.groups[i].viewport.SetHeight(vh)
		}
	}
}

func (v LogViewer) SetFocused(f bool) LogViewer { v.focused = f; return v }

// StatusMsg returns and clears any transient message (e.g. split-mode rejection).
func (v *LogViewer) ConsumeStatusMsg() string {
	msg := v.statusMsg
	v.statusMsg = ""
	return msg
}

// SetPods configures the viewer for a single (untabbed) group spanning all pods.
func (v LogViewer) SetPods(pods []string) LogViewer {
	v.pods = pods
	v.tabGroups = nil
	v.lines = nil
	v.colorCache = nil
	v.indentCache = nil
	v.lowerCache = nil
	v.jsonIndent = false
	v.lastLineAt = time.Time{}
	v.resetViewerState()
	v.groups = []logGroupState{newGroupState("")}
	v.focusedGroup = 0
	v.layout = LayoutTabs
	v.applySizes()
	return v
}

// SetPodGroups configures tab mode: one group per LogGroup, pods within each
// group merged. A single group falls back to SetPods (no tab bar).
func (v LogViewer) SetPodGroups(groups []k8slogs.LogGroup) LogViewer {
	if len(groups) <= 1 {
		if len(groups) == 1 {
			return v.SetPods(groups[0].Pods)
		}
		return v.SetPods(nil)
	}
	var allPods []string
	tabs := make([]string, len(groups))
	gs := make([]logGroupState, len(groups))
	for i, g := range groups {
		tabs[i] = g.Name
		allPods = append(allPods, g.Pods...)
		gs[i] = newGroupState(g.Name)
	}
	v.pods = allPods
	v.tabGroups = tabs
	v.lines = nil
	v.colorCache = nil
	v.indentCache = nil
	v.lowerCache = nil
	v.jsonIndent = false
	v.lastLineAt = time.Time{}
	v.resetViewerState()
	v.groups = gs
	v.focusedGroup = 0
	v.layout = LayoutTabs
	v.applySizes()
	return v
}

// resetViewerState wipes the viewer-level filter/search/pause/wrap/previous
// state when the pod set changes. Filter and search are deliberately not
// preserved across "open logs for a different selection" — there is no shared
// referent. autoScroll is reset to the default-on state (matches k9s "follow"
// on entering log mode).
func (v *LogViewer) resetViewerState() {
	v.autoScroll = true
	v.filter = ""
	v.filterInput = ""
	v.filterOn = false
	v.searchQuery = ""
	v.searchInput = ""
	v.searchOn = false
	v.paused = false
	v.pendingLines = nil
	v.wrap = false
	v.previous = false
}

// SetPrevious is called by the root after restarting the streamer with the
// inverted Previous flag, so the viewer's title can reflect the current
// source. The viewer mirrors the flag but does not own the streamer.
func (v LogViewer) SetPrevious(prev bool) LogViewer {
	v.previous = prev
	v.lines = nil
	v.colorCache = nil
	v.indentCache = nil
	v.lowerCache = nil
	v.pendingLines = nil
	v.lastLineAt = time.Time{}
	v.rebuildAll()
	return v
}

// Previous returns the current "previous container" state — used by the root
// when restarting the streamer in response to LogPreviousToggleMsg.
func (v LogViewer) Previous() bool { return v.previous }

// HandleKey routes a keypress through the viewer's layered state machine and
// reports whether the viewer consumed it. The peel order on ESC is: drag
// selection → search input → filter input → pod solo → search query →
// filter query → split layout. Each ESC peels exactly one layer; when nothing
// remains to peel, ESC returns consumed=false so the root's fullscreen-peel /
// mode-exit cascade can run. q / ctrl+c / F absorb as literal text only while
// a filter or search input is open — otherwise they fall through to the root
// so the global keybindings still work.
//
// This is the single seam through which the root reaches the viewer — no
// state-introspection accessors are exposed.
func (v LogViewer) HandleKey(k tea.KeyPressMsg) (LogViewer, tea.Cmd, bool) {
	switch k.String() {
	case "esc":
		if v.hasActiveState() {
			return v.handleEsc(), nil, true
		}
		return v, nil, false
	case "q", "ctrl+c", "F":
		if v.isCapturingInput() {
			next, cmd := v.Update(k)
			return next, cmd, true
		}
		return v, nil, false
	}
	next, cmd := v.Update(k)
	return next, cmd, true
}

// hasActiveState reports whether any peelable state is active. Used by
// HandleKey to decide whether to peel state vs. yield ESC to the root. Pause
// is deliberately not peelable — the user resumes with `s`, not ESC.
func (v LogViewer) hasActiveState() bool {
	if v.layout != LayoutTabs {
		return true
	}
	if v.filterOn || v.searchOn || v.filter != "" || v.searchQuery != "" {
		return true
	}
	if len(v.groups) == 0 {
		return false
	}
	g := v.groups[v.focusedGroup]
	return g.drag.Active || g.podFilter >= 0
}

// isCapturingInput reports whether a filter or search input is open. Used by
// HandleKey to absorb q / ctrl+c / F as literal text while typing into a
// filter or search field. Both inputs are viewer-level now.
func (v LogViewer) isCapturingInput() bool {
	return v.filterOn || v.searchOn
}

// handleEsc peels one layer of state, then returns to LayoutTabs from any
// split layout. Called by HandleKey on ESC. Order:
//
//	drag selection → search input → filter input → pod-solo
//	→ search query → filter query → split layout
//
// Pause is intentionally not in this list — see the type doc on paused.
func (v LogViewer) handleEsc() LogViewer {
	if len(v.groups) > 0 {
		g := &v.groups[v.focusedGroup]
		if g.drag.Active {
			g.clearSelection()
			v.rebuildGroup(v.focusedGroup)
			return v
		}
	}
	if v.searchOn {
		v.searchOn = false
		return v
	}
	if v.filterOn {
		v.filterOn = false
		return v
	}
	if len(v.groups) > 0 {
		g := &v.groups[v.focusedGroup]
		if g.podFilter >= 0 {
			g.podFilter = -1
			v.rebuildAll()
			return v
		}
	}
	if v.searchQuery != "" {
		v.searchQuery = ""
		v.searchInput = ""
		v.clearGroupMatches()
		v.rebuildAll()
		return v
	}
	if v.filter != "" {
		v.filter = ""
		v.filterInput = ""
		v.rebuildAll()
		return v
	}
	if v.layout != LayoutTabs {
		v.layout = LayoutTabs
		v.applySizes()
		v.rebuildAll()
		return v
	}
	return v
}

// clearGroupMatches wipes per-group search positions. Used when the viewer-
// level search query is cleared; the rebuild then re-populates positions
// for any remaining query, or leaves them empty.
func (v *LogViewer) clearGroupMatches() {
	for i := range v.groups {
		v.groups[i].searchMatches = nil
		v.groups[i].searchCurrent = -1
	}
}

func (v LogViewer) Update(msg tea.Msg) (LogViewer, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		// Bracketed paste — used by terminals that emit \e[200~ … \e[201~ when
		// the user hits Cmd+V / Ctrl+Shift+V. Route into whichever viewer-level
		// input is open.
		clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(msg.Content)
		if v.filterOn {
			v.filterInput += clean
			v.rebuildAll()
		} else if v.searchOn {
			v.searchInput += clean
			v.rebuildAll()
		}
		return v, nil

	case k8slogs.LogLineMsg:
		if len(msg.Lines) > 0 {
			// lastLineAt tracks *upstream* activity — bump it even while paused
			// so the "quiet 30s" hint reflects the stream, not the display.
			v.lastLineAt = time.Now()
		}
		if v.paused {
			// Buffer with drop-oldest cap. A 60s pause against a 1000-line/sec
			// pod would otherwise grow pendingLines without bound; capping at
			// maxLogLines matches the live-buffer policy.
			v.pendingLines = append(v.pendingLines, msg.Lines...)
			if over := len(v.pendingLines) - maxLogLines; over > 0 {
				v.pendingLines = v.pendingLines[over:]
			}
			return v, nil
		}
		v.appendLines(msg.Lines)
		v.rebuildAll()
		if v.autoScroll {
			for i := range v.groups {
				v.groups[i].viewport.GotoBottom()
			}
		}
		return v, nil

	case tea.KeyPressMsg:
		// Filter input mode (/) — viewer-level
		if v.filterOn {
			switch msg.String() {
			case "enter", "esc":
				v.filterOn = false
				v.filter = v.filterInput
				v.rebuildAll()
			case "backspace":
				if len(v.filterInput) > 0 {
					v.filterInput = v.filterInput[:len(v.filterInput)-1]
					v.rebuildAll()
				}
			case "ctrl+v":
				if text, err := clipboard.ReadAll(); err == nil && text != "" {
					clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
					v.filterInput += clean
					v.rebuildAll()
				}
			default:
				if len(msg.Text) > 0 {
					v.filterInput += msg.Text
					v.rebuildAll()
				}
			}
			return v, nil
		}

		// Search input mode (ctrl+f) — viewer-level
		if v.searchOn {
			switch msg.String() {
			case "enter":
				v.searchQuery = v.searchInput
				v.searchOn = false
				v.autoScroll = false
				v.rebuildAll()
				g := &v.groups[v.focusedGroup]
				if len(g.searchMatches) > 0 {
					g.searchCurrent = 0
					g.viewport.SetYOffset(g.searchMatches[0])
				}
			case "esc":
				v.searchOn = false
			case "backspace":
				if len(v.searchInput) > 0 {
					v.searchInput = v.searchInput[:len(v.searchInput)-1]
					v.rebuildAll()
				}
			case "ctrl+v":
				if text, err := clipboard.ReadAll(); err == nil && text != "" {
					clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
					v.searchInput += clean
					v.rebuildAll()
				}
			default:
				if len(msg.Text) > 0 {
					v.searchInput += msg.Text
					v.rebuildAll()
				}
			}
			return v, nil
		}

		g := &v.groups[v.focusedGroup]

		switch msg.String() {
		case "/":
			v.searchOn = false
			v.filterOn = true
			v.filterInput = v.filter
		case "ctrl+f":
			v.filterOn = false
			v.searchOn = true
			v.searchInput = v.searchQuery
			v.autoScroll = false
		case "n":
			if len(g.searchMatches) > 0 {
				g.searchCurrent = (g.searchCurrent + 1) % len(g.searchMatches)
				g.viewport.SetYOffset(g.searchMatches[g.searchCurrent])
				v.autoScroll = false
			}
		case "N":
			if len(g.searchMatches) > 0 {
				g.searchCurrent = (g.searchCurrent - 1 + len(g.searchMatches)) % len(g.searchMatches)
				g.viewport.SetYOffset(g.searchMatches[g.searchCurrent])
				v.autoScroll = false
			}
		case "s":
			return v.togglePause(), nil
		case "a":
			v.autoScroll = !v.autoScroll
			if v.autoScroll {
				for i := range v.groups {
					v.groups[i].viewport.GotoBottom()
				}
			}
		case "w":
			v.wrap = !v.wrap
			v.rebuildAll()
		case "c":
			if g.drag.Active {
				// Live drag selection — HandleMouseUp owns the copy path.
				return v, nil
			}
			lines := v.visibleLinesForFocusedGroup()
			if len(lines) == 0 {
				v.statusMsg = "logs: nothing to copy"
				return v, nil
			}
			if err := clipboard.WriteAll(strings.Join(lines, "\n")); err != nil {
				v.statusMsg = "copy failed: " + err.Error()
				return v, nil
			}
			v.statusMsg = fmt.Sprintf("copied %d line%s to clipboard", len(lines), plural(len(lines)))
		case "ctrl+s":
			v.statusMsg = v.saveVisibleToFile()
		case "p":
			return v, func() tea.Msg { return LogPreviousToggleMsg{} }
		case "tab":
			if len(v.tabGroups) > 1 {
				v.groups[v.focusedGroup].clearSelection()
				v.rebuildGroup(v.focusedGroup)
				v.focusedGroup = (v.focusedGroup + 1) % len(v.groups)
				if v.layout == LayoutTabs {
					ng := &v.groups[v.focusedGroup]
					ng.viewport.SetXOffset(0)
					if v.autoScroll {
						ng.viewport.GotoBottom()
					}
				}
			}
		case "v":
			v = v.cycleLayout()
		case "J":
			v.jsonIndent = !v.jsonIndent
			for i, l := range v.lines {
				v.colorCache[i] = tryColorizeJSON(l.Text, v.jsonIndent)
			}
			v.rebuildAll()
		case "g", "G":
			// g/G are intentionally inert in log mode (spec 0005). The new
			// `a` toggle subsumes "jump to bottom"; pgup/pgdn cover the rest.
			// We swallow them here so the viewport's default keymap doesn't
			// scroll on our behalf and the AtBottom recheck below doesn't
			// re-enable autoscroll as a side-effect.
			return v, nil
		case "0":
			g.viewport.SetXOffset(0)
			if len(v.tabGroups) > 1 {
				if v.focusedGroup != 0 {
					v.groups[v.focusedGroup].clearSelection()
					v.rebuildGroup(v.focusedGroup)
				}
				v.focusedGroup = 0
			} else {
				g.clearSelection()
				g.podFilter = -1
				v.rebuildGroup(v.focusedGroup)
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			n := int(msg.String()[0]-'0') - 1
			if len(v.tabGroups) > 1 {
				if n < len(v.groups) && n != v.focusedGroup {
					v.groups[v.focusedGroup].clearSelection()
					v.rebuildGroup(v.focusedGroup)
					v.focusedGroup = n
					if v.layout == LayoutTabs {
						ng := &v.groups[v.focusedGroup]
						if v.autoScroll {
							ng.viewport.GotoBottom()
						}
					}
				}
			} else if n < len(v.pods) {
				g.clearSelection()
				g.podFilter = n
				v.rebuildGroup(v.focusedGroup)
			}
		default:
			var cmd tea.Cmd
			g.viewport, cmd = g.viewport.Update(msg)
			switch msg.String() {
			case "up", "k", "pgup", "ctrl+u":
				v.autoScroll = false
			default:
				if g.viewport.AtBottom() {
					v.autoScroll = true
				}
			}
			return v, cmd
		}
		return v, nil

	case tea.MouseWheelMsg:
		// Wheel scrolls the focused group's viewport; autoscroll is viewer-wide.
		g := &v.groups[v.focusedGroup]
		var cmd tea.Cmd
		g.viewport, cmd = g.viewport.Update(msg)
		switch msg.Button {
		case tea.MouseWheelUp:
			v.autoScroll = false
		case tea.MouseWheelDown:
			if g.viewport.AtBottom() {
				v.autoScroll = true
			}
		}
		return v, cmd

	default:
		g := &v.groups[v.focusedGroup]
		var cmd tea.Cmd
		g.viewport, cmd = g.viewport.Update(msg)
		return v, cmd
	}
}

// appendLines pushes new lines onto the master buffer, growing the parallel
// caches and trimming to maxLogLines if needed. Trimming may invalidate
// drag-select indices, so any active selection is cleared.
func (v *LogViewer) appendLines(lines []k8slogs.LogLine) {
	for _, line := range lines {
		v.lines = append(v.lines, line)
		v.colorCache = append(v.colorCache, tryColorizeJSON(line.Text, v.jsonIndent))
		v.indentCache = append(v.indentCache, tryIndentJSON(line.Text))
		v.lowerCache = append(v.lowerCache, strings.ToLower(line.Text))
	}
	if len(v.lines) > maxLogLines {
		trim := len(v.lines) - maxLogLines
		v.lines = v.lines[trim:]
		v.colorCache = v.colorCache[trim:]
		v.indentCache = v.indentCache[trim:]
		v.lowerCache = v.lowerCache[trim:]
		for i := range v.groups {
			if v.groups[i].drag.Active {
				v.groups[i].clearSelection()
			}
		}
	}
}

// togglePause flips paused. On resume the pending buffer is flushed into the
// main lines buffer via the same trim/rebuild path the live append uses.
func (v LogViewer) togglePause() LogViewer {
	v.paused = !v.paused
	if !v.paused && len(v.pendingLines) > 0 {
		v.appendLines(v.pendingLines)
		v.pendingLines = nil
		v.rebuildAll()
		if v.autoScroll {
			for i := range v.groups {
				v.groups[i].viewport.GotoBottom()
			}
		}
	}
	return v
}

// visibleLinesForFocusedGroup returns the post-filter, group-belonging line
// text for the focused group. Shared by the copy ('c') and save ('ctrl+s')
// paths and by HandleMouseUp's drag-to-copy path so the three stay in sync.
func (v LogViewer) visibleLinesForFocusedGroup() []string {
	if len(v.groups) == 0 {
		return nil
	}
	g := v.groups[v.focusedGroup]
	activeFilter := v.filter
	if v.filterOn {
		activeFilter = v.filterInput
	}
	lowFilter := strings.ToLower(activeFilter)
	var out []string
	for i, l := range v.lines {
		if !v.lineBelongsToGroup(l, g) {
			continue
		}
		if lowFilter != "" {
			var lowText string
			if i < len(v.lowerCache) {
				lowText = v.lowerCache[i]
			} else {
				lowText = strings.ToLower(l.Text)
			}
			if !strings.Contains(lowText, lowFilter) {
				continue
			}
		}
		out = append(out, l.Text)
	}
	return out
}

// saveVisibleToFile writes the focused group's visible lines to
// $KLENS_DUMP_DIR (default ~/.klens/dumps/), using a timestamp-suffixed name
// so repeated dumps don't clobber each other. Returns a status string.
func (v LogViewer) saveVisibleToFile() string {
	lines := v.visibleLinesForFocusedGroup()
	if len(lines) == 0 {
		return "logs: nothing to save"
	}
	dir := os.Getenv("KLENS_DUMP_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "save failed: " + err.Error()
		}
		dir = filepath.Join(home, ".klens", "dumps")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "save failed: " + err.Error()
	}
	label := v.dumpLabel()
	ts := time.Now().Format("20060102-150405")
	name := fmt.Sprintf("logs-%s-%s.log", label, ts)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return "save failed: " + err.Error()
	}
	return "saved to " + path
}

// dumpLabel returns a filesystem-safe label identifying the focused group's
// source — group name in tab mode, pod name in single-group mode, or "all".
func (v LogViewer) dumpLabel() string {
	if len(v.groups) == 0 {
		return "all"
	}
	g := v.groups[v.focusedGroup]
	var raw string
	switch {
	case len(v.tabGroups) > 1:
		raw = v.tabGroups[v.focusedGroup]
	case g.podFilter >= 0 && g.podFilter < len(v.pods):
		raw = v.pods[g.podFilter]
	case len(v.pods) > 0:
		raw = v.pods[0]
	default:
		raw = "all"
	}
	// Replace path separators and whitespace so the result is safe to slot
	// into a filename. Keep colons and slashes out (deploy/api → deploy-api).
	r := strings.NewReplacer("/", "-", "\\", "-", " ", "_", ":", "-")
	return r.Replace(raw)
}

// splitViewCap is the maximum number of groups that can be shown side-by-side
// in split view. Beyond this, stripes get too cramped to be useful.
const splitViewCap = 4

// canSplit reports whether the current group count permits split view.
func (v LogViewer) canSplit() bool {
	return len(v.tabGroups) >= 2 && len(v.tabGroups) <= splitViewCap
}

// cycleLayout advances the layout mode if the group count permits; otherwise
// records a transient status message and stays in LayoutTabs.
func (v LogViewer) cycleLayout() LogViewer {
	for i := range v.groups {
		v.groups[i].clearSelection()
	}
	if !v.canSplit() {
		v.statusMsg = fmt.Sprintf("split view requires 2-%d groups", splitViewCap)
		v.layout = LayoutTabs
		v.applySizes()
		return v
	}
	switch v.layout {
	case LayoutTabs:
		v.layout = LayoutHorizontal
	case LayoutHorizontal:
		v.layout = LayoutVertical
	case LayoutVertical:
		v.layout = LayoutTabs
	}
	v.applySizes()
	if !v.stripeDimsViable() {
		v.statusMsg = "terminal too small for split view"
		v.layout = LayoutTabs
		v.applySizes()
		return v
	}
	v.rebuildAll()
	return v
}

// stripeDimsViable returns true if every per-group viewport has at least 2
// rows and 20 columns under the current layout.
func (v LogViewer) stripeDimsViable() bool {
	for i := range v.groups {
		if v.groups[i].viewport.Height() < 2 || v.groups[i].viewport.Width() < 20 {
			return false
		}
	}
	return true
}

// lineBelongsToGroup reports whether line l should appear in group g, based on
// group name and pod-solo filter.
func (v LogViewer) lineBelongsToGroup(l k8slogs.LogLine, g logGroupState) bool {
	if len(v.tabGroups) > 1 {
		if l.Group != g.group {
			return false
		}
	} else {
		// Single-group mode: pod-solo filter optionally subsets v.pods.
		if g.podFilter >= 0 && g.podFilter < len(v.pods) && l.Pod != v.pods[g.podFilter] {
			return false
		}
	}
	return true
}

// rebuildAll rebuilds every group's viewport content from v.lines.
func (v *LogViewer) rebuildAll() {
	for i := range v.groups {
		v.rebuildGroup(i)
	}
}

// rebuildGroup rebuilds a single group's viewport content from v.lines,
// applying the viewer-level filter/search and updating per-group
// searchMatches & lineCountStr.
func (v *LogViewer) rebuildGroup(idx int) {
	if idx < 0 || idx >= len(v.groups) {
		return
	}
	g := &v.groups[idx]
	g.searchMatches = nil
	g.displayRows = g.displayRows[:0]

	activeFilter := v.filter
	if v.filterOn {
		activeFilter = v.filterInput
	}
	lowFilter := strings.ToLower(activeFilter)

	activeSearch := v.searchQuery
	if v.searchOn {
		activeSearch = v.searchInput
	}
	lowSearch := strings.ToLower(activeSearch)

	// Selection range — only the focused group renders highlighting.
	selLo, selHi := -1, -1
	if idx == v.focusedGroup && g.drag.Active && g.drag.Start >= 0 && g.drag.End >= 0 {
		selLo, selHi = g.drag.Start, g.drag.End
		if selLo > selHi {
			selLo, selHi = selHi, selLo
		}
	}

	wrapWidth := 0
	if v.wrap {
		wrapWidth = g.viewport.Width()
	}

	var sb strings.Builder
	viewLine := 0
	shown := 0
	groupTotal := 0
	for i, l := range v.lines {
		if !v.lineBelongsToGroup(l, *g) {
			continue
		}
		groupTotal++

		var lowText string
		if i < len(v.lowerCache) {
			lowText = v.lowerCache[i]
		} else {
			lowText = strings.ToLower(l.Text)
		}

		if lowFilter != "" && !strings.Contains(lowText, lowFilter) {
			continue
		}
		shown++

		selected := selLo >= 0 && i >= selLo && i <= selHi

		// Search highlight takes priority over JSON colorization (both emit ANSI
		// codes). For selected lines we skip Chroma — composing reverse-video
		// over nested SGR codes is unreliable across terminals, and a clean
		// reverse is what users expect from a selection highlight. But in
		// indented JSON mode we use the plain indented form so the line keeps
		// its row count (otherwise it'd collapse from N rows to 1 on selection).
		text := l.Text
		if !selected {
			if lowSearch != "" && strings.Contains(lowText, lowSearch) {
				g.searchMatches = append(g.searchMatches, viewLine)
				text = highlightMatches(l.Text, lowSearch)
			} else if !l.IsSystem && i < len(v.colorCache) && v.colorCache[i] != "" {
				text = v.colorCache[i]
			}
		} else {
			if lowSearch != "" && strings.Contains(lowText, lowSearch) {
				// Still record the match index so n/N navigation works after
				// selection clears.
				g.searchMatches = append(g.searchMatches, viewLine)
			}
			if v.jsonIndent && !l.IsSystem && i < len(v.indentCache) && v.indentCache[i] != "" {
				text = v.indentCache[i]
			}
		}

		rendered := renderLogLineText(l, text)
		if selected {
			rendered = lipgloss.NewStyle().Reverse(true).Render(rendered)
		}
		if wrapWidth > 0 {
			// ANSI-aware wrap preserves SGR resumption across wrap points so
			// Chroma colorization and search-highlight reverse stay intact.
			rendered = xansi.Wrap(rendered, wrapWidth, "")
		}
		sb.WriteString(rendered)
		sb.WriteByte('\n')
		rows := strings.Count(rendered, "\n") + 1
		for r := 0; r < rows; r++ {
			g.displayRows = append(g.displayRows, i)
		}
		viewLine += rows
	}

	if len(g.searchMatches) == 0 {
		g.searchCurrent = -1
	} else if g.searchCurrent >= len(g.searchMatches) {
		g.searchCurrent = len(g.searchMatches) - 1
	}

	g.viewport.SetContent(sb.String())

	if len(v.tabGroups) > 1 || (g.podFilter >= 0 && g.podFilter < len(v.pods)) {
		g.lineCountStr = fmt.Sprintf("  %d/%d lines", shown, groupTotal)
	} else {
		g.lineCountStr = fmt.Sprintf("  %d lines", groupTotal)
	}
}

// highlightMatches wraps all case-insensitive occurrences of query in text with SearchHighlight.
func highlightMatches(text, query string) string {
	if query == "" {
		return text
	}
	low := strings.ToLower(text)
	var result strings.Builder
	pos := 0
	for {
		idx := strings.Index(low[pos:], query)
		if idx < 0 {
			result.WriteString(text[pos:])
			break
		}
		idx += pos
		result.WriteString(text[pos:idx])
		result.WriteString(styles.SearchHighlight.Render(text[idx : idx+len(query)]))
		pos = idx + len(query)
	}
	return result.String()
}

func renderLogLineText(l k8slogs.LogLine, text string) string {
	colorIdx := l.ColorIdx
	if colorIdx >= len(styles.LogPrefixColors) {
		colorIdx = colorIdx % len(styles.LogPrefixColors)
	}
	label := l.Pod
	if l.Container != "" {
		label = l.Pod + "/" + l.Container
	}
	prefix := styles.LogPrefixStyles[colorIdx].Render(fmt.Sprintf("[%s] ", label))
	if l.IsSystem {
		return styles.Muted.Italic(true).Render(text)
	}
	return prefix + text
}

// tryIndentJSON returns json.MarshalIndent of text if text is valid JSON.
// Returns "" for non-JSON. Used to render selected lines without Chroma SGR
// codes while preserving the row count of the indented view (so a line's
// height doesn't change when it's included in a drag-select).
func tryIndentJSON(text string) string {
	if len(text) == 0 || (text[0] != '{' && text[0] != '[') {
		return ""
	}
	var obj interface{}
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return ""
	}
	pretty, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return ""
	}
	return string(pretty)
}

// tryColorizeJSON pretty-prints (when indent=true) and Chroma-colorizes text if it is valid JSON.
// Returns empty string for non-JSON or on any error (caller falls back to plain text).
func tryColorizeJSON(text string, indent bool) string {
	if len(text) == 0 || (text[0] != '{' && text[0] != '[') {
		return ""
	}
	var obj interface{}
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return ""
	}
	var src string
	if indent {
		pretty, err := json.MarshalIndent(obj, "", "  ")
		if err != nil {
			return ""
		}
		src = string(pretty)
	} else {
		src = text
	}
	lexer := chroma.Coalesce(lexers.Get("json"))
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	iterator, err := lexer.Tokenise(nil, src)
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, klensChromaStyle, iterator); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}

// View renders the log viewer per the current layout.
func (v LogViewer) View() string {
	switch v.layout {
	case LayoutHorizontal, LayoutVertical:
		return v.renderSplit()
	default:
		return v.renderTabs()
	}
}

// renderTabs renders a single bordered panel: shared title + (optional) tab
// bar + viewer-level filter/search bars + the focused group's help/viewport.
func (v LogViewer) renderTabs() string {
	border := styles.NormalBorder
	if v.focused {
		border = styles.FocusedBorder
	}
	g := v.groups[v.focusedGroup]

	var title string
	if len(v.tabGroups) > 1 {
		activeName := v.tabGroups[v.focusedGroup]
		maxW := v.width - 30
		if len(activeName) > maxW && maxW > 3 {
			activeName = activeName[:maxW-1] + "…"
		}
		title = styles.Title.Render("Logs: ") + styles.Primary.Render(activeName)
	} else if g.podFilter >= 0 && g.podFilter < len(v.pods) {
		titlePods := v.pods[g.podFilter]
		maxW := v.width - 30
		if len(titlePods) > maxW && maxW > 3 {
			titlePods = titlePods[:maxW-1] + "…"
		}
		title = styles.Title.Render("Logs: ") + styles.Primary.Render(titlePods) +
			styles.Muted.Render(fmt.Sprintf("  [%d/%d · 0=all · esc]", g.podFilter+1, len(v.pods)))
	} else {
		titlePods := strings.Join(v.pods, ", ")
		if len(titlePods) > v.width-20 {
			titlePods = titlePods[:v.width-23] + "…"
		}
		title = styles.Title.Render("Logs: ") + styles.Primary.Render(titlePods)
	}
	if v.previous {
		title += "  " + styles.Warning.Render("[previous]")
	}

	scrollStatus := v.liveStateLabel(g)
	indentHint := ""
	if v.jsonIndent {
		indentHint = "  " + styles.Muted.Render("[json indent]")
	}
	wrapHint := ""
	if v.wrap {
		wrapHint = "  " + styles.Muted.Render("[wrap]")
	}

	tabBar := ""
	if len(v.tabGroups) > 1 {
		tabBar = "\n" + v.renderTabBar()
	}

	filterBar := v.renderFilterBar(g)
	searchBar := v.renderSearchBar(g)

	help := "  " + RenderHelpInline(v.helpItems())
	header := title + scrollStatus + indentHint + wrapHint + "  " + styles.Muted.Render(g.lineCountStr) + tabBar + filterBar + searchBar + "\n" + help

	sbStr := renderScrollbar(
		g.viewport.Height(),
		g.viewport.VisibleLineCount(),
		g.viewport.TotalLineCount(),
		g.viewport.YOffset(),
		v.focused,
	)
	return border.Width(max(1, v.width)).Height(max(1, v.height)).Render(
		header + "\n\n" + joinScrollbar(g.viewport.View(), sbStr),
	)
}

// helpItems builds the inline help list. The full key surface is too wide for
// narrow terminals on a single line; RenderHelpInline takes care of wrapping
// based on the visible width.
func (v LogViewer) helpItems() []HelpItem {
	items := []HelpItem{{Key: "↑↓/jk", Desc: "scroll"}}
	if len(v.tabGroups) > 1 {
		items = append(items, HelpItem{Key: "tab/1-9", Desc: "switch"})
		if v.canSplit() {
			items = append(items, HelpItem{Key: "v", Desc: "split"})
		}
	} else {
		items = append(items,
			HelpItem{Key: "1-9", Desc: "solo pod"},
			HelpItem{Key: "0", Desc: "all"},
		)
	}
	items = append(items,
		HelpItem{Key: "/", Desc: "filter"},
		HelpItem{Key: "ctrl+f", Desc: "search"},
		HelpItem{Key: "n/N", Desc: "next/prev"},
		HelpItem{Key: "s", Desc: "pause"},
		HelpItem{Key: "a", Desc: "autoscroll"},
		HelpItem{Key: "w", Desc: "wrap"},
		HelpItem{Key: "c", Desc: "copy"},
		HelpItem{Key: "ctrl+s", Desc: "save"},
		HelpItem{Key: "p", Desc: "prev"},
		HelpItem{Key: "J", Desc: "indent"},
		HelpItem{Key: "F", Desc: "fullscreen"},
		HelpItem{Key: "esc", Desc: "back"},
	)
	return items
}

// renderTabBar builds the "1:name │ 2:name │ ..." bar for tab mode.
func (v LogViewer) renderTabBar() string {
	if len(v.tabGroups) <= 1 {
		return ""
	}
	tabW := (v.width - 4) / len(v.tabGroups)
	var tabs []string
	for i, name := range v.tabGroups {
		label := fmt.Sprintf(" %d:%s ", i+1, name)
		if len(label) > tabW && tabW > 5 {
			label = fmt.Sprintf(" %d:%s ", i+1, name[:tabW-5]) + "… "
		}
		if i == v.focusedGroup {
			tabs = append(tabs, styles.Primary.Bold(true).Render(label))
		} else {
			tabs = append(tabs, styles.Muted.Render(label))
		}
	}
	return strings.Join(tabs, styles.Muted.Render("│"))
}

// renderSplit renders an outer panel containing N stripe boxes (one per group),
// laid out either as horizontal stripes or vertical columns. Viewer-level
// filter and search bars live on the outer header so a single query covers
// every stripe.
func (v LogViewer) renderSplit() string {
	border := styles.NormalBorder
	if v.focused {
		border = styles.FocusedBorder
	}

	layoutName := "horizontal split"
	if v.layout == LayoutVertical {
		layoutName = "vertical split"
	}
	title := styles.Title.Render("Logs ") +
		styles.Muted.Render("("+layoutName+"): ") +
		styles.Primary.Render(fmt.Sprintf("%d groups", len(v.tabGroups)))
	if v.previous {
		title += "  " + styles.Warning.Render("[previous]")
	}
	title += "   " + RenderHelpInline([]HelpItem{
		{Key: "v", Desc: "cycle layout"},
		{Key: "tab", Desc: "focus next"},
		{Key: "/", Desc: "filter"},
		{Key: "ctrl+f", Desc: "search"},
		{Key: "s", Desc: "pause"},
		{Key: "a", Desc: "autoscroll"},
		{Key: "w", Desc: "wrap"},
		{Key: "F", Desc: "fullscreen"},
		{Key: "esc", Desc: "exit split"},
	})

	g := v.groups[v.focusedGroup]
	filterBar := v.renderFilterBar(g)
	searchBar := v.renderSearchBar(g)
	outerHeader := title + filterBar + searchBar

	// Header line count: title (1) + filter (0 or 1) + search (0 or 1) + blank (1).
	headerLines := 1 + 1 // title + blank
	if v.filterOn || v.filter != "" {
		headerLines++
	}
	if v.searchOn || v.searchQuery != "" {
		headerLines++
	}

	// Compute stripe size & build stripe boxes.
	innerW := max(1, v.width-2)
	innerH := max(1, v.height-2-headerLines)

	var stripes []string
	switch v.layout {
	case LayoutHorizontal:
		n := len(v.groups)
		stripeH := innerH / n
		extra := innerH - stripeH*n // give remainder rows to the last stripe
		stripeW := innerW
		for i := range v.groups {
			h := stripeH
			if i == n-1 {
				h += extra
			}
			stripes = append(stripes, v.renderStripeBox(i, stripeW, h))
		}
		body := lipgloss.JoinVertical(lipgloss.Left, stripes...)
		return border.Width(max(1, v.width)).Height(max(1, v.height)).Render(
			outerHeader + "\n\n" + body,
		)
	case LayoutVertical:
		n := len(v.groups)
		stripeW := innerW / n
		extra := innerW - stripeW*n
		stripeH := innerH
		for i := range v.groups {
			w := stripeW
			if i == n-1 {
				w += extra
			}
			stripes = append(stripes, v.renderStripeBox(i, w, stripeH))
		}
		body := lipgloss.JoinHorizontal(lipgloss.Top, stripes...)
		return border.Width(max(1, v.width)).Height(max(1, v.height)).Render(
			outerHeader + "\n\n" + body,
		)
	}
	return ""
}

// renderStripeBox builds the bordered box for group i sized w x h. Viewer-
// level filter/search bars render on the outer panel header, so per-stripe
// chrome is title + counts + viewport only.
func (v LogViewer) renderStripeBox(i, w, h int) string {
	g := &v.groups[i] // safe: receiver is a value but groups slice array is shared

	stripeBorder := styles.NormalBorder
	if i == v.focusedGroup {
		stripeBorder = styles.FocusedBorder
	}

	// chromeLines: title (1) + help (1).
	chromeLines := 1 + 1
	vpW := max(1, w-3)
	vpH := max(1, h-2-chromeLines)
	g.viewport.SetWidth(vpW)
	g.viewport.SetHeight(vpH)

	// Rebuild this group's content for the new viewport size.
	v.rebuildGroup(i)

	name := v.tabGroups[i]
	maxN := w - 20
	if len(name) > maxN && maxN > 3 {
		name = name[:maxN-1] + "…"
	}
	focusMark := ""
	if i == v.focusedGroup {
		focusMark = styles.Primary.Bold(true).Render(" ▸ ")
	} else {
		focusMark = "   "
	}
	stripeTitle := focusMark + styles.Primary.Render(name) + v.liveStateLabel(*g) +
		"  " + styles.Muted.Render(g.lineCountStr)

	help := "  " + RenderHelpInline([]HelpItem{
		{Key: "↑↓/jk", Desc: "scroll"},
		{Key: "tab", Desc: "focus next"},
		{Key: "esc", Desc: "tabs"},
	})

	header := stripeTitle + "\n" + help

	sbStr := renderScrollbar(
		g.viewport.Height(),
		g.viewport.VisibleLineCount(),
		g.viewport.TotalLineCount(),
		g.viewport.YOffset(),
		i == v.focusedGroup,
	)
	return stripeBorder.Width(w).Height(h).Render(
		header + "\n" + joinScrollbar(g.viewport.View(), sbStr),
	)
}

// liveStateLabel returns the three-state indicator for the group. Order:
//
//	paused      → "■ paused +N"   (N = pending lines)
//	autoscroll+ → "● live"        (plus optional "· quiet Ks" after 30s)
//	otherwise   → "⏸ N%"          (N = scroll percent)
//
// The pause label takes priority over autoscroll because pause is the
// stronger UX commitment — once paused, the user wants to know the buffered
// count and isn't reading from the live tail.
func (v LogViewer) liveStateLabel(g logGroupState) string {
	if v.paused {
		return styles.Muted.Render(fmt.Sprintf("  ■ paused +%d", len(v.pendingLines)))
	}
	if v.autoScroll && g.viewport.AtBottom() {
		s := styles.Success.Render("  ● live")
		if !v.lastLineAt.IsZero() {
			if since := time.Since(v.lastLineAt); since >= 30*time.Second {
				s += styles.Muted.Render(fmt.Sprintf(" · quiet %ds", int(since.Seconds())))
			}
		}
		return s
	}
	pct := 100
	if g.viewport.TotalLineCount() > 0 {
		pct = int(g.viewport.ScrollPercent() * 100)
	}
	return styles.Muted.Render(fmt.Sprintf("  ⏸ %d%%", pct))
}

func (v LogViewer) renderFilterBar(g logGroupState) string {
	if v.filterOn {
		return "\n" + styles.Primary.Render("filter: ") + v.filterInput + styles.Muted.Render("█")
	}
	if v.filter != "" {
		return "\n" + styles.Primary.Render("filter: ") + styles.Warning.Render(v.filter) +
			styles.Muted.Render("  (/ change, esc clear)")
	}
	return ""
}

func (v LogViewer) renderSearchBar(g logGroupState) string {
	if v.searchOn {
		extra := ""
		if v.searchInput != "" && len(g.searchMatches) > 0 {
			extra = "  " + styles.Muted.Render(fmt.Sprintf("%d matches", len(g.searchMatches)))
		}
		return "\n" + styles.Primary.Render("search: ") + v.searchInput + styles.Muted.Render("█") + extra
	}
	if v.searchQuery != "" {
		matchInfo := "no matches"
		if len(g.searchMatches) > 0 {
			matchInfo = fmt.Sprintf("%d/%d", g.searchCurrent+1, len(g.searchMatches))
		}
		return "\n" + styles.Primary.Render("search: ") + styles.Warning.Render(v.searchQuery) +
			"  " + styles.Muted.Render(matchInfo+"  n↓ N↑  esc clear")
	}
	return ""
}

// HandleClickAt routes a left-click in panel-local coordinates to a tab or
// stripe and returns true on hit.
func (v LogViewer) HandleClickAt(x, y int) (LogViewer, bool) {
	for _, z := range v.tabHitZones() {
		if x >= z.x1 && x <= z.x2 && y >= z.y1 && y <= z.y2 {
			if v.focusedGroup != z.idx {
				v.groups[v.focusedGroup].clearSelection()
				v.rebuildGroup(v.focusedGroup)
			}
			v.focusedGroup = z.idx
			if v.layout == LayoutTabs && v.autoScroll {
				v.groups[z.idx].viewport.GotoBottom()
			}
			return v, true
		}
	}
	for _, z := range v.stripeHitZones() {
		if x >= z.x1 && x <= z.x2 && y >= z.y1 && y <= z.y2 {
			if v.focusedGroup != z.idx {
				v.groups[v.focusedGroup].clearSelection()
				v.rebuildGroup(v.focusedGroup)
			}
			v.focusedGroup = z.idx
			return v, true
		}
	}
	return v, false
}

// outerHeaderRows returns the number of rows the outer panel header occupies
// in split layouts: title (1) + optional filter (0/1) + optional search (0/1)
// + blank separator (1). The tabs path computes this inline; split-layout
// arithmetic in stripeBoundsFor / hit zones / size calcs all read from here
// so they stay consistent.
func (v LogViewer) outerHeaderRows() int {
	rows := 1 + 1 // title + blank
	if v.filterOn || v.filter != "" {
		rows++
	}
	if v.searchOn || v.searchQuery != "" {
		rows++
	}
	return rows
}

// stripeBoundsFor returns panel-local bounds (inclusive) of group i's outer
// stripe rectangle in split layouts. Returns ok=false in tabs mode or for
// out-of-range indices.
func (v LogViewer) stripeBoundsFor(i int) (x1, y1, x2, y2 int, ok bool) {
	if v.layout == LayoutTabs {
		return 0, 0, 0, 0, false
	}
	n := len(v.groups)
	if n == 0 || i < 0 || i >= n {
		return 0, 0, 0, 0, false
	}
	headerRows := v.outerHeaderRows()
	innerW := max(1, v.width-2)
	innerH := max(1, v.height-2-headerRows)
	switch v.layout {
	case LayoutHorizontal:
		stripeH := innerH / n
		extra := innerH - stripeH*n
		y := 1 + headerRows // outer border-top + outer header
		for k := 0; k < i; k++ {
			h := stripeH
			if k == n-1 {
				h += extra
			}
			y += h
		}
		h := stripeH
		if i == n-1 {
			h += extra
		}
		return 1, y, innerW, y + h - 1, true
	case LayoutVertical:
		stripeW := innerW / n
		extra := innerW - stripeW*n
		yTop := 1 + headerRows
		yBot := yTop + innerH - 1
		x := 1
		for k := 0; k < i; k++ {
			w := stripeW
			if k == n-1 {
				w += extra
			}
			x += w
		}
		w := stripeW
		if i == n-1 {
			w += extra
		}
		return x, yTop, x + w - 1, yBot, true
	}
	return 0, 0, 0, 0, false
}

// viewportBoundsFor returns the panel-local rectangle of group i's viewport
// content area (inclusive). In LayoutTabs only the focused group has a
// visible viewport; non-focused groups return ok=false.
func (v LogViewer) viewportBoundsFor(i int) (x1, y1, x2, y2 int, ok bool) {
	if i < 0 || i >= len(v.groups) {
		return 0, 0, 0, 0, false
	}
	g := v.groups[i]
	if v.layout == LayoutTabs {
		if i != v.focusedGroup {
			return 0, 0, 0, 0, false
		}
		row := 1 // outer panel border-top
		row++    // title row
		if len(v.tabGroups) > 1 {
			row++
		}
		if v.filterOn || v.filter != "" {
			row++
		}
		if v.searchOn || v.searchQuery != "" {
			row++
		}
		row++ // help line
		row++ // blank line ("\n\n" between header and viewport)
		return 1, row, max(1, v.width-2), row + g.viewport.Height() - 1, true
	}
	sx1, sy1, sx2, sy2, sok := v.stripeBoundsFor(i)
	if !sok {
		return 0, 0, 0, 0, false
	}
	chrome := 1 // stripe border-top
	chrome++    // stripe title
	chrome++    // help line (no blank row in split — only "\n" between header and viewport)
	vpY1 := sy1 + chrome
	vpY2 := vpY1 + g.viewport.Height() - 1
	if vpY2 > sy2-1 {
		vpY2 = sy2 - 1
	}
	return sx1 + 1, vpY1, sx2 - 1, vpY2, true
}

// lineAtScreenInGroup returns the v.lines index for the screen point inside
// group i's viewport. Returns ok=false if the point is outside that group's
// viewport content area.
func (v LogViewer) lineAtScreenInGroup(i, x, y int) (int, bool) {
	x1, y1, x2, y2, ok := v.viewportBoundsFor(i)
	if !ok {
		return 0, false
	}
	if x < x1 || x > x2 || y < y1 || y > y2 {
		return 0, false
	}
	g := v.groups[i]
	displayRow := g.viewport.YOffset() + (y - y1)
	if displayRow < 0 || displayRow >= len(g.displayRows) {
		return 0, false
	}
	return g.displayRows[displayRow], true
}

// LineAtScreenY translates a panel-local Y coordinate into the v.lines index
// in the focused group's viewport. Backward-compat wrapper around
// lineAtScreenInGroup that ignores X (sufficient for tabs mode where the
// viewport spans the full panel width).
func (v LogViewer) LineAtScreenY(localY int) (int, bool) {
	x1, _, _, _, ok := v.viewportBoundsFor(v.focusedGroup)
	if !ok {
		return 0, false
	}
	return v.lineAtScreenInGroup(v.focusedGroup, x1, localY)
}

// groupAtViewport finds which group's viewport content the point lies in.
// In tabs mode the focused group is the only candidate; in split mode every
// group's stripe is checked. Returns ok=false if the point is on chrome or
// outside any viewport.
func (v LogViewer) groupAtViewport(x, y int) (groupIdx, lineIdx int, ok bool) {
	if v.layout == LayoutTabs {
		idx, hit := v.lineAtScreenInGroup(v.focusedGroup, x, y)
		if !hit {
			return 0, 0, false
		}
		return v.focusedGroup, idx, true
	}
	for i := range v.groups {
		idx, hit := v.lineAtScreenInGroup(i, x, y)
		if hit {
			return i, idx, true
		}
	}
	return 0, 0, false
}

// HandleMouseDown begins a drag-select if the click lands on a log line in
// any visible viewport. In split layouts a click in a non-focused stripe
// switches focus to that stripe before starting selection. Returns
// started=true when a selection has begun, in which case the caller should
// suppress other click routing (tab/stripe/focus).
func (v LogViewer) HandleMouseDown(localX, localY int) (LogViewer, bool) {
	groupIdx, idx, ok := v.groupAtViewport(localX, localY)
	if !ok {
		return v, false
	}
	if groupIdx != v.focusedGroup {
		v.groups[v.focusedGroup].clearSelection()
		v.rebuildGroup(v.focusedGroup)
		v.focusedGroup = groupIdx
	}
	g := &v.groups[v.focusedGroup]
	g.drag.Begin(idx, localX, localY)
	v.rebuildGroup(v.focusedGroup)
	return v, true
}

// HandleMouseDrag extends the in-progress drag-select to localY. No-op if no
// drag is in progress. The cursor position is recorded so AutoScrollStep can
// drive viewport scrolling on its tick when the cursor sits outside the
// viewport vertically (cell-motion mouse mode delivers no events while the
// cursor is held still). Selection stays scoped to the focused group even if
// the cursor wanders into another stripe in split mode.
func (v LogViewer) HandleMouseDrag(localX, localY int) LogViewer {
	g := &v.groups[v.focusedGroup]
	if !g.drag.Active {
		return v
	}
	g.drag.Track(localX, localY)
	idx, ok := v.lineAtScreenInGroup(v.focusedGroup, localX, localY)
	if !ok {
		// Outside the viewport — auto-scroll tick will extend selection.
		return v
	}
	if g.drag.Extend(idx) {
		v.rebuildGroup(v.focusedGroup)
	}
	return v
}

// IsDragging reports whether a drag-select is in progress on the focused
// group. The root model uses this to decide whether to keep the auto-scroll
// tick alive.
func (v LogViewer) IsDragging() bool {
	if len(v.groups) == 0 || v.focusedGroup < 0 || v.focusedGroup >= len(v.groups) {
		return false
	}
	return v.groups[v.focusedGroup].drag.Active
}

// AutoScrollStep performs one viewport scroll step when the last known drag
// cursor position sits above or below the focused group's viewport. selEndLine
// is advanced to the newly-revealed first/last visible line so the selection
// grows to match. No-op if not dragging or the cursor is inside the viewport.
func (v LogViewer) AutoScrollStep() LogViewer {
	if len(v.groups) == 0 {
		return v
	}
	g := &v.groups[v.focusedGroup]
	if !g.drag.Active {
		return v
	}
	_, y1, _, y2, ok := v.viewportBoundsFor(v.focusedGroup)
	if !ok {
		return v
	}
	if g.drag.LastY >= y1 && g.drag.LastY <= y2 {
		return v
	}
	height := g.viewport.Height()
	if height <= 0 {
		return v
	}
	rows := len(g.displayRows)
	if rows == 0 {
		return v
	}
	maxOffset := rows - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	cur := g.viewport.YOffset()
	var next int
	if g.drag.LastY < y1 {
		if cur <= 0 {
			return v
		}
		next = cur - 1
	} else {
		if cur >= maxOffset {
			return v
		}
		next = cur + 1
	}
	g.viewport.SetYOffset(next)
	// Disable autoScroll-to-tail when manually scrolling up; matches keyboard
	// scroll semantics elsewhere in the file. autoScroll is viewer-wide.
	if next < cur {
		v.autoScroll = false
	}
	var endRow int
	if g.drag.LastY < y1 {
		endRow = next
	} else {
		endRow = next + height - 1
		if endRow >= rows {
			endRow = rows - 1
		}
	}
	if endRow < 0 {
		endRow = 0
	}
	newEnd := g.displayRows[endRow]
	g.drag.Extend(newEnd)
	v.rebuildGroup(v.focusedGroup)
	return v
}

// HandleMouseUp finalizes a drag-select. If the user actually dragged
// (cursor moved between lines), copies the selected lines' raw text to the
// clipboard and returns a status string for the status bar. A click without
// drag clears the selection and returns "" (no clipboard write — clicks
// shouldn't clobber the clipboard).
func (v LogViewer) HandleMouseUp(localX, localY int) (LogViewer, string) {
	g := &v.groups[v.focusedGroup]
	if !g.drag.Active {
		return v, ""
	}
	dragged := g.drag.Moved
	lo, hi := g.drag.Range()
	g.clearSelection()
	v.rebuildGroup(v.focusedGroup)
	if !dragged || lo < 0 || hi < 0 || lo >= len(v.lines) {
		return v, ""
	}
	if hi >= len(v.lines) {
		hi = len(v.lines) - 1
	}
	fg := v.groups[v.focusedGroup]
	activeFilter := v.filter
	if v.filterOn {
		activeFilter = v.filterInput
	}
	lowFilter := strings.ToLower(activeFilter)
	var parts []string
	for i := lo; i <= hi; i++ {
		l := v.lines[i]
		if !v.lineBelongsToGroup(l, fg) {
			continue
		}
		if lowFilter != "" {
			var lowText string
			if i < len(v.lowerCache) {
				lowText = v.lowerCache[i]
			} else {
				lowText = strings.ToLower(l.Text)
			}
			if !strings.Contains(lowText, lowFilter) {
				continue
			}
		}
		parts = append(parts, l.Text)
	}
	if len(parts) == 0 {
		return v, ""
	}
	if err := clipboard.WriteAll(strings.Join(parts, "\n")); err != nil {
		return v, "copy failed: " + err.Error()
	}
	return v, fmt.Sprintf("copied %d line%s to clipboard", len(parts), plural(len(parts)))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// tabHitZones reports clickable rectangles for the tab bar in LayoutTabs.
// Coordinates are panel-local: (0,0) is the top-left of the LogViewer's
// outer border.
func (v LogViewer) tabHitZones() []hitZone {
	if v.layout != LayoutTabs || len(v.tabGroups) <= 1 {
		return nil
	}
	// Tab bar lives on the second line inside the border:
	//   row 0: outer border top
	//   row 1: title + scroll status (panel content row 0)
	//   row 2: tab bar           (panel content row 1)
	tabBarY := 1 + 1 // border-top + title row
	tabW := (v.width - 4) / len(v.tabGroups)
	if tabW < 1 {
		tabW = 1
	}
	zones := make([]hitZone, 0, len(v.tabGroups))
	x := 1 // skip border-left
	for i, name := range v.tabGroups {
		label := fmt.Sprintf(" %d:%s ", i+1, name)
		w := len(label)
		if w > tabW && tabW > 5 {
			w = tabW
		}
		zones = append(zones, hitZone{x1: x, y1: tabBarY, x2: x + w - 1, y2: tabBarY, idx: i})
		// Account for the "│" separator between tabs.
		x += w + 1
	}
	return zones
}

// stripeHitZones reports clickable rectangles for stripes in split layouts.
func (v LogViewer) stripeHitZones() []hitZone {
	if v.layout == LayoutTabs {
		return nil
	}
	innerW := max(1, v.width-2)
	innerH := max(1, v.height-2-2)
	n := len(v.groups)
	if n == 0 {
		return nil
	}
	zones := make([]hitZone, 0, n)
	switch v.layout {
	case LayoutHorizontal:
		stripeH := innerH / n
		extra := innerH - stripeH*n
		// Stripes start at row: 1 (border-top) + 1 (title) + 1 (blank) = 3
		y := 1 + 2
		for i := 0; i < n; i++ {
			h := stripeH
			if i == n-1 {
				h += extra
			}
			zones = append(zones, hitZone{x1: 1, y1: y, x2: 1 + innerW - 1, y2: y + h - 1, idx: i})
			y += h
		}
	case LayoutVertical:
		stripeW := innerW / n
		extra := innerW - stripeW*n
		// Stripes start at col: 1 (border-left). Top row inside outer header is 3.
		yTop := 1 + 2
		yBot := yTop + innerH - 1
		x := 1
		for i := 0; i < n; i++ {
			w := stripeW
			if i == n-1 {
				w += extra
			}
			zones = append(zones, hitZone{x1: x, y1: yTop, x2: x + w - 1, y2: yBot, idx: i})
			x += w
		}
	}
	return zones
}
