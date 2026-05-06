package panels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/atotto/clipboard"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	k8slogs "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

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

// logGroupState owns the per-group filter, search, scroll, autoScroll, and
// pod-solo state. There is always at least one group — even when SetPods is
// used (no tabs), a synthetic group with empty name represents "all pods".
type logGroupState struct {
	group    string // "" when there are no tab groups
	viewport viewport.Model

	autoScroll bool

	filterOn    bool
	filterInput string
	filter      string

	searchOn      bool
	searchInput   string
	searchQuery   string
	searchMatches []int
	searchCurrent int

	// podFilter solos a single pod within this group's pod set.
	// -1 = all pods. Only meaningful in single-group mode where
	// "this group's pod set" == LogViewer.pods.
	podFilter int

	lineCountStr string
}

func newGroupState(name string) logGroupState {
	vp := viewport.New(viewport.WithWidth(1), viewport.WithHeight(1))
	vp.SetHorizontalStep(8)
	return logGroupState{
		group:         name,
		viewport:      vp,
		autoScroll:    true,
		podFilter:     -1,
		searchCurrent: -1,
		lineCountStr:  "  0 lines",
	}
}

// LogViewer displays merged streaming logs from multiple pods, organized into
// one or more groups (tabs). Each group owns its own filter, search, scroll
// position, autoScroll flag, and pod-solo state — preserved across tab
// switches and across split-view layout cycles.
type LogViewer struct {
	width, height int
	focused       bool
	pods          []string

	// Master log buffer — shared across all groups, filtered per-group at render time.
	lines      []k8slogs.LogLine
	colorCache []string // parallel to lines; Chroma-colorized JSON or "" for plain text
	lowerCache []string // parallel to lines; pre-cached strings.ToLower(line.Text)
	jsonIndent bool     // pretty-print JSON with indentation; toggle with J
	lastLineAt time.Time

	tabGroups []string // empty when in single-group (no tabs) mode

	groups       []logGroupState // always len >= 1
	focusedGroup int             // index into groups
	layout       LogLayoutMode

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
		jsonIndent: true,
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
	v.lowerCache = nil
	v.jsonIndent = true
	v.lastLineAt = time.Time{}
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
	v.lowerCache = nil
	v.jsonIndent = true
	v.lastLineAt = time.Time{}
	v.groups = gs
	v.focusedGroup = 0
	v.layout = LayoutTabs
	v.applySizes()
	return v
}

// HasActiveState reports whether any peelable state is active on the focused
// group, OR the viewer is in a non-default layout. The model's esc handler
// uses this to decide whether to peel state vs. exit log mode entirely.
func (v LogViewer) HasActiveState() bool {
	if v.layout != LayoutTabs {
		return true
	}
	if len(v.groups) == 0 {
		return false
	}
	g := v.groups[v.focusedGroup]
	return g.podFilter >= 0 || g.filter != "" || g.filterOn || g.searchQuery != "" || g.searchOn
}

// IsCapturingInput reports whether the focused group's filter or search input is open.
func (v LogViewer) IsCapturingInput() bool {
	if len(v.groups) == 0 {
		return false
	}
	g := v.groups[v.focusedGroup]
	return g.filterOn || g.searchOn
}

