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
	// styles.Header has Padding(0,1) — 1 char left + 1 char right.
	// All width arithmetic must use the inner content area, not the full terminal width.
	inner := max(1, h.width-2)

	// Build right side: shortcuts and optional version string.
	right := strings.Join([]string{
		styles.Muted.Render("ctrl+k") + styles.HelpDesc.Render(" ctx"),
		styles.Muted.Render("ctrl+n") + styles.HelpDesc.Render(" ns"),
	}, "  ")
	if h.version != "" {
		right = styles.Muted.Render("k8s "+h.version) + "  " + right
	}
	rightW := lipgloss.Width(right)

	// Fixed display overhead of the left section (labels + spacing):
	//   "  cluster: " + "  ns: " = 17 chars; "  ● [RO]" adds 8 more.
	fixedLeft := 17
	if h.readOnly {
		fixedLeft += 8
	}

	// Hide the right section when the inner area is too narrow to fit both sides
	// with at least a 1-char gap and a trailing space.
	showRight := inner >= fixedLeft+4+rightW+2
	var namesAvail int
	if showRight {
		namesAvail = max(2, inner-fixedLeft-rightW-2)
	} else {
		right = ""
		rightW = 0
		namesAvail = max(2, inner-fixedLeft-2)
	}

	// Distribute available space: cluster ~60 %, namespace ~40 %.
	clusterMax := max(1, namesAvail*3/5)
	nsMax := max(1, namesAvail-clusterMax)

	left := fmt.Sprintf("  %s %s  %s %s",
		styles.Muted.Render("cluster:"),
		styles.Primary.Bold(true).Render(truncateHeader(h.cluster, clusterMax)),
		styles.Muted.Render("ns:"),
		styles.Success.Render(truncateHeader(h.namespace, nsMax)),
	)
	if h.readOnly {
		left += "  " + lipgloss.NewStyle().Foreground(styles.ColorReadOnly).Bold(true).Render("● [RO]")
	}

	leftW := lipgloss.Width(left)

	var line string
	if showRight {
		gap := max(1, inner-leftW-rightW-1)
		line = left + strings.Repeat(" ", gap) + right + " "
	} else {
		line = left
	}

	return styles.Header.
		Width(h.width).
		Render(line)
}

// truncateHeader shortens s to at most n runes, appending "…" if cut.
func truncateHeader(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}
