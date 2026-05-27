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
	lipgloss "charm.land/lipgloss/v2"
	k8sres "github.com/chaitanyak/klens/internal/k8s"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// YAMLAutoScrollTickMsg drives drag-to-copy auto-scroll in the YAML viewer.
type YAMLAutoScrollTickMsg struct{}

// YAMLAutoScrollTickCmd schedules one auto-scroll tick (50 ms cadence). The
// root model reissues this while a drag is in progress and the cursor sits
// outside the viewport vertically.
func YAMLAutoScrollTickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return YAMLAutoScrollTickMsg{}
	})
}

var yamlSelectionStyle = lipgloss.NewStyle().Reverse(true)

// YAMLFetchedMsg carries the YAML string for a fetched resource.
type YAMLFetchedMsg struct {
	Kind      string
	Name      string
	Namespace string
	YAML      string
	Err       error
}

// YAMLViewer displays syntax-highlighted YAML in a scrollable viewport.
type YAMLViewer struct {
	viewport  viewport.Model
	width     int
	height    int
	focused   bool
	kind      string
	name      string
	namespace string
	raw       string

	// rawLines and highlightedLines are parallel arrays — same length, same
	// indices. raw drives clipboard output; highlighted drives display.
	rawLines         []string
	highlightedLines []string

	// drag holds drag-to-copy lifecycle state (indices into rawLines /
	// highlightedLines). See DragSelection in drag.go.
	drag DragSelection
}

func NewYAMLViewer(w, h int) YAMLViewer {
	vp := viewport.New(viewport.WithWidth(w-3), viewport.WithHeight(h-4))
	vp.Style = lipgloss.NewStyle()
	return YAMLViewer{viewport: vp, width: w, height: h}
}

func (v YAMLViewer) SetSize(w, h int) YAMLViewer {
	v.width = w
	v.height = h
	v.viewport.SetWidth(max(1, w-3))
	v.viewport.SetHeight(max(1, h-4))
	return v
}
func (v YAMLViewer) SetFocused(f bool) YAMLViewer { v.focused = f; return v }
func (v YAMLViewer) RawYAML() string               { return v.raw }
func (v YAMLViewer) ResourceInfo() (kind, name, ns string) {
	return v.kind, v.name, v.namespace
}

func (v YAMLViewer) Update(msg tea.Msg) (YAMLViewer, tea.Cmd) {
	switch msg := msg.(type) {
	case YAMLFetchedMsg:
		v.kind = msg.Kind
		v.name = msg.Name
		v.namespace = msg.Namespace
		v.raw = msg.YAML
		v.rawLines = strings.Split(msg.YAML, "\n")
		v.highlightedLines = strings.Split(highlightYAML(msg.YAML), "\n")
		v.drag.Reset()
		v.rebuild()
		v.viewport.GotoTop()
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		v.viewport, cmd = v.viewport.Update(msg)
		return v, cmd
	case tea.KeyPressMsg:
		switch msg.String() {
		case "g":
			v.viewport.GotoTop()
			return v, nil
		case "G":
			v.viewport.GotoBottom()
			return v, nil
		default:
			var cmd tea.Cmd
			v.viewport, cmd = v.viewport.Update(msg)
			return v, cmd
		}
	}
	return v, nil
}

// rebuild stitches highlightedLines back into a single string, applying a
// reverse-style highlight to lines [lo..hi] of an active selection. Mirrors
// log_viewer's per-line render strategy so Chroma styling stays intact.
func (v *YAMLViewer) rebuild() {
	if len(v.highlightedLines) == 0 {
		v.viewport.SetContent("")
		return
	}
	if !v.drag.Active || v.drag.Start < 0 || v.drag.End < 0 {
		v.viewport.SetContent(strings.Join(v.highlightedLines, "\n"))
		return
	}
	lo, hi := v.drag.Range()
	if lo < 0 {
		lo = 0
	}
	if hi >= len(v.highlightedLines) {
		hi = len(v.highlightedLines) - 1
	}
	out := make([]string, len(v.highlightedLines))
	for i, line := range v.highlightedLines {
		if i >= lo && i <= hi {
			out[i] = yamlSelectionStyle.Render(line)
		} else {
			out[i] = line
		}
	}
	v.viewport.SetContent(strings.Join(out, "\n"))
}

