package panels

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/atotto/clipboard"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
	k8sres "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/k8s/kinds"
	"github.com/chaitanyak/klens/internal/ui/styles"

	corev1 "k8s.io/api/core/v1"
)

// TableAutoScrollTickMsg drives drag-to-copy auto-scroll in the resource
// table when the cursor sits outside the visible row band.
type TableAutoScrollTickMsg struct{}

// TableAutoScrollTickCmd schedules one auto-scroll tick.
func TableAutoScrollTickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return TableAutoScrollTickMsg{}
	})
}

var (
	tableRowCursorBase = lipgloss.NewStyle().Background(styles.ColorSelection).Foreground(styles.ColorWhite)
	tableRowHoverBase  = lipgloss.NewStyle().Background(styles.ColorHover)
	tableRowBase       = lipgloss.NewStyle()
)

// ContentMode controls what is shown in the content panel.
type ContentMode int

const (
	ModeTable ContentMode = iota
	ModeYAML
	ModeEditor
	ModeLogs
	ModeXRay
	ModeMetrics
	ModeEvents
)

// ResourceTable displays a live table of Kubernetes resources.
type ResourceTable struct {
	width    int
	height   int
	focused  bool
	kind     string
	rows     []k8sres.ResourceRow
	filtered []k8sres.ResourceRow
	cursor   int
	selected map[string]bool // selected row names (for multi-select)
	filter   string
	filterOn bool
	filterInput string
	syncing  bool
	// hScroll is the rune offset applied to the resource's Scrollable column
	// (see k8sres.Column.Scrollable). ←/→ keys and horizontal wheel ticks
	// adjust it; SetKind, applyFilter, and WithRows reclamp it.
	hScroll int

	// wrapColIdx, when ≥0, names the column whose value is rendered as
	// multi-line wrapped text at its allotted width. Mutually exclusive with
	// hScroll: SetWrapColumn zeroes hScroll, and the ←/→ keys early-return
	// when wrap is on. Reset to -1 on SetKind so wrap is per-kind state.
	wrapColIdx int

	// sortColIdx is the column index used as the active interactive sort key,
	// or -1 for the kind's default order (newest-first for time-stamped kinds
	// like Event, else natural namespace/name). sortAsc flips direction. '>'
	// cycles the sort column, '<' reverses direction; both reset on kind
	// switch. The actual sort runs in sortRows, called from WithRows and the
	// sort keys.
	sortColIdx int
	sortAsc    bool

	// titleBadge appends a small "· badge" tag to the title row when set.
	// Used by the model to surface state that isn't otherwise visible — e.g.
	// the events-view faults filter, which removes rows but doesn't otherwise
	// announce itself once the toggle's transient feedback fades.
	titleBadge string

	// drag holds drag-to-copy lifecycle state (indices into t.filtered).
	// See DragSelection in drag.go.
	drag DragSelection

	// hoverIdx is the t.filtered index under the mouse pointer (-1 none).
	// Render-only: never affects cursor, selection, or scroll.
	hoverIdx int

	// sbDragActive marks an in-flight scrollbar thumb drag; sbDragGrab is the
	// row offset inside the thumb where it was grabbed, so the thumb tracks
	// the pointer without jumping (herdr's grab_row_offset pattern).
	sbDragActive bool
	sbDragGrab   int
}

func NewResourceTable(w, h int) ResourceTable {
	return ResourceTable{
		width:      w,
		height:     h,
		selected:   make(map[string]bool),
		wrapColIdx: -1,
		sortColIdx: -1,
		sortAsc:    true,
		hoverIdx:   -1,
	}
}

func (t ResourceTable) SetSize(w, h int) ResourceTable { t.width = w; t.height = h; return t }
func (t ResourceTable) SetFocused(f bool) ResourceTable { t.focused = f; return t }
func (t ResourceTable) SetSyncing(s bool) ResourceTable { t.syncing = s; return t }
func (t ResourceTable) SetKind(kind string) ResourceTable {
	if t.kind != kind {
		t.cursor = 0
		t.selected = make(map[string]bool)
		t.filter = ""
		t.filterInput = ""
		t.filterOn = false
		t.hScroll = 0
		t.wrapColIdx = -1
		t.titleBadge = ""
		t.sortColIdx = -1
		t.sortAsc = true
		t.hoverIdx = -1
		t.sbDragActive = false
	}
	t.kind = kind
	return t
}

// scrollbarGeometry mirrors View()'s renderScrollbar call: the scrollbar
// column's outer-relative X, the rows-area top (border-inner Y), the rendered
// track height, and the thumb extent. ok=false when no scrollbar is shown
// (wrap mode, empty table).
func (t ResourceTable) scrollbarGeometry() (sbX, topY, height, thumbPos, thumbSize int, ok bool) {
	if t.WrapActive() || len(t.filtered) == 0 {
		return 0, 0, 0, 0, 0, false
	}
	innerW := max(1, t.width-2)
	dataW := max(1, innerW-1)
	sbX = 1 + dataW // 1 = left border; rows render from outer X 1, width dataW
	topY = t.firstVisibleRowY()
	start := t.scrollStart()
	shown := t.visibleRowCount()
	if rest := len(t.filtered) - start; rest < shown {
		shown = rest
	}
	if shown < 1 {
		shown = 1
	}
	height = shown
	total := len(t.filtered)
	if total <= shown {
		return sbX, topY, height, 0, height, true // full-track thumb, nothing to drag
	}
	thumbSize = max(1, height*shown/total)
	thumbPos = int(float64(start) / float64(max(1, total-shown)) * float64(height-thumbSize))
	return sbX, topY, height, thumbPos, thumbSize, true
}

// scrollToThumbPos moves the cursor so the scroll window matches thumb
// position p. The table has no independent scroll state — scrollStart()
// derives from the cursor — so the drag drives the cursor to the bottom of
// the target window (offset+shown-1), which makes scrollStart() == offset.
func (t ResourceTable) scrollToThumbPos(p int) ResourceTable {
	_, _, height, _, thumbSize, ok := t.scrollbarGeometry()
	if !ok || height <= thumbSize {
		return t
	}
	if p < 0 {
		p = 0
	}
	if p > height-thumbSize {
		p = height - thumbSize
	}
	shown := height
	total := len(t.filtered)
	offset := int(float64(p)/float64(height-thumbSize)*float64(total-shown) + 0.5)
	cursor := offset + shown - 1
	if cursor >= total {
		cursor = total - 1
	}
	if cursor < 0 {
		cursor = 0
	}
	t.cursor = cursor
	return t
}

