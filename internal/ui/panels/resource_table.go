package panels

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	k8sres "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	tableRowBase       = lipgloss.NewStyle()
	pctFailedStyle     = lipgloss.NewStyle().Foreground(styles.ColorFailed)
	pctPendingStyle    = lipgloss.NewStyle().Foreground(styles.ColorPending)
)

// ContentMode controls what is shown in the content panel.
type ContentMode int

const (
	ModeTable ContentMode = iota
	ModeYAML
	ModeEditor
	ModeLogs
	ModeTopology
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

	// drag holds drag-to-copy lifecycle state (indices into t.filtered).
	// See DragSelection in drag.go.
	drag DragSelection
}

func NewResourceTable(w, h int) ResourceTable {
	return ResourceTable{
		width:    w,
		height:   h,
		selected: make(map[string]bool),
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
	}
	t.kind = kind
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

func (t ResourceTable) FilterActive() bool { return t.filterOn }
func (t ResourceTable) HasFilter() bool    { return t.filterOn || t.filter != "" }

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
		switch msg.Button {
		case tea.MouseWheelUp:
			if len(t.filtered) > 0 {
				t.cursor = (t.cursor - 1 + len(t.filtered)) % len(t.filtered)
			}
		case tea.MouseWheelDown:
			if len(t.filtered) > 0 {
				t.cursor = (t.cursor + 1) % len(t.filtered)
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
			if len(t.filtered) > 0 {
				t.cursor = (t.cursor - 1 + len(t.filtered)) % len(t.filtered)
			}
		case "down", "j":
			if len(t.filtered) > 0 {
				t.cursor = (t.cursor + 1) % len(t.filtered)
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
			t.filter = ""
			t.filterInput = ""
			t.applyFilter()
		case "space":
			if row := t.SelectedRow(); row != nil {
				t.selected[row.Name] = !t.selected[row.Name]
			}
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
// row.
func (t ResourceTable) rowAtInnerY(innerY int) (int, bool) {
	firstY := t.firstVisibleRowY()
	if innerY < firstY {
		return 0, false
	}
	rowIdx := (innerY - firstY) + t.scrollStart()
	if rowIdx < 0 || rowIdx >= len(t.filtered) {
		return 0, false
	}
	return rowIdx, true
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
		if lo >= 0 && lo < len(t.filtered) {
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
		parts = append(parts, t.filtered[i].Name)
	}
	if err := clipboard.WriteAll(strings.Join(parts, "\n")); err != nil {
		return t, "copy failed: " + err.Error()
	}
	n := hi - lo + 1
	noun := "name"
	if n != 1 {
		noun = "names"
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
	firstRowY := 2 // title (line 0) + header (line 1)
	if t.filterOn || t.filter != "" {
		firstRowY = 3 // + filter bar
	}
	if innerY < firstRowY {
		return t, false
	}
	rowIdx := (innerY - firstRowY) + t.scrollStart()
	if rowIdx < 0 || rowIdx >= len(t.filtered) {
		return t, false
	}
	t.cursor = rowIdx
	if leftClick {
		if row := t.SelectedRow(); row != nil {
			t.selected[row.Name] = !t.selected[row.Name]
		}
	}
	return t, true
}

func (t *ResourceTable) applyFilter() {
	if t.filterInput == "" {
		t.filtered = t.rows
		return
	}
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
	if t.cursor >= len(t.filtered) {
		t.cursor = max(0, len(t.filtered)-1)
	}
}

func (t ResourceTable) View() string {
	border := styles.NormalBorder
	if t.focused {
		border = styles.FocusedBorder
	}
	innerW := max(1, t.width-2)

	desc, ok := k8sres.Resolve(t.kind)
	if !ok {
		return border.Width(t.width).Height(t.height).Render(
			styles.Muted.Render("  Select a resource type from the left panel"))
	}

	// Title row
	countInfo := fmt.Sprintf(" %d", len(t.filtered))
	if t.filter != "" {
		countInfo = fmt.Sprintf(" %d/%d", len(t.filtered), len(t.rows))
	}
	title := styles.Title.Render(t.kind) + styles.Muted.Render(countInfo)
	if n := t.SelectionCount(); n > 0 {
		title += styles.Primary.Render(fmt.Sprintf("  ·  %d selected", n))
	}

	// Filter bar
	filterBar := ""
	if t.filterOn {
		filterBar = "\n" + styles.Primary.Render("filter: ") + t.filterInput + styles.Muted.Render("█")
	} else if t.filter != "" {
		filterBar = "\n" + styles.Primary.Render("filter: ") + styles.Warning.Render(t.filter) + styles.Muted.Render("  (/ to change, esc to clear)")
	}

	// Header
	colWidths := computeColWidths(desc.Columns, innerW)
	header := buildHeader(desc, colWidths)

	// Rows
	visibleRows := t.visibleRowCount()
	start := t.scrollStart()

	dragLo, dragHi := -1, -1
	if t.drag.Active {
		dragLo, dragHi = t.drag.Range()
	}

	var rowLines []string
	for i := start; i < len(t.filtered) && i < start+visibleRows; i++ {
		row := t.filtered[i]
		sel := t.selected[row.Name]
		inDrag := dragLo >= 0 && i >= dragLo && i <= dragHi
		isCursor := i == t.cursor || inDrag

		line := buildRow(row, desc, innerW, colWidths, sel, isCursor)
		rowLines = append(rowLines, line)
	}

	if len(t.filtered) == 0 {
		if t.syncing {
			rowLines = []string{styles.Muted.Render("  Syncing resources...")}
		} else {
			rowLines = []string{styles.Muted.Render("  No resources found")}
		}
	}

	content := title + filterBar + "\n" + header + "\n" + strings.Join(rowLines, "\n")
	return border.Width(t.width).Height(t.height).Render(content)
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

func buildHeader(desc k8sres.ResourceDescriptor, colWidths []int) string {
	cols := desc.Columns
	var parts []string
	for i, c := range cols {
		parts = append(parts, padOrTrunc(c.Header, colWidths[i]))
	}
	line := strings.Join(parts, " ")
	return styles.TableHeader.Render(line)
}

func buildRow(row k8sres.ResourceRow, desc k8sres.ResourceDescriptor, width int, colWidths []int, selected, cursor bool) string {
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
		return tableRowCursorBase.Width(width).Render(line)
	}
	return tableRowBase.Width(width).Render(line)
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
	sort.Slice(t.rows, func(i, j int) bool {
		if !t.rows[i].SortByTime.IsZero() {
			return t.rows[i].SortByTime.After(t.rows[j].SortByTime)
		}
		return t.rows[i].Name < t.rows[j].Name
	})
	// Reapply filter — use filterInput so in-progress (uncommitted) filters survive refreshes.
	if t.filterInput != "" {
		t.applyFilter()
	} else {
		t.filtered = t.rows
	}
	if t.cursor >= len(t.filtered) {
		t.cursor = max(0, len(t.filtered)-1)
	}
	return t
}

// BuildPodRows converts pod list to resource rows.
// Columns: NAME, READY, STATUS, RESTARTS, AGE, CPU, %CPU/R, %CPU/L, MEM, %MEM/R, %MEM/L
func BuildPodRows(pods []*corev1.Pod, metricsData k8sres.MetricsUpdatedMsg) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(pods))
	for _, p := range pods {
		ready := 0
		total := len(p.Spec.Containers)
		restarts := 0
		status := string(p.Status.Phase)
		waitingFound := false
		for _, cs := range p.Status.ContainerStatuses {
			if cs.Ready {
				ready++
			}
			restarts += int(cs.RestartCount)
			if !waitingFound && cs.State.Waiting != nil {
				status = cs.State.Waiting.Reason
				waitingFound = true
			}
		}
		if p.DeletionTimestamp != nil {
			status = "Terminating"
		}
		age := k8sres.AgeString(p.CreationTimestamp)

		cpuStr, cpuRStr, cpuLStr := "n/a", "~", "~"
		memStr, memRStr, memLStr := "n/a", "~", "~"
		if rm := metricsData.Pods[p.Namespace+"/"+p.Name]; rm != nil {
			cpuM := int64(rm.CPULatest)
			memMi := int64(rm.MEMLatest) / (1024 * 1024)
			cpuStr = fmt.Sprintf("%d", cpuM)
			memStr = fmt.Sprintf("%d", memMi)
			cpuReqM, cpuLimM, memReqB, memLimB := PodResourceTotals(p)
			cpuRStr = fmtPctColored(cpuM, cpuReqM)
			cpuLStr = fmtPctColored(cpuM, cpuLimM)
			memRStr = fmtPctColored(int64(rm.MEMLatest), memReqB)
			memLStr = fmtPctColored(int64(rm.MEMLatest), memLimB)
		}

		rows = append(rows, k8sres.ResourceRow{
			Name:      p.Name,
			Namespace: p.Namespace,
			Status:    status,
			Values: []string{
				p.Name, fmt.Sprintf("%d/%d", ready, total), status, fmt.Sprintf("%d", restarts), age,
				cpuStr, cpuRStr, cpuLStr, memStr, memRStr, memLStr,
			},
			Raw: p,
		})
	}
	return rows
}

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

func fmtPct(num, denom int64) string {
	if denom == 0 {
		return "~"
	}
	return fmt.Sprintf("%d", num*100/denom)
}

func fmtPctColored(num, denom int64) string {
	if denom == 0 {
		return "~"
	}
	pct := num * 100 / denom
	s := fmt.Sprintf("%d", pct)
	switch {
	case pct >= 90:
		return pctFailedStyle.Render(s)
	case pct >= 70:
		return pctPendingStyle.Render(s)
	default:
		return s
	}
}

// Columns: NAME, READY, UP-TO-DATE, AVAILABLE, AGE
func BuildDeploymentRows(deps []*appsv1.Deployment) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(deps))
	for _, d := range deps {
		ready := d.Status.ReadyReplicas
		desired := *d.Spec.Replicas
		age := k8sres.AgeString(d.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      d.Name,
			Namespace: d.Namespace,
			Status:    deployStatus(d),
			Values: []string{
				d.Name,
				fmt.Sprintf("%d/%d", ready, desired),
				fmt.Sprintf("%d", d.Status.UpdatedReplicas),
				fmt.Sprintf("%d", d.Status.AvailableReplicas),
				age,
			},
			Raw: d,
		})
	}
	return rows
}