// viewportBounds returns the inclusive panel-local rectangle of the viewport
// content area. Layout matches View(): title (row 0) + help (row 1) + blank
// (row 2) + viewport content starting at row 3.
func (v YAMLViewer) viewportBounds() (x1, y1, x2, y2 int, ok bool) {
	if v.viewport.Height() <= 0 {
		return 0, 0, 0, 0, false
	}
	x1 = 1 // panel border-left
	y1 = 1 + 3
	x2 = max(x1, v.width-2)
	y2 = y1 + v.viewport.Height() - 1
	return x1, y1, x2, y2, true
}

func (v YAMLViewer) lineAtScreenY(localY int) (int, bool) {
	_, y1, _, y2, ok := v.viewportBounds()
	if !ok || localY < y1 || localY > y2 {
		return 0, false
	}
	row := v.viewport.YOffset() + (localY - y1)
	if row < 0 || row >= len(v.highlightedLines) {
		return 0, false
	}
	return row, true
}

// HandleMouseDown begins a drag-select if the click lands on a YAML line in
// the viewport. Returns started=true so the root model can launch the
// auto-scroll tick.
func (v YAMLViewer) HandleMouseDown(localX, localY int) (YAMLViewer, bool) {
	idx, ok := v.lineAtScreenY(localY)
	if !ok {
		return v, false
	}
	v.drag.Begin(idx, localX, localY)
	v.rebuild()
	return v, true
}

// HandleMouseDrag extends the selection. Records dragLastX/Y so AutoScrollStep
// can drive scrolling when the cursor sits outside the viewport.
func (v YAMLViewer) HandleMouseDrag(localX, localY int) YAMLViewer {
	if !v.drag.Active {
		return v
	}
	v.drag.Track(localX, localY)
	idx, ok := v.lineAtScreenY(localY)
	if !ok {
		return v
	}
	if v.drag.Extend(idx) {
		v.rebuild()
	}
	return v
}

// HandleMouseUp finalises a drag-select. On a real drag (cursor crossed at
// least one line boundary), copies the raw YAML lines in [lo..hi] to the
// clipboard. A click without drag clears the selection but does not clobber
// the clipboard.
func (v YAMLViewer) HandleMouseUp(localX, localY int) (YAMLViewer, string) {
	if !v.drag.Active {
		return v, ""
	}
	dragged := v.drag.Moved
	lo, hi := v.drag.Range()
	v.drag.Reset()
	v.rebuild()
	if !dragged || lo < 0 || hi < 0 || lo >= len(v.rawLines) {
		return v, ""
	}
	if hi >= len(v.rawLines) {
		hi = len(v.rawLines) - 1
	}
	parts := v.rawLines[lo : hi+1]
	if err := clipboard.WriteAll(strings.Join(parts, "\n")); err != nil {
		return v, "copy failed: " + err.Error()
	}
	n := hi - lo + 1
	return v, fmt.Sprintf("copied %d line%s to clipboard", n, plural(n))
}

// IsDragging reports whether a drag-select is in progress.
func (v YAMLViewer) IsDragging() bool { return v.drag.Active }

