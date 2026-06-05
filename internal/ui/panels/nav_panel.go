package panels

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	k8sres "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

// navTitleBase, navCursorBase, navBodyBase are pre-built without Width so that
// per-render calls only incur one copy (`.Width(w)`) instead of a full style chain.
var (
	navTitleBase  = lipgloss.NewStyle().Foreground(styles.ColorPrimary).Bold(true).PaddingLeft(1)
	navCursorBase = lipgloss.NewStyle().Foreground(styles.ColorPrimary).Bold(true)
	navBodyBase   = lipgloss.NewStyle().Foreground(styles.ColorBodyText)
)

// NavPanel is the left-side resource navigator: kinds grouped under
// collapsible category headers (Workloads / Network / …). The cursor browses
// rows; landing on a kind row live-switches the table (as before), landing
// on a group header leaves the active kind untouched.
type NavPanel struct {
	width  int
	height int

	items     []navItem       // all kinds, grouped display order
	collapsed map[string]bool // group name → collapsed

	cursor     int    // index into rows()
	scroll     int    // first visible rows() index
	activeKind string // the kind the table currently shows
	focused    bool

	// Live data for the active kind only (the app runs informers for the
	// watched kind, never for the other 25 — see design doc).
	activeCount int  // row count; -1 = unknown
	activeFault bool // any row in a fault status

	hoverRow int // rows() index under the mouse; -1 = none

	filter      string
	filterOn    bool
	filterInput string
	filtered    []navItem
}

type navItem struct {
	kind    string
	display string
	group   string
}

// navRow is one visible line in the navigator: a group header or a kind.
type navRow struct {
	isGroup bool
	group   string
	item    navItem
}

// groupOrder fixes the category display order; "Other" catches kinds whose
// Meta has no Group (should not happen — the field is required for new kinds).
var groupOrder = []string{"Workloads", "Network", "Config", "Storage", "Access", "Cluster", "Helm", "Other"}

// navOrder is the canonical display order for kinds within their groups.
// Kinds not listed here are appended in Registry order.
var navOrder = []string{
	"Pod",
	"Deployment",
	"StatefulSet",
	"DaemonSet",
	"ReplicaSet",
	"Job",
	"CronJob",
	"Service",
	"Endpoints",
	"Ingress",
	"ConfigMap",
	"Secret",
	"ServiceAccount",
	"PersistentVolumeClaim",
	"HorizontalPodAutoscaler",
	"NetworkPolicy",
	"Role",
	"RoleBinding",
	"Node",
	"PersistentVolume",
	"Namespace",
	"ClusterRole",
	"ClusterRoleBinding",
	"StorageClass",
	"Event",
	"HelmRelease",
}

func NewNavPanel(w, h int) NavPanel {
	kindRank := make(map[string]int, len(navOrder))
	for i, k := range navOrder {
		kindRank[k] = i
	}
	groupRank := make(map[string]int, len(groupOrder))
	for i, g := range groupOrder {
		groupRank[g] = i
	}
	ordered := make([]k8sres.ResourceDescriptor, 0, len(k8sres.Registry))
	ordered = append(ordered, k8sres.Registry...)
	sort.SliceStable(ordered, func(i, j int) bool {
		gi, gj := navGroupOf(ordered[i]), navGroupOf(ordered[j])
		if gi != gj {
			return groupRank[gi] < groupRank[gj]
		}
		ri, oi := kindRank[ordered[i].Kind]
		rj, oj := kindRank[ordered[j].Kind]
		switch {
		case oi && oj:
			return ri < rj
		case oi:
			return true
		case oj:
			return false
		default:
			return false // preserve Registry order for unknowns
		}
	})
	items := make([]navItem, 0, len(ordered))
	for _, r := range ordered {
		items = append(items, navItem{kind: r.Kind, display: r.Kind, group: navGroupOf(r)})
	}
	p := NavPanel{
		width:       w,
		height:      h,
		items:       items,
		collapsed:   make(map[string]bool),
		activeCount: -1,
		hoverRow:    -1,
	}
	p.filtered = p.items
	if len(items) > 0 {
		p.activeKind = items[0].kind
		p.cursor = 1 // first kind row, under its group header
	}
	return p
}