// Columns: NAME, READY, AGE
func BuildStatefulSetRows(sets []*appsv1.StatefulSet) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(sets))
	for _, s := range sets {
		ready := fmt.Sprintf("%d/%d", s.Status.ReadyReplicas, *s.Spec.Replicas)
		age := k8sres.AgeString(s.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      s.Name,
			Namespace: s.Namespace,
			Values:    []string{s.Name, ready, age},
			Raw:       s,
		})
	}
	return rows
}

// Columns: NAME, DESIRED, READY, UP-TO-DATE, AGE
func BuildDaemonSetRows(sets []*appsv1.DaemonSet) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(sets))
	for _, d := range sets {
		age := k8sres.AgeString(d.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      d.Name,
			Namespace: d.Namespace,
			Values: []string{
				d.Name,
				fmt.Sprintf("%d", d.Status.DesiredNumberScheduled),
				fmt.Sprintf("%d", d.Status.NumberReady),
				fmt.Sprintf("%d", d.Status.UpdatedNumberScheduled),
				age,
			},
			Raw: d,
		})
	}
	return rows
}

// Columns: NAME, COMPLETIONS, DURATION, AGE
func BuildJobRows(jobs []*batchv1.Job) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(jobs))
	for _, j := range jobs {
		completions := "0"
		if j.Spec.Completions != nil {
			completions = fmt.Sprintf("%d/%d", j.Status.Succeeded, *j.Spec.Completions)
		}
		duration := ""
		if j.Status.CompletionTime != nil && !j.Status.StartTime.IsZero() {
			d := j.Status.CompletionTime.Sub(j.Status.StartTime.Time)
			duration = fmt.Sprintf("%.0fs", d.Seconds())
		}
		age := k8sres.AgeString(j.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      j.Name,
			Namespace: j.Namespace,
			Status:    jobStatus(j),
			Values:    []string{j.Name, completions, duration, age},
			Raw:       j,
		})
	}
	return rows
}