// AutoScrollStep advances the viewport one line up or down when the last
// drag-cursor position sits outside the viewport vertically, and extends
// selEnd to the newly-revealed first/last visible line.
func (v YAMLViewer) AutoScrollStep() YAMLViewer {
	if !v.drag.Active || len(v.highlightedLines) == 0 {
		return v
	}
	_, y1, _, y2, ok := v.viewportBounds()
	if !ok {
		return v
	}
	if v.drag.LastY >= y1 && v.drag.LastY <= y2 {
		return v
	}
	height := v.viewport.Height()
	if height <= 0 {
		return v
	}
	rows := len(v.highlightedLines)
	maxOffset := rows - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	cur := v.viewport.YOffset()
	var next, endRow int
	if v.drag.LastY < y1 {
		if cur <= 0 {
			return v
		}
		next = cur - 1
		endRow = next
	} else {
		if cur >= maxOffset {
			return v
		}
		next = cur + 1
		endRow = next + height - 1
		if endRow >= rows {
			endRow = rows - 1
		}
	}
	v.viewport.SetYOffset(next)
	if endRow < 0 {
		endRow = 0
	}
	if v.drag.Extend(endRow) {
		v.rebuild()
	}
	return v
}

func (v YAMLViewer) View() string {
	border := appstyles.NormalBorder
	if v.focused {
		border = appstyles.FocusedBorder
	}
	title := appstyles.Title.Render(fmt.Sprintf("YAML: %s/%s", v.kind, v.name))
	help := "  " + RenderHelpInline([]HelpItem{
		{Key: "↑↓/jk", Desc: "scroll"},
		{Key: "g", Desc: "top"},
		{Key: "G", Desc: "bottom"},
		{Key: "e", Desc: "edit"},
		{Key: "F", Desc: "fullscreen"},
		{Key: "esc", Desc: "back"},
	})
	sbStr := renderScrollbar(
		v.viewport.Height(),
		v.viewport.VisibleLineCount(),
		v.viewport.TotalLineCount(),
		v.viewport.YOffset(),
		v.focused,
	)
	return border.Width(max(1, v.width)).Height(max(1, v.height)).Render(
		title + "\n" + help + "\n\n" + joinScrollbar(v.viewport.View(), sbStr),
	)
}

// FetchYAMLCmd returns a tea.Cmd that fetches the raw YAML for a resource.
func FetchYAMLCmd(cs kubernetes.Interface, kind, name, namespace string) tea.Cmd {
	return func() tea.Msg {
		obj, err := fetchObject(cs, kind, name, namespace)
		if err != nil {
			return YAMLFetchedMsg{Kind: kind, Name: name, Namespace: namespace, Err: err}
		}
		// Remove managed fields noise
		if m, ok := obj.(interface{ GetManagedFields() interface{} }); ok {
			_ = m
		}
		b, err := json.Marshal(obj)
		if err != nil {
			return YAMLFetchedMsg{Err: err}
		}
		y, err := yaml.JSONToYAML(b)
		if err != nil {
			return YAMLFetchedMsg{Err: err}
		}
		return YAMLFetchedMsg{
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
			YAML:      string(y),
		}
	}
}

// FetchHelmReleaseYAMLCmd builds YAML from the already-cached unstructured object — no network call needed.
func FetchHelmReleaseYAMLCmd(u *unstructured.Unstructured) tea.Cmd {
	return func() tea.Msg {
		data, err := yaml.Marshal(u.Object)
		if err != nil {
			return YAMLFetchedMsg{Err: err}
		}
		return YAMLFetchedMsg{
			Kind:      u.GetKind(),
			Name:      u.GetName(),
			Namespace: u.GetNamespace(),
			YAML:      string(data),
		}
	}
}

func fetchObject(cs kubernetes.Interface, kind, name, namespace string) (interface{}, error) {
	rd, ok := k8sres.Resolve(kind)
	if !ok || rd.Fetch == nil {
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
	return rd.Fetch(cs, name, namespace)
}

func highlightYAML(src string) string {
	lexer := lexers.Get("yaml")
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	iterator, err := lexer.Tokenise(nil, src)
	if err != nil {
		return src
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, klensChromaStyle, iterator); err != nil {
		return src
	}
	return buf.String()
}