func navGroupOf(r k8sres.ResourceDescriptor) string {
	if r.NavGroup == "" {
		return "Other"
	}
	return r.NavGroup
}

// setCollapsed sets a group's collapsed state copy-on-write, preserving the
// panel's value semantics (a returned NavPanel never aliases the receiver's
// mutable state).
func (n NavPanel) setCollapsed(group string, collapsed bool) NavPanel {
	next := make(map[string]bool, len(n.collapsed)+1)
	for k, v := range n.collapsed {
		next[k] = v
	}
	next[group] = collapsed
	n.collapsed = next
	return n
}

func (n NavPanel) SetSize(w, h int) NavPanel  { n.width = w; n.height = h; return n }
func (n NavPanel) SetFocused(f bool) NavPanel { n.focused = f; return n }
func (n NavPanel) FilterActive() bool         { return n.filterOn }

// SetActiveCounts records the live row count and fault flag for the active
// kind (informer data the table already holds).
func (n NavPanel) SetActiveCounts(count int, fault bool) NavPanel {
	n.activeCount = count
	n.activeFault = fault
	return n
}

// SetHoverRow marks the rows() index under the mouse (-1 clears). Render-only.
func (n NavPanel) SetHoverRow(idx int) NavPanel {
	n.hoverRow = idx
	return n
}

func (n NavPanel) ActiveKind() string { return n.activeKind }

// rows materializes the visible row list: a flat kind list while filtering,
// otherwise group headers with their kinds (collapsed groups hide theirs).
func (n NavPanel) rows() []navRow {
	if n.filterOn || n.filter != "" {
		out := make([]navRow, 0, len(n.filtered))
		for _, it := range n.filtered {
			out = append(out, navRow{item: it})
		}
		return out
	}
	out := make([]navRow, 0, len(n.items)+8)
	lastGroup := ""
	for _, it := range n.items {
		if it.group != lastGroup {
			out = append(out, navRow{isGroup: true, group: it.group})
			lastGroup = it.group
		}
		if !n.collapsed[it.group] {
			out = append(out, navRow{item: it, group: it.group})
		}
	}
	return out
}

// SetActiveKind activates kind: clears any filter, expands its group, and
// moves the cursor to its row.
func (n NavPanel) SetActiveKind(kind string) NavPanel {
	n.filter = ""
	n.filterInput = ""
	n.filterOn = false
	n.filtered = n.items
	for _, item := range n.items {
		if strings.EqualFold(item.kind, kind) {
			n.activeKind = item.kind
			n = n.setCollapsed(item.group, false)
			break
		}
	}
	for i, row := range n.rows() {
		if !row.isGroup && row.item.kind == n.activeKind {
			n.cursor = i
			break
		}
	}
	return n.ensureCursorVisible()
}

// CursorOnGroup reports whether the cursor rests on a group header (the app
// routes enter/right to a collapse toggle instead of a focus switch then).
func (n NavPanel) CursorOnGroup() bool {
	rows := n.rows()
	return n.cursor < len(rows) && rows[n.cursor].isGroup
}

// ToggleCursorGroup flips the collapsed state of the header under the cursor.
func (n NavPanel) ToggleCursorGroup() NavPanel {
	rows := n.rows()
	if n.cursor >= len(rows) || !rows[n.cursor].isGroup {
		return n
	}
	g := rows[n.cursor].group
	n = n.setCollapsed(g, !n.collapsed[g])
	return n.clampCursor()
}

