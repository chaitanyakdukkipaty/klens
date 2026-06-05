package panels

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/chaitanyak/klens/internal/ui/styles"
)

// HeaderChip identifies a clickable element in the header bar.
type HeaderChip int

const (
	ChipNone HeaderChip = iota
	ChipCluster
	ChipNamespace
	ChipMenu
)

type Header struct {
	width     int
	cluster   string
	namespace string
	version   string
	readOnly  bool
	hover     HeaderChip
}

func NewHeader(width int) Header {
	return Header{width: width, cluster: "—", namespace: "default"}
}

func (h Header) SetWidth(w int) Header          { h.width = w; return h }
func (h Header) SetCluster(c string) Header     { h.cluster = c; return h }
func (h Header) SetNamespace(n string) Header   { h.namespace = n; return h }
func (h Header) SetVersion(v string) Header     { h.version = v; return h }
func (h Header) SetReadOnly(ro bool) Header     { h.readOnly = ro; return h }
func (h Header) SetHover(c HeaderChip) Header   { h.hover = c; return h }
func (h Header) Hover() HeaderChip              { return h.hover }

// headerSeg is one rendered run of header text and its chip identity
// (ChipNone for inert text). x is header-local (padding included), so it
// matches the X the hit map hands out for ZoneHeader.
type headerSeg struct {
	chip HeaderChip
	text string // plain text; styled at render time
	x    int
}

// segments computes the header line layout. Shared by View and ChipAt so the
// rendered bar and its click targets can never disagree.
//
//	 ⎈ cluster ▾   ns: namespace ▾   ● [RO] … k8s version  ☰
func (h Header) segments() []headerSeg {
	// styles.Header pads 1 left + 1 right; work in padded coordinates.
	inner := max(1, h.width-2)

	clusterChip := func(name string) string { return "⎈ " + name + " ▾" }
	nsChip := func(name string) string { return "ns: " + name + " ▾" }

	// Fixed overhead: chip glyphs/labels and separators, excluding the names.
	const gap = "  "
	fixed := lipgloss.Width(clusterChip("")) + lipgloss.Width(nsChip("")) + len(gap)
	roBadge := "● [RO]"
	if h.readOnly {
		fixed += len(gap) + lipgloss.Width(roBadge)
	}

	// Right side: optional version, then the menu button at the far edge.
	menu := "☰"
	right := menu
	if h.version != "" {
		right = "k8s " + h.version + "  " + menu
	}
	rightW := lipgloss.Width(right)

	showRight := inner >= fixed+8+rightW+2
	namesAvail := inner - fixed - 2
	if showRight {
		namesAvail = inner - fixed - rightW - 2
	}
	namesAvail = max(2, namesAvail)

	clusterMax := max(1, namesAvail*3/5)
	nsMax := max(1, namesAvail-clusterMax)

	segs := make([]headerSeg, 0, 5)
	x := 1 // padding column
	add := func(chip HeaderChip, text string) {
		segs = append(segs, headerSeg{chip: chip, text: text, x: x})
		x += lipgloss.Width(text)
	}

	add(ChipCluster, clusterChip(truncateHeader(h.cluster, clusterMax)))
	add(ChipNone, gap)
	add(ChipNamespace, nsChip(truncateHeader(h.namespace, nsMax)))
	if h.readOnly {
		add(ChipNone, gap)
		add(ChipNone, roBadge)
	}

	if showRight {
		// Right-anchored block: version text then menu at the last column.
		rx := 1 + inner - rightW
		if rx > x {
			if h.version != "" {
				segs = append(segs, headerSeg{chip: ChipNone, text: "k8s " + h.version + "  ", x: rx})
				rx += lipgloss.Width("k8s "+h.version) + 2
			}
			segs = append(segs, headerSeg{chip: ChipMenu, text: menu, x: rx})
		}
	}
	return segs
}

// ChipAt resolves a header-local X (as delivered by the hit map) to the chip
// under it. The menu button gets a one-cell grace margin on each side.
func (h Header) ChipAt(x int) (HeaderChip, bool) {
	for _, seg := range h.segments() {
		if seg.chip == ChipNone {
			continue
		}
		lo, hi := seg.x, seg.x+lipgloss.Width(seg.text)
		if seg.chip == ChipMenu {
			lo, hi = lo-1, hi+1
		}
		if x >= lo && x < hi {
			return seg.chip, true
		}
	}
	return ChipNone, false
}

func (h Header) View() string {
	segs := h.segments()
	var b strings.Builder
	x := 1
	for _, seg := range segs {
		if seg.x > x {
			b.WriteString(strings.Repeat(" ", seg.x-x))
			x = seg.x
		}
		b.WriteString(h.renderSeg(seg))
		x += lipgloss.Width(seg.text)
	}
	return styles.Header.Width(h.width).Render(b.String())
}

func (h Header) renderSeg(seg headerSeg) string {
	hovered := seg.chip != ChipNone && seg.chip == h.hover
	switch seg.chip {
	case ChipCluster:
		st := styles.Primary.Bold(true)
		if hovered {
			st = st.Underline(true)
		}
		return st.Render(seg.text)
	case ChipNamespace:
		st := styles.Success
		if hovered {
			st = st.Underline(true)
		}
		return st.Render(seg.text)
	case ChipMenu:
		if hovered {
			return styles.Primary.Bold(true).Render(seg.text)
		}
		return styles.Muted.Render(seg.text)
	default:
		if seg.text == "● [RO]" {
			return lipgloss.NewStyle().Foreground(styles.ColorReadOnly).Bold(true).Render(seg.text)
		}
		return styles.Muted.Render(seg.text)
	}
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