// HandleScrollbarDown claims a left-click on the scrollbar column: a click
// on the thumb starts a drag; a click on the track jumps there and keeps
// dragging (the thumb re-centers under the pointer).
func (t ResourceTable) HandleScrollbarDown(x, y int) (ResourceTable, bool) {
	sbX, topY, height, thumbPos, thumbSize, ok := t.scrollbarGeometry()
	if !ok || x != sbX {
		return t, false
	}
	ry := y - topY
	if ry < 0 || ry >= height {
		return t, false
	}
	if height <= thumbSize {
		return t, true // full-track thumb: claim the click, nothing to scroll
	}
	if ry >= thumbPos && ry < thumbPos+thumbSize {
		t.sbDragActive = true
		t.sbDragGrab = ry - thumbPos
		return t, true
	}
	t = t.scrollToThumbPos(ry - thumbSize/2)
	t.sbDragActive = true
	t.sbDragGrab = thumbSize / 2
	return t, true
}

// HandleScrollbarDrag tracks an in-flight thumb drag at border-inner Y.
func (t ResourceTable) HandleScrollbarDrag(y int) ResourceTable {
	if !t.sbDragActive {
		return t
	}
	_, topY, _, _, _, ok := t.scrollbarGeometry()
	if !ok {
		t.sbDragActive = false
		return t
	}
	return t.scrollToThumbPos(y - topY - t.sbDragGrab)
}

// HandleScrollbarUp ends a thumb drag. Reports whether one was in flight.
func (t ResourceTable) HandleScrollbarUp() (ResourceTable, bool) {
	was := t.sbDragActive
	t.sbDragActive = false
	return t, was
}

// ScrollbarDragging reports an in-flight scrollbar thumb drag.
func (t ResourceTable) ScrollbarDragging() bool { return t.sbDragActive }

// SetHoverRow marks the filtered-row index under the mouse (-1 clears).
func (t ResourceTable) SetHoverRow(idx int) ResourceTable {
	t.hoverIdx = idx
	return t
}

// RowIndexAt maps a panel-inner Y to an index into the filtered rows, for
// hover hit-testing (clicks use HandleClickAt, which also moves the cursor).
func (t ResourceTable) RowIndexAt(innerY int) (int, bool) {
	return t.rowAtInnerY(innerY)
}

// SetWrapColumn opts the table into multi-line rendering for `idx`. Subsequent
// rows render the named column wrapped at its allotted width; other columns
// pad blank rows to keep the row band aligned. Mutually exclusive with hScroll
// — calling this zeroes the horizontal offset because both modes can't share
// the same column. Negative idx is treated as "off".
func (t ResourceTable) SetWrapColumn(idx int) ResourceTable {
	if idx < 0 {
		return t.ClearWrapColumn()
	}
	t.wrapColIdx = idx
	t.hScroll = 0
	return t
}

// ClearWrapColumn turns wrap rendering off.
func (t ResourceTable) ClearWrapColumn() ResourceTable {
	t.wrapColIdx = -1
	return t
}

// CursorToName moves the cursor to the first filtered row whose Name matches.
// Returns ok=true on a successful move. No-op when the target isn't in the
// current filter window (the caller decides whether to retry).
func (t ResourceTable) CursorToName(name string) (ResourceTable, bool) {
	if name == "" {
		return t, false
	}
	for i, r := range t.filtered {
		if r.Name == name {
			t.cursor = i
			return t, true
		}
	}
	return t, false
}

// WrapActive reports whether the table is currently rendering one of its
// columns as wrapped multi-line text.
func (t ResourceTable) WrapActive() bool { return t.wrapColIdx >= 0 }

// WheelAtBoundary reports whether a wheel event in `button`'s direction would
// be a pure no-op against the table's current state. The root model uses this
// to drop boundary-spam wheel events via tea.WithFilter, skipping the per-msg
// View() cost that would otherwise pile up during trackpad momentum scroll.
func (t ResourceTable) WheelAtBoundary(button tea.MouseButton) bool {
	switch button {
	case tea.MouseWheelUp:
		return t.cursor <= 0
	case tea.MouseWheelDown:
		return len(t.filtered) == 0 || t.cursor >= len(t.filtered)-1
	}
	return false
}

// SetTitleBadge sets the badge text rendered next to the kind name in the
// title row. Pass "" to clear. Cleared automatically on kind change.
func (t ResourceTable) SetTitleBadge(s string) ResourceTable {
	t.titleBadge = s
	return t
}

func (t ResourceTable) SelectedRow() *k8sres.ResourceRow {
	if len(t.filtered) == 0 {
		return nil
	}
	if t.cursor >= len(t.filtered) {
		return nil
	}
	r := t.filtered[t.cursor]
	return &r
}

