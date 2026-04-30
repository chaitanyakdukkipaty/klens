package panels

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"
	k8smetrics "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
	"github.com/chaitanyak/klens/internal/ui/widgets"
)

// MetricsPanel shows CPU and memory time-series charts for a selected resource.
type MetricsPanel struct {
	viewport  viewport.Model
	width     int
	height    int
	focused   bool
	name      string
	namespace string
	metrics   *k8smetrics.ResourceMetrics
	cpuReqM   int64
	cpuLimM   int64
	memReqB   int64
	memLimB   int64
}

func NewMetricsPanel(w, h int) MetricsPanel {
	vp := viewport.New(w-4, h-8)
	return MetricsPanel{viewport: vp, width: w, height: h}
}

func (m MetricsPanel) SetSize(w, h int) MetricsPanel {
	m.width = w
	m.height = h
	m.viewport.Width = w - 4
	m.viewport.Height = h - 8
	return m
}

func (m MetricsPanel) SetFocused(f bool) MetricsPanel { m.focused = f; return m }

func (m MetricsPanel) SetResource(name, namespace string, metrics *k8smetrics.ResourceMetrics) MetricsPanel {
	m.name = name
	m.namespace = namespace
	m.metrics = metrics
	m.rebuildContent()
	return m
}

func (m MetricsPanel) SetLimits(cpuReqM, cpuLimM, memReqB, memLimB int64) MetricsPanel {
	m.cpuReqM = cpuReqM
	m.cpuLimM = cpuLimM
	m.memReqB = memReqB
	m.memLimB = memLimB
	m.rebuildContent()
	return m
}

func (m *MetricsPanel) rebuildContent() {
	if m.metrics == nil {
		m.viewport.SetContent(styles.Muted.Render("  No metrics available (metrics-server may not be installed)"))
		return
	}

	chartW := m.width/2 - 6
	if chartW < 20 {
		chartW = 20
	}
	chartH := 8

	var sb strings.Builder

	// CPU chart
	cpuTitle := styles.Title.Render("CPU (millicores)")
	sb.WriteString(cpuTitle + "\n")
	if len(m.metrics.CPUSamples) > 1 {
		graph := asciigraph.Plot(m.metrics.CPUSamples,
			asciigraph.Height(chartH),
			asciigraph.Width(chartW),
			asciigraph.Caption(fmt.Sprintf("latest: %.1fm", m.metrics.CPULatest)),
		)
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#00ADD8")).Render(graph))
	} else {
		sb.WriteString(styles.Muted.Render("  Collecting samples…"))
	}
	sb.WriteString("\n")
	sb.WriteString(resourceLimitsLine(int64(m.metrics.CPULatest), m.cpuReqM, m.cpuLimM, "m"))
	sb.WriteString("\n\n")

	// Memory chart
	memSamplesGB := make([]float64, len(m.metrics.MEMSamples))
	for i, v := range m.metrics.MEMSamples {
		memSamplesGB[i] = v / (1024 * 1024)
	}
	memTitle := styles.Title.Render("Memory (MiB)")
	sb.WriteString(memTitle + "\n")
	if len(memSamplesGB) > 1 {
		graph := asciigraph.Plot(memSamplesGB,
			asciigraph.Height(chartH),
			asciigraph.Width(chartW),
			asciigraph.Caption(fmt.Sprintf("latest: %.1f MiB", m.metrics.MEMLatest/(1024*1024))),
		)
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#4CAF50")).Render(graph))
	} else {
		sb.WriteString(styles.Muted.Render("  Collecting samples…"))
	}
	sb.WriteString("\n")
	memUsageMiB := int64(m.metrics.MEMLatest) / (1024 * 1024)
	memReqMiB := m.memReqB / (1024 * 1024)
	memLimMiB := m.memLimB / (1024 * 1024)
	sb.WriteString(resourceLimitsLine(memUsageMiB, memReqMiB, memLimMiB, " MiB"))

	// Inline sparklines summary
	sb.WriteString("\n\n")
	cpuSpark := widgets.Sparkline(m.metrics.CPUSamples, 20)
	memSpark := widgets.Sparkline(memSamplesGB, 20)
	sb.WriteString(styles.Primary.Render("CPU  ") + cpuSpark +
		"  " + styles.Success.Render("MEM  ") + memSpark)

	m.viewport.SetContent(sb.String())
}

func (m MetricsPanel) Update(msg tea.Msg) (MetricsPanel, tea.Cmd) {
	switch msg := msg.(type) {
	case k8smetrics.MetricsUpdatedMsg:
		if m.namespace != "" {
			key := m.namespace + "/" + m.name
			if rm, ok := msg.Pods[key]; ok {
				m.metrics = rm
				m.rebuildContent()
			}
		}
	case tea.KeyMsg:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// resourceLimitsLine renders "Req: <req><unit>  Lim: <lim><unit>  (<pct>% req / <pct>% lim)"
// with colored percentages. unit is "m" for millicores or " MiB" for memory.
func resourceLimitsLine(usage, req, lim int64, unit string) string {
	reqStr := "—"
	limStr := "—"
	pctReq := "~"
	pctLim := "~"
	if req > 0 {
		reqStr = fmt.Sprintf("%d%s", req, unit)
		pct := usage * 100 / req
		pctReq = colorPct(pct)
	}
	if lim > 0 {
		limStr = fmt.Sprintf("%d%s", lim, unit)
		pct := usage * 100 / lim
		pctLim = colorPct(pct)
	}
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#9E9E9E"))
	return muted.Render(fmt.Sprintf("  Req: %s  Lim: %s", reqStr, limStr)) +
		"  (" + pctReq + muted.Render("% req") + " / " + pctLim + muted.Render("% lim") + ")"
}

func colorPct(pct int64) string {
	s := fmt.Sprintf("%d", pct)
	switch {
	case pct >= 90:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#F44336")).Render(s)
	case pct >= 70:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFC107")).Render(s)
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#9E9E9E")).Render(s)
	}
}

func (m MetricsPanel) View() string {
	border := styles.NormalBorder
	if m.focused {
		border = styles.FocusedBorder
	}
	title := styles.Title.Render(fmt.Sprintf("Metrics: %s", m.name))
	help := styles.Muted.Render("  ↑↓ scroll  esc back  (refreshes every 15s)")
	return border.Width(m.width - 2).Height(m.height - 2).Render(
		title + "\n" + help + "\n\n" + m.viewport.View())
}
