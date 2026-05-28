package panels

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
	k8sdescribe "github.com/chaitanyak/klens/internal/k8s/describe"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

// DescribeFetchedMsg carries the kubectl-style describe output for a resource.
type DescribeFetchedMsg struct {
	Kind      string
	Name      string
	Namespace string
	Text      string
	Err       error
}

// DescribeViewer renders kubectl describe output in a scrollable viewport with
// support for regex filter, match navigation, copy-to-clipboard, and
// save-to-file. Mirrors YAMLViewer's shape; no Chroma highlighting since the
// describe format is line-oriented plaintext.
type DescribeViewer struct {
	viewport viewport.Model
	width    int
	height   int
	focused  bool

	kind      string
	name      string
	namespace string

	// raw is the verbatim describe output; rawLines is its line split. matched
	// is the subset of rawLines indices that satisfy the active filter (or all
	// of them when filter == "").
	raw      string
	rawLines []string
	matched  []int

	// filterOn is true while the / input is open. filter is the *applied*
	// regex (empty when none); filterInput is what's currently being typed.
	filterOn    bool
	filterInput string
	filter      string

	// matchCursor indexes into matched[] when n/N is used. -1 means no
	// match navigation has occurred yet.
	matchCursor int

	// statusMsg is consumed by the controller and surfaced to the status bar
	// (copy / save outcomes). Cleared on next read.
	statusMsg string
}

func NewDescribeViewer(w, h int) DescribeViewer {
	vp := viewport.New(viewport.WithWidth(max(1, w-3)), viewport.WithHeight(max(1, h-5)))
	vp.Style = lipgloss.NewStyle()
	return DescribeViewer{viewport: vp, width: w, height: h, matchCursor: -1}
}

func (v DescribeViewer) SetSize(w, h int) DescribeViewer {
	v.width = w
	v.height = h
	v.viewport.SetWidth(max(1, w-3))
	v.viewport.SetHeight(max(1, h-5))
	return v
}

func (v DescribeViewer) SetFocused(f bool) DescribeViewer { v.focused = f; return v }

// HasActiveState reports whether ESC has a layer to peel before exiting the
// mode (open filter input, or a non-empty applied filter).
func (v DescribeViewer) HasActiveState() bool { return v.filterOn || v.filter != "" }

// HandleEsc peels one layer of state (open input → applied filter). Returns
// the updated viewer and a boolean: true means a layer was consumed and the
// caller should NOT also exit the mode.
func (v DescribeViewer) HandleEsc() (DescribeViewer, bool) {
	if v.filterOn {
		v.filterOn = false
		v.filterInput = ""
		return v, true
	}
	if v.filter != "" {
		v.filter = ""
		v.recompute()
		return v, true
	}
	return v, false
}

// ConsumeStatusMsg returns and clears any pending status message (e.g. "copied"
// after `c` or "saved" after `ctrl+s`). The controller polls this after every
// Update call.
func (v DescribeViewer) ConsumeStatusMsg() (DescribeViewer, string) {
	msg := v.statusMsg
	v.statusMsg = ""
	return v, msg
}

func (v DescribeViewer) Update(msg tea.Msg) (DescribeViewer, tea.Cmd) {
	switch msg := msg.(type) {
	case DescribeFetchedMsg:
		v.kind = msg.Kind
		v.name = msg.Name
		v.namespace = msg.Namespace
		v.raw = msg.Text
		v.rawLines = strings.Split(msg.Text, "\n")
		v.filter = ""
		v.filterInput = ""
		v.filterOn = false
		v.matchCursor = -1
		v.recompute()
		v.viewport.GotoTop()
		return v, nil
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		v.viewport, cmd = v.viewport.Update(msg)
		return v, cmd
	case tea.KeyPressMsg:
		return v.handleKey(msg)
	}
	return v, nil
}

func (v DescribeViewer) handleKey(msg tea.KeyPressMsg) (DescribeViewer, tea.Cmd) {
	if v.filterOn {
		switch msg.String() {
		case "enter":
			v.filterOn = false
			v.filter = v.filterInput
			v.matchCursor = -1
			v.recompute()
			v.viewport.GotoTop()
		case "esc":
			v.filterOn = false
			v.filterInput = ""
		case "backspace":
			if len(v.filterInput) > 0 {
				v.filterInput = v.filterInput[:len(v.filterInput)-1]
			}
		case "ctrl+v":
			if text, err := clipboard.ReadAll(); err == nil && text != "" {
				clean := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(text)
				v.filterInput += clean
			}
		default:
			if len(msg.Text) > 0 {
				v.filterInput += msg.Text
			}
		}
		return v, nil
	}

	switch msg.String() {
	case "/":
		v.filterOn = true
		v.filterInput = v.filter
		return v, nil
	case "n":
		v.jumpMatch(+1)
		return v, nil
	case "N":
		v.jumpMatch(-1)
		return v, nil
	case "g":
		v.viewport.GotoTop()
		return v, nil
	case "G":
		v.viewport.GotoBottom()
		return v, nil
	case "c":
		v.statusMsg = v.copyAll()
		return v, nil
	case "ctrl+s":
		v.statusMsg = v.saveToFile()
		return v, nil
	default:
		var cmd tea.Cmd
		v.viewport, cmd = v.viewport.Update(msg)
		return v, cmd
	}
}