func (t ResourceTable) SelectedPods() []string {
	var names []string
	for name, ok := range t.selected {
		if ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 && t.SelectedRow() != nil {
		names = append(names, t.SelectedRow().Name)
	}
	return names
}

func (t ResourceTable) SelectedRows() []*k8sres.ResourceRow {
	var rows []*k8sres.ResourceRow
	for i := range t.filtered {
		if t.selected[t.filtered[i].Name] {
			rows = append(rows, &t.filtered[i])
		}
	}
	if len(rows) == 0 {
		if r := t.SelectedRow(); r != nil {
			return []*k8sres.ResourceRow{r}
		}
	}
	return rows
}

func (t ResourceTable) ClearSelection() ResourceTable {
	t.selected = make(map[string]bool)
	return t
}

func (t ResourceTable) SelectionCount() int {
	n := 0
	for _, ok := range t.selected {
		if ok {
			n++
		}
	}
	return n
}

func (t ResourceTable) supportsMultiSelect() bool {
	k, ok := kinds.Lookup(t.kind)
	if !ok {
		return false
	}
	if _, ok := any(k).(kinds.Logger); ok {
		return true
	}
	if _, ok := any(k).(kinds.Deleter); ok {
		return true
	}
	return false
}

func (t ResourceTable) FilterActive() bool { return t.filterOn }
func (t ResourceTable) HasFilter() bool    { return t.filterOn || t.filter != "" }

// HasHScroll reports whether the current resource has an active horizontal
// scroll offset on its Scrollable column. The root model peels this on `esc`
// before falling back to focus-shift.
func (t ResourceTable) HasHScroll() bool { return t.hScroll > 0 && t.scrollableColIdx() >= 0 }

// scrollableColIdx returns the column index marked Scrollable for the current
// kind, or -1 if no column opts in (and so the resource doesn't support
// horizontal scrolling). The first Scrollable column wins.
func (t ResourceTable) scrollableColIdx() int {
	desc, ok := k8sres.Resolve(t.kind)
	if !ok {
		return -1
	}
	for i, c := range desc.Columns {
		if c.Scrollable {
			return i
		}
	}
	return -1
}

func (t ResourceTable) Update(msg tea.Msg) (ResourceTable, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if t.filterOn {
			clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(msg.Content)
			t.filterInput += clean
			t.applyFilter()
		}
		return t, nil
	case tea.MouseWheelMsg:
		// Horizontal wheel ticks (trackpad horizontal swipe, tilt-wheel) scroll
		// the resource's Scrollable column. Mirrors the bubbles viewport
		// convention used by the log panel — no modifier required. Wrap mode
		// is mutually exclusive with horizontal scroll, so the wheel ticks are
		// inert there.
		if t.scrollableColIdx() >= 0 && !t.WrapActive() {
			switch msg.Button {
			case tea.MouseWheelLeft:
				t.scrollLeft()
				return t, nil
			case tea.MouseWheelRight:
				t.scrollRight()
				return t, nil
			}
		}
		switch msg.Button {
		case tea.MouseWheelUp:
			if t.cursor > 0 {
				t.cursor--
			}
		case tea.MouseWheelDown:
			if t.cursor < len(t.filtered)-1 {
				t.cursor++
			}
		}
		return t, nil
	case tea.KeyPressMsg:
		if t.filterOn {
			switch msg.String() {
			case "enter":
				t.filterOn = false
				t.filter = t.filterInput
				t.applyFilter()
			case "esc":
				t.filterOn = false
				t.filterInput = ""
				t.filter = ""
				t.applyFilter()
			case "backspace":
				if len(t.filterInput) > 0 {
					t.filterInput = t.filterInput[:len(t.filterInput)-1]
					t.applyFilter()
				}
			case "ctrl+v":
				if text, err := clipboard.ReadAll(); err == nil && text != "" {
					clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
					t.filterInput += clean
					t.applyFilter()
				}
			default:
				if len(msg.Text) > 0 {
					t.filterInput += msg.Text
					t.applyFilter()
				}
			}
			return t, nil
		}
		switch msg.String() {
		case "up", "k":
			if t.cursor > 0 {
				t.cursor--
			}
		case "down", "j":
			if t.cursor < len(t.filtered)-1 {
				t.cursor++
			}
		case "g":
			t.cursor = 0
		case "G":
			if len(t.filtered) > 0 {
				t.cursor = len(t.filtered) - 1
			}
		case "/":
			t.filterOn = true
			t.filterInput = t.filter
		case "esc":
			if t.HasHScroll() {
				t.hScroll = 0
				return t, nil
			}
			t.filter = ""
			t.filterInput = ""
			t.applyFilter()
		case "right":
			if t.scrollableColIdx() >= 0 && !t.WrapActive() {
				t.scrollRight()
			}
		case "left":
			if t.scrollableColIdx() >= 0 && !t.WrapActive() {
				t.scrollLeft()
			}
		case "space":
			if t.supportsMultiSelect() {
				if row := t.SelectedRow(); row != nil {
					t.selected[row.Name] = !t.selected[row.Name]
				}
			}
		case ">", "shift+right":
			t.cycleSortColumn()
		case "shift+left":
			t.cycleSortColumnBack()
		case "<":
			t.toggleSortDir()
		case "shift+up":
			t.setSortDir(true)
		case "shift+down":
			t.setSortDir(false)
		}
	}
	return t, nil
}

// visibleRowCount returns how many rows fit in the data area, matching View()'s math.
func (t ResourceTable) visibleRowCount() int {
	innerH := max(1, t.height-2)
	v := innerH - 3
	if t.filterOn || t.filter != "" {
		v--
	}
	if v < 1 {
		v = 1
	}
	return v
}

// scrollStart returns the index of the first visible row (kept in sync with View()).
func (t ResourceTable) scrollStart() int {
	visible := t.visibleRowCount()
	if t.cursor >= visible {
		return t.cursor - visible + 1
	}
	return 0
}

// firstVisibleRowY returns the inner-Y of the first data row. Title (0),
// header (1), and an optional filter bar precede the rows. Mirrors the layout
// used in View() and HandleClickAt.
func (t ResourceTable) firstVisibleRowY() int {
	if t.filterOn || t.filter != "" {
		return 3
	}
	return 2
}

// rowAtInnerY converts an inner-Y coordinate to an index into t.filtered.
// Returns ok=false for clicks above the first row or below the last visible
// row. In wrap mode, rows have variable terminal-line height — the search
// walks from the wrap-aware start summing per-row heights.
func (t ResourceTable) rowAtInnerY(innerY int) (int, bool) {
	firstY := t.firstVisibleRowY()
	if innerY < firstY {
		return 0, false
	}
	target := innerY - firstY
	if !t.WrapActive() {
		rowIdx := target + t.scrollStart()
		if rowIdx < 0 || rowIdx >= len(t.filtered) {
			return 0, false
		}
		return rowIdx, true
	}
	wrapColW := t.wrapColumnWidth()
	if wrapColW <= 0 {
		return 0, false
	}
	budget := t.visibleRowCount()
	start := t.scrollStartWrap(budget, wrapColW)
	consumed := 0
	for i := start; i < len(t.filtered); i++ {
		h := t.rowLineCount(i, wrapColW)
		if target < consumed+h {
			return i, true
		}
		consumed += h
		if consumed >= budget {
			break
		}
	}
	return 0, false
}

