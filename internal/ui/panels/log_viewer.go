package panels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	k8slogs "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

const maxLogLines = 10000

// LogViewer displays merged streaming logs from multiple pods.
type LogViewer struct {
	viewport   viewport.Model
	width      int
	height     int
	focused    bool
	pods       []string
	lines      []k8slogs.LogLine
	colorCache []string // parallel to lines; Chroma-colorized JSON or "" for plain text
	autoScroll bool
	jsonIndent bool // pretty-print JSON with indentation; toggle with J
	podFilter  int  // -1 = all pods; 0..N-1 = solo pods[podFilter] (single-group mode only)

	// Tab mode: one tab per LogGroup (e.g. one deployment per tab).
	// Empty when streaming a single group — falls back to podFilter behaviour.
	tabGroups     []string // group names in order
	activeTabIdx  int      // currently visible tab

	// Filter: hides non-matching lines (/ key)
	filterInput string
	filterOn    bool
	filter      string

	// Search: highlights matching text inline, navigate with n/N (ctrl+f)
	searchInput   string
	searchOn      bool
	searchQuery   string
	searchMatches []int // viewport visual line indices of matching lines
	searchCurrent int   // -1 when no match selected
}

func NewLogViewer(w, h int) LogViewer {
	vp := viewport.New(w-2, h-7)
	return LogViewer{
		viewport:      vp,
		width:         w,
		height:        h,
		autoScroll:    true,
		jsonIndent:    true,
		podFilter:     -1,
		searchCurrent: -1,
	}
}

func (v LogViewer) SetSize(w, h int) LogViewer {
	v.width = w
	v.height = h
	v.viewport.Width = w - 2
	v.viewport.Height = h - 7
	return v
}

func (v LogViewer) SetFocused(f bool) LogViewer { v.focused = f; return v }

func (v LogViewer) SetPods(pods []string) LogViewer {
	v.pods = pods
	v.tabGroups = nil
	v.activeTabIdx = 0
	v.lines = nil
	v.colorCache = nil
	v.podFilter = -1
	v.jsonIndent = true
	v.filterInput = ""
	v.filterOn = false
	v.filter = ""
	v.searchInput = ""
	v.searchOn = false
	v.searchQuery = ""
	v.searchMatches = nil
	v.searchCurrent = -1
	v.autoScroll = true
	return v
}

// SetPodGroups configures tab mode: one tab per group, pods within each group merged.
// A single group falls back to SetPods (no tab bar).
func (v LogViewer) SetPodGroups(groups []k8slogs.LogGroup) LogViewer {
	if len(groups) <= 1 {
		if len(groups) == 1 {
			return v.SetPods(groups[0].Pods)
		}
		return v.SetPods(nil)
	}
	var allPods []string
	tabs := make([]string, len(groups))
	for i, g := range groups {
		tabs[i] = g.Name
		allPods = append(allPods, g.Pods...)
	}
	v.pods = allPods
	v.tabGroups = tabs
	v.activeTabIdx = 0
	v.lines = nil
	v.colorCache = nil
	v.podFilter = -1
	v.jsonIndent = true
	v.filterInput = ""
	v.filterOn = false
	v.filter = ""
	v.searchInput = ""
	v.searchOn = false
	v.searchQuery = ""
	v.searchMatches = nil
	v.searchCurrent = -1
	v.autoScroll = true
	return v
}

// HasActiveState reports whether any filter or search state is active.
// Used by the model's esc handler to decide whether to clear state vs exit log mode.
func (v LogViewer) HasActiveState() bool {
	return v.podFilter >= 0 || v.filter != "" || v.filterOn || v.searchQuery != "" || v.searchOn
}

// HandleEsc peels one layer: cancel active input → clear pod filter → clear search → clear filter.
func (v LogViewer) HandleEsc() LogViewer {
	if v.searchOn {
		v.searchOn = false
		return v
	}
	if v.filterOn {
		v.filterOn = false
		return v
	}
	if v.podFilter >= 0 {
		v.podFilter = -1
		v.rebuildViewport()
		return v
	}
	if v.searchQuery != "" {
		v.searchQuery = ""
		v.searchInput = ""
		v.searchMatches = nil
		v.searchCurrent = -1
		v.rebuildViewport()
		return v
	}
	if v.filter != "" {
		v.filter = ""
		v.filterInput = ""
		v.rebuildViewport()
		return v
	}
	return v
}