func (n NavPanel) Update(msg tea.Msg) (NavPanel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if n.filterOn {
			clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(msg.Content)
			n.filterInput += clean
			n.applyNavFilter()
		}
		return n, nil
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			n = n.moveCursor(-1)
		case tea.MouseWheelDown:
			n = n.moveCursor(1)
		}
		return n, nil
	case tea.KeyPressMsg:
		if n.filterOn {
			switch msg.String() {
			case "enter":
				n.filterOn = false
				n.filter = n.filterInput
				n.applyNavFilter()
			case "esc":
				n.filterOn = false
				n.filterInput = ""
				n.filter = ""
				n.applyNavFilter()
			case "backspace":
				if len(n.filterInput) > 0 {
					n.filterInput = n.filterInput[:len(n.filterInput)-1]
					n.applyNavFilter()
				}
			case "ctrl+v":
				if text, err := clipboard.ReadAll(); err == nil && text != "" {
					clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
					n.filterInput += clean
					n.applyNavFilter()
				}
			default:
				if len(msg.Text) > 0 {
					n.filterInput += msg.Text
					n.applyNavFilter()
				}
			}
			return n, nil
		}
		switch msg.String() {
		case "up", "k":
			n = n.moveCursor(-1)
		case "down", "j":
			n = n.moveCursor(1)
		case "g":
			n.cursor = 0
			n = n.settleCursor()
		case "G":
			if rows := n.rows(); len(rows) > 0 {
				n.cursor = len(rows) - 1
				n = n.settleCursor()
			}
		case "h", "left":
			// Collapse: on a kind row, fold its whole group (cursor jumps to
			// the header); on an expanded header, fold it.
			rows := n.rows()
			if n.cursor < len(rows) {
				g := rows[n.cursor].group
				if rows[n.cursor].isGroup && n.collapsed[g] {
					break // already folded — nothing to peel
				}
				if g != "" {
					n = n.setCollapsed(g, true)
					for i, row := range n.rows() {
						if row.isGroup && row.group == g {
							n.cursor = i
							break
						}
					}
					n = n.ensureCursorVisible()
				}
			}
		case "l", "right":
			if rows := n.rows(); n.cursor < len(rows) && rows[n.cursor].isGroup {
				n = n.setCollapsed(rows[n.cursor].group, false)
			}
		case " ":
			n = n.ToggleCursorGroup()
		case "/":
			n.filterOn = true
			n.filterInput = n.filter
		case "esc":
			n.filter = ""
			n.filterInput = ""
			n.applyNavFilter()
		}
	}
	return n, nil
}

// moveCursor advances the cursor with wrap-around and live-switches the
// active kind when it lands on a kind row.
func (n NavPanel) moveCursor(delta int) NavPanel {
	rows := n.rows()
	if len(rows) == 0 {
		return n
	}
	n.cursor = (n.cursor + delta + len(rows)) % len(rows)
	return n.settleCursor()
}

// settleCursor applies on-land effects: kind rows become the active kind.
func (n NavPanel) settleCursor() NavPanel {
	rows := n.rows()
	if n.cursor < len(rows) && !rows[n.cursor].isGroup {
		n.activeKind = rows[n.cursor].item.kind
	}
	return n.ensureCursorVisible()
}

func (n NavPanel) clampCursor() NavPanel {
	if rows := n.rows(); n.cursor >= len(rows) {
		n.cursor = max(0, len(rows)-1)
	}
	return n.ensureCursorVisible()
}

// visibleBodyRows is how many navigator rows fit under the title (and the
// filter bar when present) inside the border.
func (n NavPanel) visibleBodyRows() int {
	innerH := max(1, n.height-2)
	innerH-- // title
	if n.filterOn || n.filter != "" {
		innerH--
	}
	return max(1, innerH)
}

func (n NavPanel) ensureCursorVisible() NavPanel {
	vis := n.visibleBodyRows()
	if n.cursor < n.scroll {
		n.scroll = n.cursor
	}
	if n.cursor >= n.scroll+vis {
		n.scroll = n.cursor - vis + 1
	}
	if total := len(n.rows()); n.scroll > max(0, total-vis) {
		n.scroll = max(0, total-vis)
	}
	return n
}

// rowIndexAt maps a panel-inner Y to a rows() index, or -1.
func (n NavPanel) rowIndexAt(innerY int) int {
	firstRowY := 1 // title at line 0
	if n.filterOn || n.filter != "" {
		firstRowY = 2 // + filter bar
	}
	idx := n.scroll + innerY - firstRowY
	if innerY < firstRowY || idx < 0 || idx >= len(n.rows()) {
		return -1
	}
	return idx
}

// RowIndexAt is the exported row hit-test (hover); -1 when no row is there.
func (n NavPanel) RowIndexAt(innerY int) int { return n.rowIndexAt(innerY) }

