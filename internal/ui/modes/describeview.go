package modes

import (
	tea "charm.land/bubbletea/v2"
	"github.com/chaitanyak/klens/internal/ui/panels"
)

// DescribeViewController wraps panels.DescribeViewer. The describe view owns
// its own filter input + match navigation, so the controller stays a thin
// dispatch layer — there are no root-owned operations to defer.
type DescribeViewController struct {
	panel panels.DescribeViewer
}

func NewDescribeViewController(p panels.DescribeViewer) DescribeViewController {
	return DescribeViewController{panel: p}
}

func (c DescribeViewController) Panel() panels.DescribeViewer { return c.panel }
func (c DescribeViewController) SetPanel(p panels.DescribeViewer) DescribeViewController {
	c.panel = p
	return c
}

func (c DescribeViewController) View() string { return c.panel.View() }

func (c DescribeViewController) SetSize(w, h int) Controller {
	c.panel = c.panel.SetSize(w, h)
	return c
}

// HasActiveState / HandleEsc let the root peel one layer at a time: open
// filter input → applied filter → exit mode. Matches the log viewer pattern.
func (c DescribeViewController) HasActiveState() bool { return c.panel.HasActiveState() }
func (c DescribeViewController) HandleEsc() (DescribeViewController, bool) {
	p, consumed := c.panel.HandleEsc()
	c.panel = p
	return c, consumed
}

// ConsumeStatusMsg surfaces copy/save outcomes to the status bar.
func (c DescribeViewController) ConsumeStatusMsg() (DescribeViewController, string) {
	p, msg := c.panel.ConsumeStatusMsg()
	c.panel = p
	return c, msg
}

func (c DescribeViewController) Update(msg tea.Msg) (Controller, tea.Cmd) {
	p, cmd := c.panel.Update(msg)
	c.panel = p
	return c, cmd
}

// HandleKey absorbs every key the describe viewer cares about. The viewer
// itself handles the filter input lifecycle, so nothing here needs to escape
// back to the root.
func (c DescribeViewController) HandleKey(k tea.KeyPressMsg) (Controller, tea.Cmd, bool) {
	p, cmd := c.panel.Update(k)
	c.panel = p
	return c, cmd, true
}