// Columns: NAME, SCHEDULE, LAST SCHEDULE, AGE
func BuildCronJobRows(cjs []*batchv1.CronJob) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(cjs))
	for _, c := range cjs {
		lastSchedule := "Never"
		if c.Status.LastScheduleTime != nil {
			lastSchedule = k8sres.AgeString(*c.Status.LastScheduleTime)
		}
		age := k8sres.AgeString(c.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      c.Name,
			Namespace: c.Namespace,
			Values:    []string{c.Name, c.Spec.Schedule, lastSchedule, age},
			Raw:       c,
		})
	}
	return rows
}

// Columns: NAME, TYPE, CLUSTER-IP, PORT(S), AGE
func BuildServiceRows(svcs []*corev1.Service) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(svcs))
	for _, s := range svcs {
		clusterIP := s.Spec.ClusterIP
		ports := ""
		for i, p := range s.Spec.Ports {
			if i > 0 {
				ports += ","
			}
			ports += fmt.Sprintf("%d/%s", p.Port, p.Protocol)
		}
		age := k8sres.AgeString(s.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      s.Name,
			Namespace: s.Namespace,
			Values:    []string{s.Name, string(s.Spec.Type), clusterIP, ports, age},
			Raw:       s,
		})
	}
	return rows
}

// Columns: NAME, STATUS, ROLES, VERSION, AGE
func BuildNodeRows(nodes []*corev1.Node) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(nodes))
	for _, n := range nodes {
		status := "NotReady"
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				status = "Ready"
			}
		}
		if n.Spec.Unschedulable {
			status = "SchedulingDisabled"
		}
		roles := nodeRoles(n)
		version := n.Status.NodeInfo.KubeletVersion
		age := k8sres.AgeString(n.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:   n.Name,
			Status: status,
			Values: []string{n.Name, status, roles, version, age},
			Raw:    n,
		})
	}
	return rows
}