// HandleEsc peels one layer of state on the focused group, then returns to
// LayoutTabs from any split layout, then yields control to the caller (model).
func (v LogViewer) HandleEsc() LogViewer {
	if len(v.groups) == 0 {
		return v
	}
	g := &v.groups[v.focusedGroup]
	if g.searchOn {
		g.searchOn = false
		return v
	}
	if g.filterOn {
		g.filterOn = false
		return v
	}
	if g.podFilter >= 0 {
		g.podFilter = -1
		v.rebuildAll()
		return v
	}
	if g.searchQuery != "" {
		g.searchQuery = ""
		g.searchInput = ""
		g.searchMatches = nil
		g.searchCurrent = -1
		v.rebuildAll()
		return v
	}
	if g.filter != "" {
		g.filter = ""
		g.filterInput = ""
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

func (v LogViewer) Update(msg tea.Msg) (LogViewer, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		// Bracketed paste — used by terminals that emit \e[200~ … \e[201~ when the
		// user hits Cmd+V / Ctrl+Shift+V. Route into whichever input is open on the focused group.
		clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(msg.Content)
		g := &v.groups[v.focusedGroup]
		if g.filterOn {
			g.filterInput += clean
			v.rebuildGroup(v.focusedGroup)
		} else if g.searchOn {
			g.searchInput += clean
			v.rebuildGroup(v.focusedGroup)
		}
		return v, nil

	case k8slogs.LogLineMsg:
		for _, line := range msg.Lines {
			v.lines = append(v.lines, line)
			v.colorCache = append(v.colorCache, tryColorizeJSON(line.Text, v.jsonIndent))
			v.lowerCache = append(v.lowerCache, strings.ToLower(line.Text))
		}
		if len(msg.Lines) > 0 {
			v.lastLineAt = time.Now()
		}
		if len(v.lines) > maxLogLines {
			trim := len(v.lines) - maxLogLines
			v.lines = v.lines[trim:]
			v.colorCache = v.colorCache[trim:]
			v.lowerCache = v.lowerCache[trim:]
		}
		v.rebuildAll()
		for i := range v.groups {
			if v.groups[i].autoScroll {
				v.groups[i].viewport.GotoBottom()
			}
		}
		return v, nil

	case tea.KeyPressMsg:
		g := &v.groups[v.focusedGroup]

		// Filter input mode (/)
		if g.filterOn {
			switch msg.String() {
			case "enter", "esc":
				g.filterOn = false
				g.filter = g.filterInput
				v.rebuildGroup(v.focusedGroup)
			case "ctrl+c":
				g.filterOn = false
				g.filterInput = ""
				g.filter = ""
				v.rebuildGroup(v.focusedGroup)
			case "backspace":
				if len(g.filterInput) > 0 {
					g.filterInput = g.filterInput[:len(g.filterInput)-1]
					v.rebuildGroup(v.focusedGroup)
				}
			case "ctrl+v":
				if text, err := clipboard.ReadAll(); err == nil && text != "" {
					clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
					g.filterInput += clean
					v.rebuildGroup(v.focusedGroup)
				}
			default:
				if len(msg.Text) > 0 {
					g.filterInput += msg.Text
					v.rebuildGroup(v.focusedGroup)
				}
			}
			return v, nil
		}

		// Search input mode (ctrl+f)
		if g.searchOn {
			switch msg.String() {
			case "enter":
				g.searchQuery = g.searchInput
				g.searchOn = false
				v.rebuildGroup(v.focusedGroup)
				if len(g.searchMatches) > 0 {
					g.searchCurrent = 0
					g.viewport.SetYOffset(g.searchMatches[0])
					g.autoScroll = false
				}
			case "esc":
				g.searchOn = false
			case "ctrl+c":
				g.searchOn = false
				g.searchInput = ""
				g.searchQuery = ""
				g.searchMatches = nil
				g.searchCurrent = -1
				v.rebuildGroup(v.focusedGroup)
			case "backspace":
				if len(g.searchInput) > 0 {
					g.searchInput = g.searchInput[:len(g.searchInput)-1]
					v.rebuildGroup(v.focusedGroup)
				}
			case "ctrl+v":
				if text, err := clipboard.ReadAll(); err == nil && text != "" {
					clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
					g.searchInput += clean
					v.rebuildGroup(v.focusedGroup)
				}
			default:
				if len(msg.Text) > 0 {
					g.searchInput += msg.Text
					v.rebuildGroup(v.focusedGroup)
				}
			}
			return v, nil
		}

		switch msg.String() {
		case "/":
			g.searchOn = false
			g.filterOn = true
			g.filterInput = g.filter
		case "ctrl+f":
			g.filterOn = false
			g.searchOn = true
			g.searchInput = g.searchQuery
			g.autoScroll = false
		case "n":
			if len(g.searchMatches) > 0 {
				g.searchCurrent = (g.searchCurrent + 1) % len(g.searchMatches)
				g.viewport.SetYOffset(g.searchMatches[g.searchCurrent])
				g.autoScroll = false
			}
		case "N":
			if len(g.searchMatches) > 0 {
				g.searchCurrent = (g.searchCurrent - 1 + len(g.searchMatches)) % len(g.searchMatches)
				g.viewport.SetYOffset(g.searchMatches[g.searchCurrent])
				g.autoScroll = false
			}
		case "G":
			g.viewport.GotoBottom()
			g.autoScroll = true
		case "g":
			g.viewport.GotoTop()
			g.viewport.SetXOffset(0)
			g.autoScroll = false
		case "tab":
			if len(v.tabGroups) > 1 {
				v.focusedGroup = (v.focusedGroup + 1) % len(v.groups)
				if v.layout == LayoutTabs {
					ng := &v.groups[v.focusedGroup]
					ng.viewport.SetXOffset(0)
					if ng.autoScroll {
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
		case "0":
			g.viewport.SetXOffset(0)
			if len(v.tabGroups) > 1 {
				v.focusedGroup = 0
			} else {
				g.podFilter = -1
				v.rebuildGroup(v.focusedGroup)
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			n := int(msg.String()[0]-'0') - 1
			if len(v.tabGroups) > 1 {
				if n < len(v.groups) {
					v.focusedGroup = n
					if v.layout == LayoutTabs {
						ng := &v.groups[v.focusedGroup]
						if ng.autoScroll {
							ng.viewport.GotoBottom()
						}
					}
				}
			} else if n < len(v.pods) {
				g.podFilter = n
				v.rebuildGroup(v.focusedGroup)
			}
		case "ctrl+c":
			// Copy focused group's currently-visible (filtered) lines.
			fg := v.groups[v.focusedGroup]
			lowFilter := strings.ToLower(fg.filter)
			var parts []string
			for _, l := range v.lines {
				if !v.lineBelongsToGroup(l, fg) {
					continue
				}
				if lowFilter != "" && !strings.Contains(strings.ToLower(l.Text), lowFilter) {
					continue
				}
				parts = append(parts, l.Text)
			}
			_ = clipboard.WriteAll(strings.Join(parts, "\n"))
		default:
			var cmd tea.Cmd
			g.viewport, cmd = g.viewport.Update(msg)
			switch msg.String() {
			case "up", "k", "pgup", "ctrl+u":
				g.autoScroll = false
			default:
				if g.viewport.AtBottom() {
					g.autoScroll = true
				}
			}
			return v, cmd
		}
		return v, nil

	case tea.MouseWheelMsg:
		// Wheel scrolls the focused group's viewport.
		g := &v.groups[v.focusedGroup]
		var cmd tea.Cmd
		g.viewport, cmd = g.viewport.Update(msg)
		switch msg.Button {
		case tea.MouseWheelUp:
			g.autoScroll = false
		case tea.MouseWheelDown:
			if g.viewport.AtBottom() {
				g.autoScroll = true
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
// applying its filter and search and updating its searchMatches & lineCountStr.
func (v *LogViewer) rebuildGroup(idx int) {
	if idx < 0 || idx >= len(v.groups) {
		return
	}
	g := &v.groups[idx]
	g.searchMatches = nil

	activeFilter := g.filter
	if g.filterOn {
		activeFilter = g.filterInput
	}
	lowFilter := strings.ToLower(activeFilter)

	activeSearch := g.searchQuery
	if g.searchOn {
		activeSearch = g.searchInput
	}
	lowSearch := strings.ToLower(activeSearch)

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

		// Search highlight takes priority over JSON colorization (both emit ANSI codes).
		text := l.Text
		if lowSearch != "" && strings.Contains(lowText, lowSearch) {
			g.searchMatches = append(g.searchMatches, viewLine)
			text = highlightMatches(l.Text, lowSearch)
		} else if !l.IsSystem && i < len(v.colorCache) && v.colorCache[i] != "" {
			text = v.colorCache[i]
		}

		rendered := renderLogLineText(l, text)
		sb.WriteString(rendered)
		sb.WriteByte('\n')
		viewLine += strings.Count(rendered, "\n") + 1
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
	prefix := styles.LogPrefixStyles[colorIdx].Render(fmt.Sprintf("[%s] ", l.Pod))
	if l.IsSystem {
		return styles.Muted.Italic(true).Render(text)
	}
	return prefix + text
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
// bar + the focused group's filter/search/help/viewport.
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

	scrollStatus := liveOrPaused(g, v.lastLineAt)
	indentHint := ""
	if !v.jsonIndent {
		indentHint = "  " + styles.Muted.Render("[json flat]")
	}

	tabBar := ""
	if len(v.tabGroups) > 1 {
		tabBar = "\n" + v.renderTabBar()
	}

	filterBar := renderFilterBar(g)
	searchBar := renderSearchBar(g)

	var help string
	if len(v.tabGroups) > 1 {
		items := []HelpItem{
			{Key: "↑↓/jk", Desc: "scroll"},
			{Key: "tab/1-9", Desc: "switch"},
		}
		if v.canSplit() {
			items = append(items, HelpItem{Key: "v", Desc: "split"})
		}
		items = append(items,
			HelpItem{Key: "/", Desc: "filter"},
			HelpItem{Key: "ctrl+f", Desc: "search"},
			HelpItem{Key: "n/N", Desc: "next/prev"},
			HelpItem{Key: "J", Desc: "indent"},
			HelpItem{Key: "g/G", Desc: "top/bottom"},
			HelpItem{Key: "F", Desc: "fullscreen"},
			HelpItem{Key: "esc", Desc: "back"},
		)
		help = "  " + RenderHelpInline(items)
	} else {
		help = "  " + RenderHelpInline([]HelpItem{
			{Key: "↑↓/jk", Desc: "scroll"},
			{Key: "/", Desc: "filter"},
			{Key: "ctrl+f", Desc: "search"},
			{Key: "n/N", Desc: "next/prev"},
			{Key: "1-9", Desc: "solo pod"},
			{Key: "0", Desc: "all"},
			{Key: "J", Desc: "indent"},
			{Key: "g/G", Desc: "top/bottom"},
			{Key: "F", Desc: "fullscreen"},
			{Key: "esc", Desc: "back"},
		})
	}
	header := title + scrollStatus + indentHint + "  " + styles.Muted.Render(g.lineCountStr) + tabBar + filterBar + searchBar + "\n" + help

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
// laid out either as horizontal stripes or vertical columns.
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
		styles.Primary.Render(fmt.Sprintf("%d groups", len(v.tabGroups))) +
		"   " + RenderHelpInline([]HelpItem{
		{Key: "v", Desc: "cycle layout"},
		{Key: "tab", Desc: "focus next"},
		{Key: "F", Desc: "fullscreen"},
		{Key: "esc", Desc: "exit split"},
	})

	// Compute stripe size & build stripe boxes.
	innerW := max(1, v.width-2)
	innerH := max(1, v.height-2-2) // border + outer header (title + blank)

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
			title + "\n\n" + body,
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
			title + "\n\n" + body,
		)
	}
	return ""
}

// renderStripeBox builds the bordered box for group i sized w x h. The
// viewport inside is sized to fit; rebuild content for current dims.
func (v LogViewer) renderStripeBox(i, w, h int) string {
	g := &v.groups[i] // safe: receiver is a value but groups slice array is shared

	stripeBorder := styles.NormalBorder
	if i == v.focusedGroup {
		stripeBorder = styles.FocusedBorder
	}

	// Set viewport dimensions for this box.
	chromeLines := 1 // title
	if g.filterOn || g.filter != "" {
		chromeLines++
	}
	if g.searchOn || g.searchQuery != "" {
		chromeLines++
	}
	chromeLines++ // help line
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
	stripeTitle := focusMark + styles.Primary.Render(name) + liveOrPaused(*g, v.lastLineAt) +
		"  " + styles.Muted.Render(g.lineCountStr)

	filterBar := renderFilterBar(*g)
	searchBar := renderSearchBar(*g)

	help := "  " + RenderHelpInline([]HelpItem{
		{Key: "↑↓/jk", Desc: "scroll"},
		{Key: "/", Desc: "filter"},
		{Key: "ctrl+f", Desc: "search"},
		{Key: "esc", Desc: "tabs"},
	})

	header := stripeTitle + filterBar + searchBar + "\n" + help

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

// liveOrPaused renders the green "live" or muted "⏸ N%" indicator for a group.
func liveOrPaused(g logGroupState, lastLineAt time.Time) string {
	if g.autoScroll {
		s := styles.Success.Render("  ● live")
		if !lastLineAt.IsZero() {
			if since := time.Since(lastLineAt); since >= 30*time.Second {
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

func renderFilterBar(g logGroupState) string {
	if g.filterOn {
		return "\n" + styles.Primary.Render("filter: ") + g.filterInput + styles.Muted.Render("█")
	}
	if g.filter != "" {
		return "\n" + styles.Primary.Render("filter: ") + styles.Warning.Render(g.filter) +
			styles.Muted.Render("  (/ change, esc clear)")
	}
	return ""
}

func renderSearchBar(g logGroupState) string {
	if g.searchOn {
		extra := ""
		if g.searchInput != "" && len(g.searchMatches) > 0 {
			extra = "  " + styles.Muted.Render(fmt.Sprintf("%d matches", len(g.searchMatches)))
		}
		return "\n" + styles.Primary.Render("search: ") + g.searchInput + styles.Muted.Render("█") + extra
	}
	if g.searchQuery != "" {
		matchInfo := "no matches"
		if len(g.searchMatches) > 0 {
			matchInfo = fmt.Sprintf("%d/%d", g.searchCurrent+1, len(g.searchMatches))
		}
		return "\n" + styles.Primary.Render("search: ") + styles.Warning.Render(g.searchQuery) +
			"  " + styles.Muted.Render(matchInfo+"  n↓ N↑  esc clear")
	}
	return ""
}

// HandleClickAt routes a left-click in panel-local coordinates to a tab or
// stripe and returns true on hit.
func (v LogViewer) HandleClickAt(x, y int) (LogViewer, bool) {
	for _, z := range v.tabHitZones() {
		if x >= z.x1 && x <= z.x2 && y >= z.y1 && y <= z.y2 {
			v.focusedGroup = z.idx
			if v.layout == LayoutTabs && v.groups[z.idx].autoScroll {
				v.groups[z.idx].viewport.GotoBottom()
			}
			return v, true
		}
	}
	for _, z := range v.stripeHitZones() {
		if x >= z.x1 && x <= z.x2 && y >= z.y1 && y <= z.y2 {
			v.focusedGroup = z.idx
			return v, true
		}
	}
	return v, false
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
