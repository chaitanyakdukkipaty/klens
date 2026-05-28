package panels

import (
	"fmt"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
	"github.com/chaitanyak/klens/internal/ui/widgets"
)

// XRayPanel renders a unicode tree of resource relationships.
type XRayPanel struct {
	viewport viewport.Model
	width    int
	height   int
	focused  bool
	kind     string
	name     string
}

func NewXRayPanel(w, h int) XRayPanel {
	vp := viewport.New(viewport.WithWidth(max(1, w-4)), viewport.WithHeight(max(1, h-6)))
	return XRayPanel{viewport: vp, width: w, height: h}
}

func (t XRayPanel) SetSize(w, h int) XRayPanel {
	t.width = w
	t.height = h
	t.viewport.SetWidth(max(1, w-4))
	t.viewport.SetHeight(max(1, h-6))
	return t
}

func (t XRayPanel) SetFocused(f bool) XRayPanel { t.focused = f; return t }

func (t XRayPanel) SetTree(kind, name string, root *k8s.TreeNode) XRayPanel {
	t.kind = kind
	t.name = name
	if root != nil {
		t.viewport.SetContent(widgets.RenderTree(root))
	} else {
		t.viewport.SetContent(styles.Muted.Render("  No XRay data available"))
	}
	t.viewport.GotoTop()
	return t
}

func (t XRayPanel) Update(msg tea.Msg) (XRayPanel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		var cmd tea.Cmd
		t.viewport, cmd = t.viewport.Update(msg)
		return t, cmd
	case tea.KeyPressMsg:
		switch msg.String() {
		case "g":
			t.viewport.GotoTop()
			return t, nil
		case "G":
			t.viewport.GotoBottom()
			return t, nil
		}
		var cmd tea.Cmd
		t.viewport, cmd = t.viewport.Update(msg)
		return t, cmd
	}
	return t, nil
}

func (t XRayPanel) View() string {
	border := styles.NormalBorder
	if t.focused {
		border = styles.FocusedBorder
	}
	title := styles.Title.Render(fmt.Sprintf("XRay: %s/%s", t.kind, t.name))
	help := "  " + RenderHelpInline([]HelpItem{
		{Key: "↑↓/jk", Desc: "scroll"},
		{Key: "g", Desc: "top"},
		{Key: "G", Desc: "bottom"},
		{Key: "F", Desc: "fullscreen"},
		{Key: "esc", Desc: "back"},
	})
	sbStr := renderScrollbar(
		t.viewport.Height(),
		t.viewport.VisibleLineCount(),
		t.viewport.TotalLineCount(),
		t.viewport.YOffset(),
		t.focused,
	)
	return border.Width(max(1, t.width)).Height(max(1, t.height)).Render(
		title + "\n" + help + "\n\n" + joinScrollbar(t.viewport.View(), sbStr))
}