// wrapColumnWidth returns the rendered width of the wrap column, or 0 when
// wrap is off / the column index doesn't fit the active kind.
func (t ResourceTable) wrapColumnWidth() int {
	if t.wrapColIdx < 0 {
		return 0
	}
	desc, ok := k8sres.Resolve(t.kind)
	if !ok {
		return 0
	}
	if t.wrapColIdx >= len(desc.Columns) {
		return 0
	}
	innerW := max(1, t.width-2)
	dataW := max(1, innerW-1)
	colWidths := computeColWidths(desc.Columns, dataW)
	if t.wrapColIdx >= len(colWidths) {
		return 0
	}
	return colWidths[t.wrapColIdx]
}

// rowLineCount returns the number of terminal lines row i will occupy in
// wrap mode at the given wrap-column width. Returns 1 in non-wrap mode.
func (t ResourceTable) rowLineCount(i, wrapColW int) int {
	if t.wrapColIdx < 0 || wrapColW <= 0 {
		return 1
	}
	if i < 0 || i >= len(t.filtered) {
		return 1
	}
	val := ""
	if t.wrapColIdx < len(t.filtered[i].Values) {
		val = t.filtered[i].Values[t.wrapColIdx]
	}
	if val == "" {
		return 1
	}
	wrapped := xansi.Wrap(val, wrapColW, "")
	return strings.Count(wrapped, "\n") + 1
}

// scrollStartWrap is the wrap-aware analogue of scrollStart: it anchors the
// cursor row at the bottom of the budget and walks backward until adding the
// next row would exceed the line budget. If the cursor row alone already
// exceeds the budget, returns cursor — that row is shown clamped at top.
func (t ResourceTable) scrollStartWrap(budget, wrapColW int) int {
	if len(t.filtered) == 0 {
		return 0
	}
	cursor := t.cursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(t.filtered) {
		cursor = len(t.filtered) - 1
	}
	used := t.rowLineCount(cursor, wrapColW)
	if used >= budget {
		return cursor
	}
	start := cursor
	for s := cursor - 1; s >= 0; s-- {
		h := t.rowLineCount(s, wrapColW)
		if used+h > budget {
			break
		}
		used += h
		start = s
	}
	return start
}

// IsDragging reports whether a drag-select is in progress.
func (t ResourceTable) IsDragging() bool { return t.drag.Active }

// HandleMouseDown starts a drag-select on the row under (innerX, innerY).
// Multi-select is NOT toggled here — that happens in HandleMouseUp for
// clicks that didn't drag, preserving the prior single-click semantics.
func (t ResourceTable) HandleMouseDown(innerX, innerY int) (ResourceTable, bool) {
	row, ok := t.rowAtInnerY(innerY)
	if !ok {
		return t, false
	}
	t.drag.Begin(row, innerX, innerY)
	t.cursor = row
	return t, true
}

// HandleMouseDrag extends the drag-select. Auto-scroll past the visible band
// is handled separately by AutoScrollStep, which fires on a tick.
func (t ResourceTable) HandleMouseDrag(innerX, innerY int) ResourceTable {
	if !t.drag.Active {
		return t
	}
	t.drag.Track(innerX, innerY)
	row, ok := t.rowAtInnerY(innerY)
	if !ok {
		return t
	}
	if t.drag.Extend(row) {
		t.cursor = row
	}
	return t
}

// HandleMouseUp finalises a drag-select. A real drag copies the [lo..hi]
// range of resource names to the clipboard. A click without drag falls back
// to the single-click behaviour: toggle multi-select on the clicked row.
func (t ResourceTable) HandleMouseUp(innerX, innerY int) (ResourceTable, string) {
	if !t.drag.Active {
		return t, ""
	}
	dragged := t.drag.Moved
	lo, hi := t.drag.Range()
	t.drag.Reset()
	if !dragged {
		if t.supportsMultiSelect() && lo >= 0 && lo < len(t.filtered) {
			t.selected[t.filtered[lo].Name] = !t.selected[t.filtered[lo].Name]
		}
		return t, ""
	}
	if lo < 0 || hi < 0 || lo >= len(t.filtered) {
		return t, ""
	}
	if hi >= len(t.filtered) {
		hi = len(t.filtered) - 1
	}
	parts := make([]string, 0, hi-lo+1)
	for i := lo; i <= hi; i++ {
		row := t.filtered[i]
		if t.kind == "Event" && len(row.Values) > 6 {
			parts = append(parts, row.Name+": "+row.Values[6])
		} else {
			parts = append(parts, row.Name)
		}
	}
	if err := clipboard.WriteAll(strings.Join(parts, "\n")); err != nil {
		return t, "copy failed: " + err.Error()
	}
	n := hi - lo + 1
	noun := "name"
	if n != 1 {
		noun = "names"
	}
	if t.kind == "Event" {
		noun = "event"
		if n != 1 {
			noun = "events"
		}
	}
	return t, fmt.Sprintf("copied %d %s", n, noun)
}

// AutoScrollStep advances the cursor (and hence scrollStart) by one row when
// the last drag cursor sat above or below the visible row band.
func (t ResourceTable) AutoScrollStep() ResourceTable {
	if !t.drag.Active || len(t.filtered) == 0 {
		return t
	}
	firstY := t.firstVisibleRowY()
	lastY := firstY + t.visibleRowCount() - 1
	if t.drag.LastY >= firstY && t.drag.LastY <= lastY {
		return t
	}
	if t.drag.LastY < firstY {
		if t.cursor <= 0 {
			return t
		}
		t.cursor--
	} else {
		if t.cursor >= len(t.filtered)-1 {
			return t
		}
		t.cursor++
	}
	t.drag.Extend(t.cursor)
	return t
}

// HandleClickAt moves the cursor to the row at panel-inner-Y. If leftClick,
// also toggles multi-select on that row (same as pressing space).
// Returns true if the click landed on a real row (not title / header / filter / empty area).
func (t ResourceTable) HandleClickAt(innerY int, leftClick bool) (ResourceTable, bool) {
	rowIdx, ok := t.rowAtInnerY(innerY)
	if !ok {
		return t, false
	}
	t.cursor = rowIdx
	if leftClick && t.supportsMultiSelect() {
		if row := t.SelectedRow(); row != nil {
			t.selected[row.Name] = !t.selected[row.Name]
		}
	}
	return t, true
}

// headerColTextOffset is the inner-X at which the first column header's text
// begins: 1 for the panel's left border + 1 for styles.TableHeader's left
// padding. Clicks left of this (border / padding) don't map to a column.
const headerColTextOffset = 2