// Columns: NAME, ADDRESSES, RULES, AGE
func BuildIngressRows(ings []*networkingv1.Ingress) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(ings))
	for _, ing := range ings {
		var addrs []string
		for _, lb := range ing.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				addrs = append(addrs, lb.IP)
			} else if lb.Hostname != "" {
				addrs = append(addrs, lb.Hostname)
			}
		}
		addrStr := strings.Join(addrs, ",")
		if addrStr == "" {
			addrStr = "<pending>"
		}

		var rulePairs []string
		for _, rule := range ing.Spec.Rules {
			host := rule.Host
			if host == "" {
				host = "*"
			}
			if rule.HTTP != nil {
				for _, path := range rule.HTTP.Paths {
					svc := path.Backend.Service
					if svc != nil {
						rulePairs = append(rulePairs,
							fmt.Sprintf("%s%s → %s:%d", host, path.Path, svc.Name, svc.Port.Number))
					}
				}
			}
		}
		rulesStr := "<none>"
		if len(rulePairs) == 1 {
			rulesStr = rulePairs[0]
		} else if len(rulePairs) > 1 {
			rulesStr = fmt.Sprintf("%s  +%d more", rulePairs[0], len(rulePairs)-1)
		}

		age := k8sres.AgeString(ing.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      ing.Name,
			Namespace: ing.Namespace,
			Values:    []string{ing.Name, addrStr, rulesStr, age},
			Raw:       ing,
		})
	}
	return rows
}

// Columns: NAME, DATA, AGE
func BuildConfigMapRows(cms []*corev1.ConfigMap) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(cms))
	for _, c := range cms {
		age := k8sres.AgeString(c.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      c.Name,
			Namespace: c.Namespace,
			Values:    []string{c.Name, fmt.Sprintf("%d", len(c.Data)), age},
			Raw:       c,
		})
	}
	return rows
}

// Columns: NAME, TYPE, DATA, AGE
func BuildSecretRows(secrets []*corev1.Secret) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(secrets))
	for _, s := range secrets {
		age := k8sres.AgeString(s.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      s.Name,
			Namespace: s.Namespace,
			Values:    []string{s.Name, string(s.Type), fmt.Sprintf("%d", len(s.Data)), age},
			Raw:       s,
		})
	}
	return rows
}

// Columns: NAME, STATUS, VOLUME, CAPACITY, AGE
func BuildPVCRows(pvcs []*corev1.PersistentVolumeClaim) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(pvcs))
	for _, p := range pvcs {
		cap := ""
		if storage, ok := p.Status.Capacity[corev1.ResourceStorage]; ok {
			cap = storage.String()
		}
		phase := string(p.Status.Phase)
		age := k8sres.AgeString(p.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      p.Name,
			Namespace: p.Namespace,
			Status:    phase,
			Values:    []string{p.Name, phase, p.Spec.VolumeName, cap, age},
			Raw:       p,
		})
	}
	return rows
}

// Columns: NAME, CAPACITY, ACCESS MODES, STATUS, AGE
func BuildPVRows(pvs []*corev1.PersistentVolume) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(pvs))
	for _, p := range pvs {
		cap := ""
		if storage, ok := p.Spec.Capacity[corev1.ResourceStorage]; ok {
			cap = storage.String()
		}
		modes := make([]string, 0, len(p.Spec.AccessModes))
		for _, m := range p.Spec.AccessModes {
			modes = append(modes, string(m))
		}
		phase := string(p.Status.Phase)
		age := k8sres.AgeString(p.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:   p.Name,
			Status: phase,
			Values: []string{p.Name, cap, strings.Join(modes, ","), phase, age},
			Raw:    p,
		})
	}
	return rows
}