// recompute rebuilds the matched-line index from rawLines + filter and pushes
// the resulting content into the viewport.
func (v *DescribeViewer) recompute() {
	if v.filter == "" {
		v.matched = nil
		v.viewport.SetContent(strings.Join(v.rawLines, "\n"))
		return
	}
	re, err := regexp.Compile("(?i)" + v.filter)
	if err != nil {
		// Bad regex → treat as plain substring (case-insensitive).
		needle := strings.ToLower(v.filter)
		var visible []string
		v.matched = v.matched[:0]
		for i, line := range v.rawLines {
			if strings.Contains(strings.ToLower(line), needle) {
				v.matched = append(v.matched, i)
				visible = append(visible, line)
			}
		}
		v.viewport.SetContent(strings.Join(visible, "\n"))
		return
	}
	var visible []string
	v.matched = v.matched[:0]
	for i, line := range v.rawLines {
		if re.MatchString(line) {
			v.matched = append(v.matched, i)
			visible = append(visible, line)
		}
	}
	v.viewport.SetContent(strings.Join(visible, "\n"))
}

// jumpMatch scrolls the viewport to the next/prev match. In filtered view all
// visible lines are matches, so this advances the viewport one line at a time
// through them — the visible content's row index matches matchCursor.
func (v *DescribeViewer) jumpMatch(delta int) {
	if len(v.matched) == 0 {
		return
	}
	if v.matchCursor < 0 {
		if delta > 0 {
			v.matchCursor = 0
		} else {
			v.matchCursor = len(v.matched) - 1
		}
	} else {
		v.matchCursor = (v.matchCursor + delta + len(v.matched)) % len(v.matched)
	}
	v.viewport.SetYOffset(v.matchCursor)
}

func (v DescribeViewer) copyAll() string {
	if v.raw == "" {
		return "describe: nothing to copy"
	}
	if err := clipboard.WriteAll(v.raw); err != nil {
		return "copy failed: " + err.Error()
	}
	return fmt.Sprintf("copied %d lines to clipboard", len(v.rawLines))
}

// saveToFile writes the describe output to $KLENS_DUMP_DIR (default ~/.klens/dumps),
// using a timestamp-suffixed filename so repeated dumps don't clobber each other.
func (v DescribeViewer) saveToFile() string {
	if v.raw == "" {
		return "describe: nothing to save"
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
	ts := time.Now().Format("20060102-150405")
	name := fmt.Sprintf("%s-%s-%s.txt", strings.ToLower(v.kind), v.name, ts)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(v.raw), 0o644); err != nil {
		return "save failed: " + err.Error()
	}
	return "saved to " + path
}

func (v DescribeViewer) View() string {
	border := appstyles.NormalBorder
	if v.focused {
		border = appstyles.FocusedBorder
	}
	// Title matches the spec: `Describe: <Kind>/<Name>` for cluster-scoped
	// resources, `Describe: <Namespace>/<Kind>/<Name>` when namespaced.
	titleText := fmt.Sprintf("Describe: %s/%s", v.kind, v.name)
	if v.namespace != "" {
		titleText = fmt.Sprintf("Describe: %s/%s/%s", v.namespace, v.kind, v.name)
	}
	title := appstyles.Title.Render(titleText)
	help := "  " + RenderHelpInline([]HelpItem{
		{Key: "↑↓/jk", Desc: "scroll"},
		{Key: "/", Desc: "filter"},
		{Key: "n", Desc: "next"},
		{Key: "c", Desc: "copy"},
		{Key: "ctrl+s", Desc: "save"},
		{Key: "F", Desc: "fullscreen"},
		{Key: "esc", Desc: "back"},
	})
	statusLine := v.renderStatusLine()
	sbStr := renderScrollbar(
		v.viewport.Height(),
		v.viewport.VisibleLineCount(),
		v.viewport.TotalLineCount(),
		v.viewport.YOffset(),
		v.focused,
	)
	body := joinScrollbar(v.viewport.View(), sbStr)
	return border.Width(max(1, v.width)).Height(max(1, v.height)).Render(
		title + "\n" + help + "\n" + statusLine + "\n" + body,
	)
}

// renderStatusLine returns the filter input prompt (when open) or the applied
// filter summary (when set), padded so the viewport always starts at the same
// row regardless of state.
func (v DescribeViewer) renderStatusLine() string {
	if v.filterOn {
		return "  " + appstyles.Primary.Render("filter: ") + v.filterInput + appstyles.Muted.Render("█")
	}
	if v.filter != "" {
		summary := fmt.Sprintf("filter: %s  (%d/%d lines)", v.filter, len(v.matched), len(v.rawLines))
		return "  " + appstyles.Muted.Render(summary)
	}
	return ""
}

// FetchDescribeCmd returns a tea.Cmd that runs kubectl-style describe in the
// background and emits DescribeFetchedMsg when done. Mirrors FetchYAMLCmd.
func FetchDescribeCmd(cfg *rest.Config, gvk schema.GroupVersionKind, kind, name, namespace string) tea.Cmd {
	return func() tea.Msg {
		if cfg == nil {
			return DescribeFetchedMsg{Kind: kind, Name: name, Namespace: namespace,
				Err: fmt.Errorf("no cluster connection")}
		}
		text, err := k8sdescribe.Describe(cfg, gvk, namespace, name)
		return DescribeFetchedMsg{
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
			Text:      text,
			Err:       err,
		}
	}
}