// headerRowInnerY returns the inner-Y of the column-header row. The header sits
// one line above the first data row (after the title and the optional filter
// bar), so it tracks firstVisibleRowY.
func (t ResourceTable) headerRowInnerY() int {
	return t.firstVisibleRowY() - 1
}

// columnAtX maps an inner-X coordinate on the header row to a column index,
// mirroring buildHeader's geometry (headerColTextOffset leading cells, then
// each column padded to its computed width and joined by a single separator
// space). The trailing separator is folded into the preceding column's
// hit-area so there are no dead gaps between headers. Returns ok=false for
// clicks left of the first column or past the last (e.g. the reserved
// scrollbar column).
func (t ResourceTable) columnAtX(innerX int) (int, bool) {
	desc, ok := k8sres.Resolve(t.kind)
	if !ok {
		return 0, false
	}
	innerW := max(1, t.width-2)
	dataW := max(1, innerW-1)
	colWidths := computeColWidths(desc.Columns, dataW)
	adjusted := innerX - headerColTextOffset
	if adjusted < 0 {
		return 0, false
	}
	acc := 0
	for i, w := range colWidths {
		span := w
		if i < len(colWidths)-1 {
			span = w + 1 // fold the trailing separator into this column
		}
		if adjusted < acc+span {
			return i, true
		}
		acc += span
	}
	return 0, false
}

// HandleHeaderClickAt sorts by the column under (innerX, innerY) when the click
// lands on the column-header row. Clicking the already-active sort column
// toggles its direction; clicking a different column makes it the active sort
// key, ascending — matching the mouse semantics of typical GUI tables. Returns
// ok=false (state untouched) for clicks that aren't on the header row or don't
// map to a column, so the caller can fall through to row drag-select.
func (t ResourceTable) HandleHeaderClickAt(innerX, innerY int) (ResourceTable, bool) {
	if innerY != t.headerRowInnerY() {
		return t, false
	}
	idx, ok := t.columnAtX(innerX)
	if !ok {
		return t, false
	}
	if idx == t.sortColIdx {
		t.sortAsc = !t.sortAsc
	} else {
		t.sortColIdx = idx
		t.sortAsc = true
	}
	t.applySort()
	return t, true
}

func (t *ResourceTable) applyFilter() {
	if t.filterInput == "" {
		t.filtered = t.rows
	} else {
		low := strings.ToLower(t.filterInput)
		// Allocate a new slice to avoid corrupting t.rows when t.filtered shares
		// its backing array (set via t.filtered = t.rows when no filter is active).
		filtered := make([]k8sres.ResourceRow, 0, len(t.rows))
		for _, r := range t.rows {
			if strings.Contains(strings.ToLower(r.Name), low) {
				filtered = append(filtered, r)
			}
		}
		t.filtered = filtered
	}
	if t.cursor >= len(t.filtered) {
		t.cursor = max(0, len(t.filtered)-1)
	}
	// A filter change may have removed every row whose Scrollable column
	// extended past the current offset. Reclamp so we never display a fully
	// empty column.
	t.clampHScroll()
}

// hScrollStep is the rune step applied per left/right press (or horizontal
// wheel tick) on a Scrollable column.
const hScrollStep = 10

// maxScrollableRunes returns the longest rune-count among the filtered rows
// for the active Scrollable column. Returns 0 if no column opts in, or no
// rows are visible.
func (t ResourceTable) maxScrollableRunes() int {
	idx := t.scrollableColIdx()
	if idx < 0 {
		return 0
	}
	maxN := 0
	for _, r := range t.filtered {
		if idx < len(r.Values) {
			if n := utf8.RuneCountInString(r.Values[idx]); n > maxN {
				maxN = n
			}
		}
	}
	return maxN
}

// clampHScroll caps hScroll to (max-1) so at least one rune of the longest
// visible cell stays on screen, and floors at 0.
func (t *ResourceTable) clampHScroll() {
	if t.hScroll <= 0 {
		t.hScroll = 0
		return
	}
	maxN := t.maxScrollableRunes()
	if maxN == 0 {
		t.hScroll = 0
		return
	}
	if t.hScroll >= maxN {
		t.hScroll = maxN - 1
	}
}

// scrollLeft shifts hScroll one step to the left, floored at 0.
func (t *ResourceTable) scrollLeft() {
	t.hScroll -= hScrollStep
	if t.hScroll < 0 {
		t.hScroll = 0
	}
}

// scrollRight shifts hScroll one step to the right, capped so at least one
// rune of the longest visible cell stays on screen.
func (t *ResourceTable) scrollRight() {
	maxN := t.maxScrollableRunes()
	if maxN == 0 {
		return
	}
	if t.hScroll+hScrollStep < maxN {
		t.hScroll += hScrollStep
	} else {
		t.hScroll = maxN - 1
	}
}

