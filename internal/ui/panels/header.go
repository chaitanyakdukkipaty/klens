package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

type Header struct {
	width     int
	cluster   string
	namespace string
	version   string
	readOnly  bool
}

func NewHeader(width int) Header {
	return Header{width: width, cluster: "—", namespace: "default"}
}

func (h Header) SetWidth(w int) Header        { h.width = w; return h }
func (h Header) SetCluster(c string) Header   { h.cluster = c; return h }
func (h Header) SetNamespace(n string) Header { h.namespace = n; return h }
func (h Header) SetVersion(v string) Header   { h.version = v; return h }
func (h Header) SetReadOnly(ro bool) Header   { h.readOnly = ro; return h }

func (h Header) View() string {
	left := fmt.Sprintf("  %s %s  %s %s",
		styles.Muted.Render("cluster:"),
		styles.Primary.Bold(true).Render(h.cluster),
		styles.Muted.Render("ns:"),
		styles.Success.Render(h.namespace),
	)
	if h.readOnly {
		left += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B")).Bold(true).Render("[RO]")
	}

	right := strings.Join([]string{
		styles.Muted.Render("ctrl+k") + styles.HelpDesc.Render(" ctx"),
		styles.Muted.Render("ctrl+n") + styles.HelpDesc.Render(" ns"),
	}, "  ")

	if h.version != "" {
		right = styles.Muted.Render("k8s "+h.version) + "  " + right
	}

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := h.width - leftW - rightW - 2
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right + " "

	return styles.Header.
		Width(h.width).
		Background(lipgloss.Color("#0D1117")).
		Render(line)
}
