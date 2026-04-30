package panels

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
	"github.com/chaitanyak/klens/internal/ui/widgets"
)

// TopologyPanel renders a unicode tree of resource relationships.
type TopologyPanel struct {
	viewport viewport.Model
	width    int
	height   int
	focused  bool
	kind     string
	name     string
}

func NewTopologyPanel(w, h int) TopologyPanel {
	vp := viewport.New(w-4, h-6)
	return TopologyPanel{viewport: vp, width: w, height: h}
}

func (t TopologyPanel) SetSize(w, h int) TopologyPanel {
	t.width = w
	t.height = h
	t.viewport.Width = w - 4
	t.viewport.Height = h - 6
	return t
}

func (t TopologyPanel) SetFocused(f bool) TopologyPanel { t.focused = f; return t }

func (t TopologyPanel) SetTree(kind, name string, root *k8s.TreeNode) TopologyPanel {
	t.kind = kind
	t.name = name
	if root != nil {
		t.viewport.SetContent(widgets.RenderTree(root))
	} else {
		t.viewport.SetContent(styles.Muted.Render("  No topology data available"))
	}
	t.viewport.GotoTop()
	return t
}

func (t TopologyPanel) Update(msg tea.Msg) (TopologyPanel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		var cmd tea.Cmd
		t.viewport, cmd = t.viewport.Update(msg)
		return t, cmd
	}
	return t, nil
}

func (t TopologyPanel) View() string {
	border := styles.NormalBorder
	if t.focused {
		border = styles.FocusedBorder
	}
	title := styles.Title.Render(fmt.Sprintf("Topology: %s/%s", t.kind, t.name))
	help := styles.Muted.Render("  ↑↓ scroll  esc back")
	return border.Width(t.width - 2).Height(t.height - 2).Render(
		title + "\n" + help + "\n\n" + t.viewport.View())
}