func (t ResourceTable) View() string {
	border := styles.NormalBorder
	if t.focused {
		border = styles.FocusedBorder
	}
	innerW := max(1, t.width-2)
	// Reserve the rightmost inner column for the vertical scrollbar so users
	// always have a "where am I" cue when key- or wheel-scrolling.
	dataW := max(1, innerW-1)

	desc, ok := k8sres.Resolve(t.kind)
	if !ok {
		return border.Width(t.width).Height(t.height).Render(
			styles.Muted.Render("  Select a resource type from the left panel"))
	}

	// Title row — append cursor position (e.g. "12/45  ·  27%") so the user
	// can read where they are in the data without consulting the scrollbar.
	countInfo := fmt.Sprintf(" %d", len(t.filtered))
	if t.filter != "" {
		countInfo = fmt.Sprintf(" %d/%d", len(t.filtered), len(t.rows))
	}
	title := styles.Title.Render(t.kind) + styles.Muted.Render(countInfo)
	if t.titleBadge != "" {
		title += styles.Warning.Render("  ·  " + t.titleBadge)
	}
	if n := t.SelectionCount(); n > 0 {
		title += styles.Primary.Render(fmt.Sprintf("  ·  %d selected", n))
	}
	if sl := t.SortLabel(); sl != "" {
		title += styles.Muted.Render("  ·  sort " + sl)
	}
	if pos := t.cursorPositionLabel(); pos != "" {
		title += styles.Muted.Render("  " + pos)
	}

	// Filter bar
	filterBar := ""
	if t.filterOn {
		filterBar = "\n" + styles.Primary.Render("filter: ") + t.filterInput + styles.Muted.Render("█")
	} else if t.filter != "" {
		filterBar = "\n" + styles.Primary.Render("filter: ") + styles.Warning.Render(t.filter) + styles.Muted.Render("  (/ to change, esc to clear)")
	}

	// Header
	colWidths := computeColWidths(desc.Columns, dataW)
	header := buildHeader(desc, colWidths, t.sortColIdx, t.sortAsc)
	scrollIdx := t.scrollableColIdx()
	if !t.WrapActive() && scrollIdx >= 0 && t.hScroll > 0 {
		header = buildHeaderWithHScroll(desc, colWidths, scrollIdx, t.hScroll, t.sortColIdx, t.sortAsc)
	}

	// Rows
	budget := t.visibleRowCount()
	var start int
	var rowLines []string
	visibleRowsShown := 0

	dragLo, dragHi := -1, -1
	if t.drag.Active {
		dragLo, dragHi = t.drag.Range()
	}

	if t.WrapActive() {
		wrapColW := 0
		if t.wrapColIdx < len(colWidths) {
			wrapColW = colWidths[t.wrapColIdx]
		}
		start = t.scrollStartWrap(budget, wrapColW)
		used := 0
		for i := start; i < len(t.filtered); i++ {
			row := t.filtered[i]
			sel := t.selected[row.Name]
			inDrag := dragLo >= 0 && i >= dragLo && i <= dragHi
			isCursor := i == t.cursor || inDrag
			block := buildWrappedRow(row, desc, dataW, colWidths, sel, isCursor, t.wrapColIdx)
			if used+len(block) > budget {
				if used == 0 {
					// Cursor row alone exceeds budget — show as much as fits so
					// the user can still see they're on the right line.
					if len(block) > budget {
						block = block[:budget]
					}
					rowLines = append(rowLines, block...)
					visibleRowsShown++
				}
				break
			}
			rowLines = append(rowLines, block...)
			used += len(block)
			visibleRowsShown++
		}
	} else {
		start = t.scrollStart()
		for i := start; i < len(t.filtered) && i < start+budget; i++ {
			row := t.filtered[i]
			sel := t.selected[row.Name]
			inDrag := dragLo >= 0 && i >= dragLo && i <= dragHi
			isCursor := i == t.cursor || inDrag

			if scrollIdx >= 0 && t.hScroll > 0 && scrollIdx < len(row.Values) {
				row = applyHScroll(row, scrollIdx, t.hScroll)
			}

			hov := i == t.hoverIdx && !isCursor && !sel
			line := buildRow(row, desc, dataW, colWidths, sel, isCursor, hov)
			rowLines = append(rowLines, line)
			visibleRowsShown++
		}
	}

	if len(t.filtered) == 0 {
		if t.syncing {
			rowLines = []string{styles.Muted.Render("  Syncing resources...")}
		} else {
			rowLines = []string{styles.Muted.Render("  No resources found")}
		}
	}

	// Vertical scrollbar over the rows area. Use the cursor position to drive
	// the thumb so both key navigation and mouse-wheel cursor moves update it.
	rowsBlock := strings.Join(rowLines, "\n")
	if len(t.filtered) > 0 {
		sb := renderScrollbar(len(rowLines), max(1, visibleRowsShown), len(t.filtered), start, t.focused)
		rowsBlock = joinScrollbar(rowsBlock, sb)
	}

	content := title + filterBar + "\n" + header + "\n" + rowsBlock
	return border.Width(t.width).Height(t.height).Render(content)
}

// cursorPositionLabel returns "i/N · P%" describing where the row cursor sits
// in the filtered data. Empty when there's nothing to show (no rows, or one
// row where position is trivially obvious).
func (t ResourceTable) cursorPositionLabel() string {
	n := len(t.filtered)
	if n <= 1 {
		return ""
	}
	pos := t.cursor + 1
	if pos < 1 {
		pos = 1
	}
	if pos > n {
		pos = n
	}
	pct := 0
	if n > 1 {
		pct = int(float64(pos-1) / float64(n-1) * 100)
	}
	return fmt.Sprintf("%d/%d · %d%%", pos, n, pct)
}

func computeColWidths(cols []k8sres.Column, width int) []int {
	if len(cols) == 0 {
		return nil
	}
	n := len(cols)
	// width available to columns = total - 2-char prefix - (n-1) single-space separators
	available := width - 2 - (n - 1)

	declared := 0
	var flexIdx []int
	widths := make([]int, n)
	for i, c := range cols {
		widths[i] = c.Width
		declared += c.Width
		if c.Flex {
			flexIdx = append(flexIdx, i)
		}
	}

	switch {
	case declared <= available && len(flexIdx) > 0:
		// Wide enough: distribute surplus equally among flex columns.
		surplus := available - declared
		per := surplus / len(flexIdx)
		rem := surplus % len(flexIdx)
		for k, idx := range flexIdx {
			widths[idx] += per
			if k < rem {
				widths[idx]++
			}
		}
	case declared > available:
		// Narrow terminal: fall back to equal-cap behavior so nothing overflows.
		maxW := max(1, available/n)
		for i := range widths {
			if widths[i] > maxW {
				widths[i] = maxW
			}
		}
	}
	return widths
}

// applyHScroll returns a copy of row with Values[colIdx] shifted left by
// scroll runes and prefixed with `‹` to indicate the truncated-left state.
// Rune-aware so multi-byte content (paths, names with non-ASCII chars from
// regional clusters) doesn't get sliced mid-codepoint.
func applyHScroll(row k8sres.ResourceRow, colIdx, scroll int) k8sres.ResourceRow {
	if scroll <= 0 || colIdx < 0 || colIdx >= len(row.Values) {
		return row
	}
	runes := []rune(row.Values[colIdx])
	offset := scroll
	if offset > len(runes) {
		offset = len(runes)
	}
	shifted := "‹" + string(runes[offset:])
	newValues := make([]string, len(row.Values))
	copy(newValues, row.Values)
	newValues[colIdx] = shifted
	row.Values = newValues
	return row
}