func (v LogViewer) Update(msg tea.Msg) (LogViewer, tea.Cmd) {
	switch msg := msg.(type) {
	case k8slogs.LogLineMsg:
		for _, line := range msg.Lines {
			v.lines = append(v.lines, line)
			v.colorCache = append(v.colorCache, tryColorizeJSON(line.Text, v.jsonIndent))
		}
		if len(v.lines) > maxLogLines {
			trim := len(v.lines) - maxLogLines
			v.lines = v.lines[trim:]
			v.colorCache = v.colorCache[trim:]
		}
		v.rebuildViewport()
		if v.autoScroll {
			v.viewport.GotoBottom()
		}
		return v, nil

	case tea.KeyMsg:
		// Filter input mode (/)
		if v.filterOn {
			switch msg.String() {
			case "enter", "esc":
				v.filterOn = false
				v.filter = v.filterInput
				v.rebuildViewport()
			case "ctrl+c":
				v.filterOn = false
				v.filterInput = ""
				v.filter = ""
				v.rebuildViewport()
			case "backspace":
				if len(v.filterInput) > 0 {
					v.filterInput = v.filterInput[:len(v.filterInput)-1]
					v.rebuildViewport()
				}
			default:
				if len(msg.String()) == 1 {
					v.filterInput += msg.String()
					v.rebuildViewport()
				}
			}
			return v, nil
		}

		// Search input mode (ctrl+f)
		if v.searchOn {
			switch msg.String() {
			case "enter":
				v.searchQuery = v.searchInput
				v.searchOn = false
				v.rebuildViewport()
				if len(v.searchMatches) > 0 {
					v.searchCurrent = 0
					v.viewport.SetYOffset(v.searchMatches[0])
					v.autoScroll = false
				}
			case "esc":
				v.searchOn = false
			case "ctrl+c":
				v.searchOn = false
				v.searchInput = ""
				v.searchQuery = ""
				v.searchMatches = nil
				v.searchCurrent = -1
				v.rebuildViewport()
			case "backspace":
				if len(v.searchInput) > 0 {
					v.searchInput = v.searchInput[:len(v.searchInput)-1]
					v.rebuildViewport()
				}
			default:
				if len(msg.String()) == 1 {
					v.searchInput += msg.String()
					v.rebuildViewport()
				}
			}
			return v, nil
		}

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
			if len(v.searchMatches) > 0 {
				v.searchCurrent = (v.searchCurrent + 1) % len(v.searchMatches)
				v.viewport.SetYOffset(v.searchMatches[v.searchCurrent])
				v.autoScroll = false
			}
		case "N":
			if len(v.searchMatches) > 0 {
				v.searchCurrent = (v.searchCurrent - 1 + len(v.searchMatches)) % len(v.searchMatches)
				v.viewport.SetYOffset(v.searchMatches[v.searchCurrent])
				v.autoScroll = false
			}
		case "G":
			v.viewport.GotoBottom()
			v.autoScroll = true
		case "g":
			v.viewport.GotoTop()
			v.autoScroll = false
		case "tab":
			if len(v.tabGroups) > 1 {
				v.activeTabIdx = (v.activeTabIdx + 1) % len(v.tabGroups)
				v.rebuildViewport()
				if v.autoScroll {
					v.viewport.GotoBottom()
				}
			}
		case "J":
			v.jsonIndent = !v.jsonIndent
			for i, l := range v.lines {
				v.colorCache[i] = tryColorizeJSON(l.Text, v.jsonIndent)
			}
			v.rebuildViewport()
		case "0":
			if len(v.tabGroups) > 1 {
				v.activeTabIdx = 0
				v.rebuildViewport()
			} else {
				v.podFilter = -1
				v.rebuildViewport()
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			n := int(msg.String()[0]-'0') - 1
			if len(v.tabGroups) > 1 {
				if n < len(v.tabGroups) {
					v.activeTabIdx = n
					v.rebuildViewport()
					if v.autoScroll {
						v.viewport.GotoBottom()
					}
				}
			} else if n < len(v.pods) {
				v.podFilter = n
				v.rebuildViewport()
			}
		default:
			var cmd tea.Cmd
			v.viewport, cmd = v.viewport.Update(msg)
			switch msg.String() {
			case "up", "k", "pgup", "ctrl+u":
				v.autoScroll = false
			default:
				if v.viewport.AtBottom() {
					v.autoScroll = true
				}
			}
			return v, cmd
		}
		return v, nil

	default:
		var cmd tea.Cmd
		v.viewport, cmd = v.viewport.Update(msg)
		if m, ok := msg.(tea.MouseMsg); ok {
			if m.Action == tea.MouseActionPress {
				switch m.Button {
				case tea.MouseButtonWheelUp:
					v.autoScroll = false
				case tea.MouseButtonWheelDown:
					if v.viewport.AtBottom() {
						v.autoScroll = true
					}
				}
			}
		}
		return v, cmd
	}
}