// HandleClickAt handles a click at panel-inner Y. Returns the clicked kind
// ("" when none) and whether the click toggled a group header — toggles are
// self-contained and must not move focus or leave the current mode.
func (n NavPanel) HandleClickAt(innerY int) (NavPanel, string, bool) {
	idx := n.rowIndexAt(innerY)
	if idx < 0 {
		return n, "", false
	}
	rows := n.rows()
	n.cursor = idx
	if rows[idx].isGroup {
		g := rows[idx].group
		n = n.setCollapsed(g, !n.collapsed[g])
		return n.clampCursor(), "", true
	}
	n.activeKind = rows[idx].item.kind
	return n, rows[idx].item.kind, false
}

func (n *NavPanel) applyNavFilter() {
	if n.filterInput == "" {
		n.filtered = n.items
	} else {
		low := strings.ToLower(n.filterInput)
		filtered := make([]navItem, 0, len(n.items))
		for _, item := range n.items {
			if strings.Contains(strings.ToLower(item.kind), low) {
				filtered = append(filtered, item)
			}
		}
		n.filtered = filtered
	}
	rows := n.rows()
	if n.cursor >= len(rows) {
		n.cursor = max(0, len(rows)-1)
	}
	*n = n.settleCursor()
}

func (n NavPanel) View() string {
	border := styles.NormalBorder
	if n.focused {
		border = styles.FocusedBorder
	}

	innerW := max(1, n.width-2)

	countInfo := ""
	if n.filter != "" {
		countInfo = styles.Muted.Render(fmt.Sprintf(" %d/%d", len(n.filtered), len(n.items)))
	}
	title := navTitleBase.Width(innerW).Render("Resources") + countInfo

	var out []string
	out = append(out, title)

	filterBar := ""
	if n.filterOn {
		filterBar = styles.Primary.Render("filter: ") + n.filterInput + styles.Muted.Render("█")
	} else if n.filter != "" {
		filterBar = styles.Primary.Render("filter: ") + styles.Warning.Render(n.filter) + styles.Muted.Render("  (/ to change, esc to clear)")
	}
	if filterBar != "" {
		out = append(out, filterBar)
	}

	rows := n.rows()
	vis := n.visibleBodyRows()
	end := min(len(rows), n.scroll+vis)
	for i := n.scroll; i < end; i++ {
		out = append(out, n.renderRow(rows[i], i, innerW))
	}

	content := strings.Join(out, "\n")
	return border.Width(max(1, n.width)).Height(max(1, n.height)).Render(content)
}

// renderRow renders one navigator line at full inner width.
func (n NavPanel) renderRow(row navRow, idx, innerW int) string {
	if row.isGroup {
		arrow := "▾ "
		if n.collapsed[row.group] {
			arrow = "▸ "
		}
		label := arrow + row.group
		st := styles.Muted.Bold(true)
		if idx == n.cursor && n.focused {
			st = navCursorBase
		} else if idx == n.hoverRow {
			st = st.Background(styles.ColorHover)
		}
		return st.Width(innerW).Render(" " + label)
	}

	// Kind row: " ▶ Name" on the cursor row, " ● Name" on the active kind,
	// "   Name" otherwise. The active kind shows its live row count (and a
	// fault dot) right-aligned.
	prefix := "   "
	switch {
	case idx == n.cursor:
		prefix = " ▶ "
	case row.item.kind == n.activeKind:
		prefix = " ● "
	}

	suffix := ""
	if row.item.kind == n.activeKind && n.activeCount >= 0 {
		suffix = fmt.Sprintf("%d ", n.activeCount)
		if n.activeFault {
			suffix = "● " + suffix
		}
	}

	maxLabel := max(1, innerW-len(prefix)-lipgloss.Width(suffix))
	label := row.item.display
	if len(label) > maxLabel {
		label = label[:max(1, maxLabel-1)] + "…"
	}

	pad := max(0, innerW-len(prefix)-len(label)-lipgloss.Width(suffix))
	plain := prefix + label + strings.Repeat(" ", pad)

	var st lipgloss.Style
	switch {
	case idx == n.cursor:
		st = navCursorBase
	case idx == n.hoverRow:
		st = navBodyBase.Background(styles.ColorHover)
	default:
		st = navBodyBase
	}

	if suffix == "" {
		return st.Width(innerW).Render(plain)
	}
	suffixStyled := styles.Muted.Render(suffix)
	if n.activeFault {
		suffixStyled = styles.Error.Render("● ") + styles.Muted.Render(strings.TrimPrefix(suffix, "● "))
	}
	return st.Render(plain) + suffixStyled
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