// buildHeaderWithHScroll annotates the Scrollable column header with the
// active rune offset so users can see how far they've scrolled.
func buildHeaderWithHScroll(desc k8sres.ResourceDescriptor, colWidths []int, scrollIdx, scroll, sortIdx int, sortAsc bool) string {
	cols := desc.Columns
	parts := make([]string, len(cols))
	for i, c := range cols {
		text := c.Header
		if i == scrollIdx {
			text = fmt.Sprintf("%s +%d", c.Header, scroll)
		}
		parts[i] = padOrTrunc(headerSortText(text, i == sortIdx, sortAsc), colWidths[i])
	}
	return styles.TableHeader.Render(strings.Join(parts, " "))
}

func buildHeader(desc k8sres.ResourceDescriptor, colWidths []int, sortIdx int, sortAsc bool) string {
	cols := desc.Columns
	var parts []string
	for i, c := range cols {
		parts = append(parts, padOrTrunc(headerSortText(c.Header, i == sortIdx, sortAsc), colWidths[i]))
	}
	line := strings.Join(parts, " ")
	return styles.TableHeader.Render(line)
}

// headerSortText appends a direction arrow to the active sort column's header.
func headerSortText(header string, active, asc bool) string {
	if !active {
		return header
	}
	if asc {
		return header + " ▲"
	}
	return header + " ▼"
}

func buildRow(row k8sres.ResourceRow, desc k8sres.ResourceDescriptor, width int, colWidths []int, selected, cursor, hovered bool) string {
	prefix := "  "
	if selected {
		prefix = "✓ "
	}

	cols := desc.Columns
	var values []string
	if len(row.Values) > 0 {
		values = row.Values
	} else {
		values = append([]string{row.Name}, append([]string{row.Status, row.Age}, row.Extra...)...)
	}

	var parts []string
	for i := range cols {
		w := colWidths[i]
		val := ""
		if i < len(values) {
			val = values[i]
		}
		if row.Status != "" && val == row.Status {
			parts = append(parts, styles.StatusStyle(row.Status).Render(padOrTrunc(val, w)))
		} else {
			parts = append(parts, padOrTrunc(val, w))
		}
	}
	line := prefix + strings.Join(parts, " ")
	if lipgloss.Width(line) > width {
		plain := ansiEscape.ReplaceAllString(line, "")
		if len(plain) > width-1 {
			line = plain[:width-1] + "…"
		} else {
			line = plain
		}
	}

	if cursor {
		return tableRowCursorBase.Width(width).Render(ansiEscape.ReplaceAllString(line, ""))
	}
	if hovered {
		return tableRowHoverBase.Width(width).Render(line)
	}
	return tableRowBase.Width(width).Render(line)
}

// buildWrappedRow renders a single row across multiple terminal lines, with
// the column at wrapIdx wrapped at its allotted width. Non-wrap columns
// render their value on the first line and pad to blank on continuation
// lines, keeping the row band aligned. Cursor highlight spans every line in
// the block.
func buildWrappedRow(row k8sres.ResourceRow, desc k8sres.ResourceDescriptor, width int, colWidths []int, selected, cursor bool, wrapIdx int) []string {
	if wrapIdx < 0 || wrapIdx >= len(colWidths) {
		return []string{buildRow(row, desc, width, colWidths, selected, cursor, false)}
	}
	cols := desc.Columns
	values := row.Values
	if len(values) == 0 {
		values = append([]string{row.Name}, append([]string{row.Status, row.Age}, row.Extra...)...)
	}
	wrapW := colWidths[wrapIdx]
	if wrapW <= 0 {
		return []string{buildRow(row, desc, width, colWidths, selected, cursor, false)}
	}
	wrapVal := ""
	if wrapIdx < len(values) {
		wrapVal = values[wrapIdx]
	}
	wrapped := xansi.Wrap(wrapVal, wrapW, "")
	segs := strings.Split(wrapped, "\n")
	if len(segs) == 0 {
		segs = []string{""}
	}

	out := make([]string, 0, len(segs))
	prefix := "  "
	if selected {
		prefix = "✓ "
	}
	for li, seg := range segs {
		parts := make([]string, 0, len(cols))
		for i := range cols {
			w := colWidths[i]
			if i == wrapIdx {
				parts = append(parts, padOrTrunc(seg, w))
				continue
			}
			if li == 0 {
				val := ""
				if i < len(values) {
					val = values[i]
				}
				if row.Status != "" && val == row.Status {
					parts = append(parts, styles.StatusStyle(row.Status).Render(padOrTrunc(val, w)))
				} else {
					parts = append(parts, padOrTrunc(val, w))
				}
			} else {
				parts = append(parts, strings.Repeat(" ", w))
			}
		}
		linePrefix := prefix
		if li > 0 {
			linePrefix = "  "
		}
		line := linePrefix + strings.Join(parts, " ")
		if lipgloss.Width(line) > width {
			plain := ansiEscape.ReplaceAllString(line, "")
			if len(plain) > width-1 {
				line = plain[:width-1] + "…"
			} else {
				line = plain
			}
		}
		if cursor {
			out = append(out, tableRowCursorBase.Width(width).Render(ansiEscape.ReplaceAllString(line, "")))
		} else {
			out = append(out, tableRowBase.Width(width).Render(line))
		}
	}
	return out
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func padOrTrunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	visW := lipgloss.Width(s)
	if visW > w {
		// Strip ANSI codes before slicing to avoid cutting mid-escape-sequence.
		plain := ansiEscape.ReplaceAllString(s, "")
		if len(plain) > w {
			return plain[:w-1] + "…"
		}
		return plain + strings.Repeat(" ", w-len(plain))
	}
	return s + strings.Repeat(" ", w-visW)
}

// PatchValuesByName overlays Values + Status on existing rows whose Name
// matches an incoming row, without resorting or recomputing the filter slice.
// Used on metrics-tick refresh of the Pod table where pod identities and order
// are unchanged but a few columns (CPU/MEM and percentages) need new values.
// O(N) and avoids the O(N log N) sort + style allocation that WithRows does.
func (t ResourceTable) PatchValuesByName(rows []k8sres.ResourceRow) ResourceTable {
	if len(t.rows) == 0 || len(rows) == 0 {
		return t
	}
	byName := make(map[string]int, len(rows))
	for i, r := range rows {
		byName[r.Name] = i
	}
	for i := range t.rows {
		j, ok := byName[t.rows[i].Name]
		if !ok {
			continue
		}
		t.rows[i].Values = rows[j].Values
		t.rows[i].Status = rows[j].Status
	}
	// Re-derive the filtered subslice without resorting. applyFilter is O(N)
	// substring scan; if no filter is active we just alias filtered to rows.
	if t.filterInput != "" {
		t.applyFilter()
	} else {
		t.filtered = t.rows
	}
	return t
}