// Columns: NAME, DESIRED, CURRENT, READY, AGE
func BuildReplicaSetRows(sets []*appsv1.ReplicaSet) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(sets))
	for _, s := range sets {
		desired := int32(0)
		if s.Spec.Replicas != nil {
			desired = *s.Spec.Replicas
		}
		age := k8sres.AgeString(s.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      s.Name,
			Namespace: s.Namespace,
			Values: []string{
				s.Name,
				fmt.Sprintf("%d", desired),
				fmt.Sprintf("%d", s.Status.Replicas),
				fmt.Sprintf("%d", s.Status.ReadyReplicas),
				age,
			},
			Raw: s,
		})
	}
	return rows
}

// Columns: LAST SEEN, COUNT, AGE, TYPE, REASON, OBJECT, MESSAGE
func BuildEventRows(evts []*corev1.Event) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(evts))
	for _, e := range evts {
		rows = append(rows, k8sres.ResourceRow{
			Name:       e.InvolvedObject.Name,
			Namespace:  e.Namespace,
			Status:     e.Type,
			SortByTime: e.LastTimestamp.Time,
			Values: []string{
				k8sres.AgeString(e.LastTimestamp),
				strconv.Itoa(int(e.Count)),
				k8sres.AgeString(e.FirstTimestamp),
				e.Type,
				e.Reason,
				e.InvolvedObject.Name,
				e.Message,
			},
			Raw: e,
		})
	}
	return rows
}

// Columns: NAME, CHART, VERSION, READY, SUSPENDED, AGE
func BuildHelmReleaseRows(releases []*unstructured.Unstructured) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(releases))
	for _, u := range releases {
		spec, _ := u.Object["spec"].(map[string]interface{})
		chartSpec, _ := func() (map[string]interface{}, bool) {
			if spec == nil {
				return nil, false
			}
			chart, _ := spec["chart"].(map[string]interface{})
			if chart == nil {
				return nil, false
			}
			cs, ok := chart["spec"].(map[string]interface{})
			return cs, ok
		}()

		chart := "-"
		version := "-"
		if chartSpec != nil {
			if v, ok := chartSpec["chart"].(string); ok && v != "" {
				chart = v
			}
			if v, ok := chartSpec["version"].(string); ok && v != "" {
				version = v
			}
		}

		suspended := false
		if spec != nil {
			if v, ok := spec["suspend"].(bool); ok {
				suspended = v
			}
		}

		ready := "False"
		statusMsg := "-"
		status, _ := u.Object["status"].(map[string]interface{})
		if status != nil {
			if conditions, ok := status["conditions"].([]interface{}); ok {
				for _, c := range conditions {
					cm, _ := c.(map[string]interface{})
					if cm["type"] == "Ready" {
						if s, ok := cm["status"].(string); ok {
							ready = s
						}
						if msg, ok := cm["message"].(string); ok && msg != "" {
							statusMsg = msg
						}
						break
					}
				}
			}
		}

		statusKey := "NotReady"
		if suspended {
			statusKey = "Suspended"
		} else if ready == "True" {
			statusKey = "Ready"
		}

		suspendedStr := "False"
		if suspended {
			suspendedStr = "True"
		}

		age := k8sres.AgeString(u.GetCreationTimestamp())
		rows = append(rows, k8sres.ResourceRow{
			Name:      u.GetName(),
			Namespace: u.GetNamespace(),
			Status:    statusKey,
			Values:    []string{u.GetName(), chart, version, ready, statusMsg, suspendedStr, age},
			Raw:       u,
		})
	}
	return rows
}

// helpers

func deployStatus(d *appsv1.Deployment) string {
	if d.Status.AvailableReplicas == *d.Spec.Replicas {
		return "Running"
	}
	return "Pending"
}

func jobStatus(j *batchv1.Job) string {
	if j.Status.Succeeded > 0 {
		return "Succeeded"
	}
	if j.Status.Failed > 0 {
		return "Failed"
	}
	return "Running"
}

func nodeRoles(n *corev1.Node) string {
	roles := []string{}
	for k := range n.Labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			roles = append(roles, strings.TrimPrefix(k, "node-role.kubernetes.io/"))
		}
	}
	if len(roles) == 0 {
		return "<none>"
	}
	return strings.Join(roles, ",")
}