func (v *LogViewer) rebuildViewport() {
	v.searchMatches = nil

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

	var sb strings.Builder
	viewLine := 0
	for i, l := range v.lines {
		if len(v.tabGroups) > 1 {
			if v.activeTabIdx < len(v.tabGroups) && l.Group != v.tabGroups[v.activeTabIdx] {
				continue
			}
		} else if v.podFilter >= 0 && v.podFilter < len(v.pods) && l.Pod != v.pods[v.podFilter] {
			continue
		}
		if lowFilter != "" && !strings.Contains(strings.ToLower(l.Text), lowFilter) {
			continue
		}

		// Search highlight takes priority over JSON colorization (both emit ANSI codes).
		text := l.Text
		if lowSearch != "" && strings.Contains(strings.ToLower(l.Text), lowSearch) {
			v.searchMatches = append(v.searchMatches, viewLine)
			text = highlightMatches(l.Text, lowSearch)
		} else if !l.IsSystem && i < len(v.colorCache) && v.colorCache[i] != "" {
			text = v.colorCache[i]
		}

		rendered := renderLogLineText(l, text)
		sb.WriteString(rendered)
		sb.WriteByte('\n')
		viewLine += strings.Count(rendered, "\n") + 1
	}

	if len(v.searchMatches) == 0 {
		v.searchCurrent = -1
	} else if v.searchCurrent >= len(v.searchMatches) {
		v.searchCurrent = len(v.searchMatches) - 1
	}

	v.viewport.SetContent(sb.String())
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
	color := styles.LogPrefixColors[colorIdx]
	prefix := lipgloss.NewStyle().Foreground(color).Bold(true).Render(fmt.Sprintf("[%s] ", l.Pod))
	if l.IsSystem {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Italic(true).Render(text)
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
	style := chromastyles.Get("dracula")
	if style == nil {
		style = chromastyles.Fallback
	}
	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}
	iterator, err := lexer.Tokenise(nil, src)
	if err != nil {
		return ""
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return ""
	}
	return strings.TrimRight(buf.String(), "\n")
}