// PopulateRows converts raw k8s objects into ResourceRows for the given kind.
func (t ResourceTable) WithRows(rows []k8sres.ResourceRow) ResourceTable {
	t.rows = rows
	t.sortRows()
	// Reapply filter — use filterInput so in-progress (uncommitted) filters survive refreshes.
	if t.filterInput != "" {
		t.applyFilter()
	} else {
		t.filtered = t.rows
	}
	if t.cursor >= len(t.filtered) {
		t.cursor = max(0, len(t.filtered)-1)
	}
	t.clampHScroll()
	return t
}

// columns returns the current kind's column layout, or nil when the kind is
// unknown (passthrough/YAML-only kinds the table never renders).
func (t ResourceTable) columns() []k8sres.Column {
	if desc, ok := k8sres.Resolve(t.kind); ok {
		return desc.Columns
	}
	return nil
}

// sortRows orders t.rows in place. When a column sort is active (sortColIdx >=
// 0) it compares that column's rendered cells via CellCompare under the
// column's SortType, honoring sortAsc, and tie-breaks equal cells by the
// natural namespace/name order (RowLess) so the result is a deterministic
// total order. Otherwise it falls back to the kind's default: newest-first for
// time-stamped kinds (Event), else natural namespace/name.
func (t *ResourceTable) sortRows() {
	cols := t.columns()
	sort.SliceStable(t.rows, func(i, j int) bool {
		ri, rj := t.rows[i], t.rows[j]
		if t.sortColIdx >= 0 && t.sortColIdx < len(cols) &&
			t.sortColIdx < len(ri.Values) && t.sortColIdx < len(rj.Values) {
			if c := k8sres.CellCompare(cols[t.sortColIdx].SortType, ri.Values[t.sortColIdx], rj.Values[t.sortColIdx]); c != 0 {
				if !t.sortAsc {
					c = -c
				}
				return c < 0
			}
			return k8sres.RowLess(ri, rj)
		}
		// Default order: time-stamped kinds render newest-first, with the
		// natural (ns, name) comparator as a deterministic tie-break so equal
		// timestamps don't reorder on every refresh.
		if !ri.SortByTime.IsZero() && !ri.SortByTime.Equal(rj.SortByTime) {
			return ri.SortByTime.After(rj.SortByTime)
		}
		return k8sres.RowLess(ri, rj)
	})
}

// cycleSortColumn advances the active sort column ('>' key): default → col 0 →
// col 1 → … → last → default. Switching column resets direction to ascending.
func (t *ResourceTable) cycleSortColumn() {
	n := len(t.columns())
	if n == 0 {
		return
	}
	t.sortColIdx++
	if t.sortColIdx >= n {
		t.sortColIdx = -1
	}
	t.sortAsc = true
	t.applySort()
}

// cycleSortColumnBack steps the active sort column backward (ctrl+left):
// default → last → … → col 0 → default. Mirror image of cycleSortColumn so
// the two arrow keys walk the columns in opposite directions. Switching
// column resets direction to ascending.
func (t *ResourceTable) cycleSortColumnBack() {
	n := len(t.columns())
	if n == 0 {
		return
	}
	if t.sortColIdx < 0 {
		t.sortColIdx = n - 1
	} else {
		// 0 → -1 lands back on the default order; otherwise step left.
		t.sortColIdx--
	}
	t.sortAsc = true
	t.applySort()
}

// toggleSortDir reverses the active column's direction ('<' key). No-op in the
// default (no column) order.
func (t *ResourceTable) toggleSortDir() {
	if t.sortColIdx < 0 {
		return
	}
	t.sortAsc = !t.sortAsc
	t.applySort()
}

// setSortDir forces the active column's direction to ascending (ctrl+up) or
// descending (ctrl+down), unlike toggleSortDir's flip. No-op in the default
// (no column) order — there's no active key to direct — and no-op when the
// direction already matches so we skip a redundant re-sort.
func (t *ResourceTable) setSortDir(asc bool) {
	if t.sortColIdx < 0 || t.sortAsc == asc {
		return
	}
	t.sortAsc = asc
	t.applySort()
}

// applySort re-sorts and re-derives the filtered subslice + cursor after a
// sort-state change, mirroring WithRows' post-sort bookkeeping.
func (t *ResourceTable) applySort() {
	t.sortRows()
	if t.filterInput != "" || t.filter != "" {
		t.applyFilter()
	} else {
		t.filtered = t.rows
	}
	if t.cursor >= len(t.filtered) {
		t.cursor = max(0, len(t.filtered)-1)
	}
}

// SortLabel returns a short "· COL ▲" descriptor for the active sort column,
// or "" when in default order. Surfaced in the table title so the sort state
// stays visible after the keypress feedback fades.
func (t ResourceTable) SortLabel() string {
	cols := t.columns()
	if t.sortColIdx < 0 || t.sortColIdx >= len(cols) {
		return ""
	}
	arrow := "▲"
	if !t.sortAsc {
		arrow = "▼"
	}
	return cols[t.sortColIdx].Header + " " + arrow
}

// PodResourceTotals sums the per-container request/limit values across all
// containers in the pod. Used by the metrics panel to compute the percentage
// graphs alongside latest CPU/MEM samples. Pod row rendering lives in
// internal/k8s/kinds/pod.go after plan 01; this helper stays here until step
// 6 of the migration moves it into kinds/pod.go too.
func PodResourceTotals(p *corev1.Pod) (cpuReqM, cpuLimM, memReqB, memLimB int64) {
	for _, c := range p.Spec.Containers {
		if q, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
			cpuReqM += q.MilliValue()
		}
		if q, ok := c.Resources.Limits[corev1.ResourceCPU]; ok {
			cpuLimM += q.MilliValue()
		}
		if q, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
			memReqB += q.Value()
		}
		if q, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
			memLimB += q.Value()
		}
	}
	return
}

// All Build<Kind>Rows + helper functions (deployStatus, jobStatus, nodeRoles)
// migrated into internal/k8s/kinds/ (plan 01). PodResourceTotals above is the
// last remaining helper here, called from the metrics panel.