func (v LogViewer) View() string {
	border := styles.NormalBorder
	if v.focused {
		border = styles.FocusedBorder
	}

	var title string
	if len(v.tabGroups) > 1 {
		// Tab mode: show active group name in title
		activeName := v.tabGroups[v.activeTabIdx]
		maxW := v.width - 30
		if len(activeName) > maxW && maxW > 3 {
			activeName = activeName[:maxW-1] + "…"
		}
		title = styles.Title.Render("Logs: ") + styles.Primary.Render(activeName)
	} else if v.podFilter >= 0 && v.podFilter < len(v.pods) {
		titlePods := v.pods[v.podFilter]
		maxW := v.width - 30
		if len(titlePods) > maxW && maxW > 3 {
			titlePods = titlePods[:maxW-1] + "…"
		}
		title = styles.Title.Render("Logs: ") + styles.Primary.Render(titlePods) +
			styles.Muted.Render(fmt.Sprintf("  [%d/%d · 0=all · esc]", v.podFilter+1, len(v.pods)))
	} else {
		titlePods := strings.Join(v.pods, ", ")
		if len(titlePods) > v.width-20 {
			titlePods = titlePods[:v.width-23] + "…"
		}
		title = styles.Title.Render("Logs: ") + styles.Primary.Render(titlePods)
	}

	scrollStatus := styles.Primary.Render("  ● live")
	if !v.autoScroll {
		pct := 100
		if v.viewport.TotalLineCount() > 0 {
			pct = int(v.viewport.ScrollPercent() * 100)
		}
		scrollStatus = styles.Warning.Render(fmt.Sprintf("  ⏸ %d%%", pct))
	}

	lineCount := fmt.Sprintf("  %d lines", len(v.lines))
	if len(v.tabGroups) > 1 && v.activeTabIdx < len(v.tabGroups) {
		shown := 0
		for _, l := range v.lines {
			if l.Group == v.tabGroups[v.activeTabIdx] {
				shown++
			}
		}
		lineCount = fmt.Sprintf("  %d/%d lines", shown, len(v.lines))
	} else if v.podFilter >= 0 && v.podFilter < len(v.pods) {
		shown := 0
		for _, l := range v.lines {
			if l.Pod == v.pods[v.podFilter] {
				shown++
			}
		}
		lineCount = fmt.Sprintf("  %d/%d lines", shown, len(v.lines))
	}

	indentHint := ""
	if !v.jsonIndent {
		indentHint = "  " + styles.Muted.Render("[json flat]")
	}

	// Tab bar (only in tab mode)
	tabBar := ""
	if len(v.tabGroups) > 1 {
		tabW := (v.width - 4) / len(v.tabGroups)
		var tabs []string
		for i, name := range v.tabGroups {
			label := fmt.Sprintf(" %d:%s ", i+1, name)
			if len(label) > tabW && tabW > 5 {
				label = fmt.Sprintf(" %d:%s ", i+1, name[:tabW-5]) + "… "
			}
			if i == v.activeTabIdx {
				tabs = append(tabs, styles.Warning.Bold(true).Render(label))
			} else {
				tabs = append(tabs, styles.Muted.Render(label))
			}
		}
		tabBar = "\n" + strings.Join(tabs, styles.Muted.Render("│"))
	}

	filterBar := ""
	if v.filterOn {
		filterBar = "\n" + styles.Primary.Render("filter: ") + v.filterInput + styles.Muted.Render("█")
	} else if v.filter != "" {
		filterBar = "\n" + styles.Primary.Render("filter: ") + styles.Warning.Render(v.filter) +
			styles.Muted.Render("  (/ change, esc clear)")
	}

	searchBar := ""
	if v.searchOn {
		extra := ""
		if v.searchInput != "" && len(v.searchMatches) > 0 {
			extra = "  " + styles.Muted.Render(fmt.Sprintf("%d matches", len(v.searchMatches)))
		}
		searchBar = "\n" + styles.Primary.Render("search: ") + v.searchInput + styles.Muted.Render("█") + extra
	} else if v.searchQuery != "" {
		matchInfo := "no matches"
		if len(v.searchMatches) > 0 {
			matchInfo = fmt.Sprintf("%d/%d", v.searchCurrent+1, len(v.searchMatches))
		}
		searchBar = "\n" + styles.Primary.Render("search: ") + styles.Warning.Render(v.searchQuery) +
			"  " + styles.Muted.Render(matchInfo+"  n↓ N↑  esc clear")
	}

	var help string
	if len(v.tabGroups) > 1 {
		help = styles.Muted.Render("  ↑↓/jk scroll  tab/1-9 switch  / filter  ctrl+f search  n/N next/prev  J indent  g top  G bottom  esc back")
	} else {
		help = styles.Muted.Render("  ↑↓/jk scroll  / filter  ctrl+f search  n/N next/prev  1-9 solo pod  0 all  J indent  g top  G bottom  esc back")
	}
	header := title + scrollStatus + indentHint + "  " + styles.Muted.Render(lineCount) + tabBar + filterBar + searchBar + "\n" + help

	return border.Width(v.width - 2).Height(v.height - 2).Render(header + "\n\n" + v.viewport.View())
}
