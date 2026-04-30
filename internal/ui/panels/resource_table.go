package panels

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	k8sres "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ContentMode controls what is shown in the content panel.
type ContentMode int

const (
	ModeTable ContentMode = iota
	ModeYAML
	ModeEditor
	ModeLogs
	ModeTopology
	ModeMetrics
	ModeEvents
)

// ResourceTable displays a live table of Kubernetes resources.
type ResourceTable struct {
	width    int
	height   int
	focused  bool
	kind     string
	rows     []k8sres.ResourceRow
	filtered []k8sres.ResourceRow
	cursor   int
	selected map[string]bool // selected row names (for multi-select)
	filter   string
	filterOn bool
	filterInput string
}

func NewResourceTable(w, h int) ResourceTable {
	return ResourceTable{
		width:    w,
		height:   h,
		selected: make(map[string]bool),
	}
}

func (t ResourceTable) SetSize(w, h int) ResourceTable { t.width = w; t.height = h; return t }
func (t ResourceTable) SetFocused(f bool) ResourceTable { t.focused = f; return t }
func (t ResourceTable) SetKind(kind string) ResourceTable {
	if t.kind != kind {
		t.cursor = 0
		t.selected = make(map[string]bool)
		t.filter = ""
	}
	t.kind = kind
	return t
}

func (t ResourceTable) SelectedRow() *k8sres.ResourceRow {
	if len(t.filtered) == 0 {
		return nil
	}
	if t.cursor >= len(t.filtered) {
		return nil
	}
	r := t.filtered[t.cursor]
	return &r
}

func (t ResourceTable) SelectedPods() []string {
	var names []string
	for name, ok := range t.selected {
		if ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 && t.SelectedRow() != nil {
		names = append(names, t.SelectedRow().Name)
	}
	return names
}

func (t ResourceTable) SelectedRows() []*k8sres.ResourceRow {
	var rows []*k8sres.ResourceRow
	for i := range t.filtered {
		if t.selected[t.filtered[i].Name] {
			rows = append(rows, &t.filtered[i])
		}
	}
	if len(rows) == 0 {
		if r := t.SelectedRow(); r != nil {
			return []*k8sres.ResourceRow{r}
		}
	}
	return rows
}

func (t ResourceTable) ClearSelection() ResourceTable {
	t.selected = make(map[string]bool)
	return t
}

func (t ResourceTable) SelectionCount() int {
	n := 0
	for _, ok := range t.selected {
		if ok {
			n++
		}
	}
	return n
}

func (t ResourceTable) FilterActive() bool { return t.filterOn }
func (t ResourceTable) HasFilter() bool    { return t.filterOn || t.filter != "" }

func (t ResourceTable) Update(msg tea.Msg) (ResourceTable, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if t.filterOn {
			switch msg.String() {
			case "enter":
				t.filterOn = false
				t.filter = t.filterInput
				t.applyFilter()
			case "esc":
				t.filterOn = false
				t.filterInput = ""
				t.filter = ""
				t.applyFilter()
			case "backspace":
				if len(t.filterInput) > 0 {
					t.filterInput = t.filterInput[:len(t.filterInput)-1]
					t.applyFilter()
				}
			default:
				if len(msg.String()) == 1 {
					t.filterInput += msg.String()
					t.applyFilter()
				}
			}
			return t, nil
		}
		switch msg.String() {
		case "up", "k":
			if len(t.filtered) > 0 {
				t.cursor = (t.cursor - 1 + len(t.filtered)) % len(t.filtered)
			}
		case "down", "j":
			if len(t.filtered) > 0 {
				t.cursor = (t.cursor + 1) % len(t.filtered)
			}
		case "g":
			t.cursor = 0
		case "G":
			if len(t.filtered) > 0 {
				t.cursor = len(t.filtered) - 1
			}
		case "/":
			t.filterOn = true
			t.filterInput = t.filter
		case "esc":
			t.filter = ""
			t.filterInput = ""
			t.applyFilter()
		case " ":
			if row := t.SelectedRow(); row != nil {
				t.selected[row.Name] = !t.selected[row.Name]
			}
		}
	}
	return t, nil
}

func (t *ResourceTable) applyFilter() {
	if t.filterInput == "" {
		t.filtered = t.rows
		return
	}
	low := strings.ToLower(t.filterInput)
	// Allocate a new slice to avoid corrupting t.rows when t.filtered shares
	// its backing array (set via t.filtered = t.rows when no filter is active).
	filtered := make([]k8sres.ResourceRow, 0, len(t.rows))
	for _, r := range t.rows {
		if strings.Contains(strings.ToLower(r.Name), low) {
			filtered = append(filtered, r)
		}
	}
	t.filtered = filtered
	if t.cursor >= len(t.filtered) {
		t.cursor = max(0, len(t.filtered)-1)
	}
}

func (t ResourceTable) View() string {
	border := styles.NormalBorder
	if t.focused {
		border = styles.FocusedBorder
	}
	innerW := t.width - 2
	innerH := t.height - 2

	desc, ok := k8sres.Resolve(t.kind)
	if !ok {
		return border.Width(innerW).Height(innerH).Render(
			styles.Muted.Render("  Select a resource type from the left panel"))
	}

	// Title row
	countInfo := fmt.Sprintf(" %d", len(t.filtered))
	if t.filter != "" {
		countInfo = fmt.Sprintf(" %d/%d", len(t.filtered), len(t.rows))
	}
	title := styles.Title.Render(t.kind) + styles.Muted.Render(countInfo)
	if n := t.SelectionCount(); n > 0 {
		title += styles.Primary.Render(fmt.Sprintf("  ·  %d selected", n))
	}

	// Filter bar
	filterBar := ""
	if t.filterOn {
		filterBar = "\n" + styles.Primary.Render("filter: ") + t.filterInput + styles.Muted.Render("█")
	} else if t.filter != "" {
		filterBar = "\n" + styles.Primary.Render("filter: ") + styles.Warning.Render(t.filter) + styles.Muted.Render("  (/ to change, esc to clear)")
	}

	// Header
	header := buildHeader(desc, innerW)

	// Rows
	visibleRows := innerH - 3 // title + header + optional filter
	if filterBar != "" {
		visibleRows--
	}

	start := 0
	if t.cursor >= visibleRows {
		start = t.cursor - visibleRows + 1
	}

	var rowLines []string
	for i := start; i < len(t.filtered) && i < start+visibleRows; i++ {
		row := t.filtered[i]
		sel := t.selected[row.Name]
		isCursor := i == t.cursor

		line := buildRow(row, desc, innerW, sel, isCursor)
		rowLines = append(rowLines, line)
	}

	if len(t.filtered) == 0 {
		rowLines = []string{styles.Muted.Render("  No resources found")}
	}

	content := title + filterBar + "\n" + header + "\n" + strings.Join(rowLines, "\n")
	return border.Width(innerW).Height(innerH).Render(content)
}

func buildHeader(desc k8sres.ResourceDescriptor, width int) string {
	cols := desc.Columns
	var parts []string
	for _, c := range cols {
		w := c.Width
		if w > width/len(cols) {
			w = width / len(cols)
		}
		parts = append(parts, padOrTrunc(c.Header, w))
	}
	line := strings.Join(parts, " ")
	return styles.TableHeader.Render(line)
}

func buildRow(row k8sres.ResourceRow, desc k8sres.ResourceDescriptor, width int, selected, cursor bool) string {
	prefix := "  "
	if selected {
		prefix = "✓ "
	}

	cols := desc.Columns
	var values []string
	if len(row.Values) > 0 {
		values = row.Values
	} else {
		values = append([]string{row.Name}, append([]string{row.Status, row.Age}, row.Extra...)...)
	}

	var parts []string
	for i, c := range cols {
		w := c.Width
		if w > width/len(cols) {
			w = width / len(cols)
		}
		val := ""
		if i < len(values) {
			val = values[i]
		}
		if row.Status != "" && val == row.Status {
			parts = append(parts, styles.StatusStyle(row.Status).Render(padOrTrunc(val, w)))
		} else {
			parts = append(parts, padOrTrunc(val, w))
		}
	}
	line := prefix + strings.Join(parts, " ")

	if cursor {
		return lipgloss.NewStyle().
			Background(lipgloss.Color("#1E3A5F")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Width(width).
			Render(line)
	}
	return lipgloss.NewStyle().Width(width).Render(line)
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func padOrTrunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	visW := lipgloss.Width(s)
	if visW > w {
		// Strip ANSI codes before slicing to avoid cutting mid-escape-sequence.
		plain := ansiEscape.ReplaceAllString(s, "")
		if len(plain) > w {
			return plain[:w-1] + "…"
		}
		return plain + strings.Repeat(" ", w-len(plain))
	}
	return s + strings.Repeat(" ", w-visW)
}

// PopulateRows converts raw k8s objects into ResourceRows for the given kind.
func (t ResourceTable) WithRows(rows []k8sres.ResourceRow) ResourceTable {
	t.rows = rows
	sort.Slice(t.rows, func(i, j int) bool {
		if !t.rows[i].SortByTime.IsZero() {
			return t.rows[i].SortByTime.After(t.rows[j].SortByTime)
		}
		return t.rows[i].Name < t.rows[j].Name
	})
	// Reapply filter
	if t.filter != "" {
		t.filterInput = t.filter
		t.applyFilter()
	} else {
		t.filtered = t.rows
	}
	if t.cursor >= len(t.filtered) {
		t.cursor = max(0, len(t.filtered)-1)
	}
	return t
}

// BuildPodRows converts pod list to resource rows.
// Columns: NAME, READY, STATUS, RESTARTS, AGE, CPU, %CPU/R, %CPU/L, MEM, %MEM/R, %MEM/L
func BuildPodRows(pods []*corev1.Pod, metricsData k8sres.MetricsUpdatedMsg) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(pods))
	for _, p := range pods {
		ready := 0
		total := len(p.Spec.Containers)
		restarts := 0
		for _, cs := range p.Status.ContainerStatuses {
			if cs.Ready {
				ready++
			}
			restarts += int(cs.RestartCount)
		}
		status := string(p.Status.Phase)
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				status = cs.State.Waiting.Reason
				break
			}
		}
		if p.DeletionTimestamp != nil {
			status = "Terminating"
		}
		age := k8sres.AgeString(p.CreationTimestamp)

		cpuStr, cpuRStr, cpuLStr := "n/a", "~", "~"
		memStr, memRStr, memLStr := "n/a", "~", "~"
		if rm := metricsData.Pods[p.Namespace+"/"+p.Name]; rm != nil {
			cpuM := int64(rm.CPULatest)
			memMi := int64(rm.MEMLatest) / (1024 * 1024)
			cpuStr = fmt.Sprintf("%d", cpuM)
			memStr = fmt.Sprintf("%d", memMi)
			cpuReqM, cpuLimM, memReqB, memLimB := PodResourceTotals(p)
			cpuRStr = fmtPctColored(cpuM, cpuReqM)
			cpuLStr = fmtPctColored(cpuM, cpuLimM)
			memRStr = fmtPctColored(int64(rm.MEMLatest), memReqB)
			memLStr = fmtPctColored(int64(rm.MEMLatest), memLimB)
		}

		rows = append(rows, k8sres.ResourceRow{
			Name:      p.Name,
			Namespace: p.Namespace,
			Status:    status,
			Values: []string{
				p.Name, fmt.Sprintf("%d/%d", ready, total), status, fmt.Sprintf("%d", restarts), age,
				cpuStr, cpuRStr, cpuLStr, memStr, memRStr, memLStr,
			},
			Raw: p,
		})
	}
	return rows
}

func PodResourceTotals(p *corev1.Pod) (cpuReqM, cpuLimM, memReqB, memLimB int64) {
	for _, c := range p.Spec.Containers {
		if q, ok := c.Resources.Requests[corev1.ResourceCPU]; ok {
			cpuReqM += q.MilliValue()
		}
		if q, ok := c.Resources.Limits[corev1.ResourceCPU]; ok {
			cpuLimM += q.MilliValue()
		}
		if q, ok := c.Resources.Requests[corev1.ResourceMemory]; ok {
			memReqB += q.Value()
		}
		if q, ok := c.Resources.Limits[corev1.ResourceMemory]; ok {
			memLimB += q.Value()
		}
	}
	return
}

func fmtPct(num, denom int64) string {
	if denom == 0 {
		return "~"
	}
	return fmt.Sprintf("%d", num*100/denom)
}

func fmtPctColored(num, denom int64) string {
	if denom == 0 {
		return "~"
	}
	pct := num * 100 / denom
	s := fmt.Sprintf("%d", pct)
	switch {
	case pct >= 90:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#F44336")).Render(s)
	case pct >= 70:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFC107")).Render(s)
	default:
		return s
	}
}

// Columns: NAME, READY, UP-TO-DATE, AVAILABLE, AGE
func BuildDeploymentRows(deps []*appsv1.Deployment) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(deps))
	for _, d := range deps {
		ready := d.Status.ReadyReplicas
		desired := *d.Spec.Replicas
		age := k8sres.AgeString(d.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      d.Name,
			Namespace: d.Namespace,
			Status:    deployStatus(d),
			Values: []string{
				d.Name,
				fmt.Sprintf("%d/%d", ready, desired),
				fmt.Sprintf("%d", d.Status.UpdatedReplicas),
				fmt.Sprintf("%d", d.Status.AvailableReplicas),
				age,
			},
			Raw: d,
		})
	}
	return rows
}

// Columns: NAME, READY, AGE
func BuildStatefulSetRows(sets []*appsv1.StatefulSet) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(sets))
	for _, s := range sets {
		ready := fmt.Sprintf("%d/%d", s.Status.ReadyReplicas, *s.Spec.Replicas)
		age := k8sres.AgeString(s.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      s.Name,
			Namespace: s.Namespace,
			Values:    []string{s.Name, ready, age},
			Raw:       s,
		})
	}
	return rows
}

// Columns: NAME, DESIRED, READY, UP-TO-DATE, AGE
func BuildDaemonSetRows(sets []*appsv1.DaemonSet) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(sets))
	for _, d := range sets {
		age := k8sres.AgeString(d.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      d.Name,
			Namespace: d.Namespace,
			Values: []string{
				d.Name,
				fmt.Sprintf("%d", d.Status.DesiredNumberScheduled),
				fmt.Sprintf("%d", d.Status.NumberReady),
				fmt.Sprintf("%d", d.Status.UpdatedNumberScheduled),
				age,
			},
			Raw: d,
		})
	}
	return rows
}

// Columns: NAME, COMPLETIONS, DURATION, AGE
func BuildJobRows(jobs []*batchv1.Job) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(jobs))
	for _, j := range jobs {
		completions := "0"
		if j.Spec.Completions != nil {
			completions = fmt.Sprintf("%d/%d", j.Status.Succeeded, *j.Spec.Completions)
		}
		duration := ""
		if j.Status.CompletionTime != nil && !j.Status.StartTime.IsZero() {
			d := j.Status.CompletionTime.Sub(j.Status.StartTime.Time)
			duration = fmt.Sprintf("%.0fs", d.Seconds())
		}
		age := k8sres.AgeString(j.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      j.Name,
			Namespace: j.Namespace,
			Status:    jobStatus(j),
			Values:    []string{j.Name, completions, duration, age},
			Raw:       j,
		})
	}
	return rows
}

// Columns: NAME, SCHEDULE, LAST SCHEDULE, AGE
func BuildCronJobRows(cjs []*batchv1.CronJob) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(cjs))
	for _, c := range cjs {
		lastSchedule := "Never"
		if c.Status.LastScheduleTime != nil {
			lastSchedule = k8sres.AgeString(*c.Status.LastScheduleTime)
		}
		age := k8sres.AgeString(c.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      c.Name,
			Namespace: c.Namespace,
			Values:    []string{c.Name, c.Spec.Schedule, lastSchedule, age},
			Raw:       c,
		})
	}
	return rows
}

// Columns: NAME, TYPE, CLUSTER-IP, PORT(S), AGE
func BuildServiceRows(svcs []*corev1.Service) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(svcs))
	for _, s := range svcs {
		clusterIP := s.Spec.ClusterIP
		ports := ""
		for i, p := range s.Spec.Ports {
			if i > 0 {
				ports += ","
			}
			ports += fmt.Sprintf("%d/%s", p.Port, p.Protocol)
		}
		age := k8sres.AgeString(s.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      s.Name,
			Namespace: s.Namespace,
			Values:    []string{s.Name, string(s.Spec.Type), clusterIP, ports, age},
			Raw:       s,
		})
	}
	return rows
}

// Columns: NAME, STATUS, ROLES, VERSION, AGE
func BuildNodeRows(nodes []*corev1.Node) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(nodes))
	for _, n := range nodes {
		status := "NotReady"
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
				status = "Ready"
			}
		}
		if n.Spec.Unschedulable {
			status = "SchedulingDisabled"
		}
		roles := nodeRoles(n)
		version := n.Status.NodeInfo.KubeletVersion
		age := k8sres.AgeString(n.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:   n.Name,
			Status: status,
			Values: []string{n.Name, status, roles, version, age},
			Raw:    n,
		})
	}
	return rows
}

// Columns: NAME, ADDRESSES, RULES, AGE
func BuildIngressRows(ings []*networkingv1.Ingress) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(ings))
	for _, ing := range ings {
		var addrs []string
		for _, lb := range ing.Status.LoadBalancer.Ingress {
			if lb.IP != "" {
				addrs = append(addrs, lb.IP)
			} else if lb.Hostname != "" {
				addrs = append(addrs, lb.Hostname)
			}
		}
		addrStr := strings.Join(addrs, ",")
		if addrStr == "" {
			addrStr = "<pending>"
		}

		var rulePairs []string
		for _, rule := range ing.Spec.Rules {
			host := rule.Host
			if host == "" {
				host = "*"
			}
			if rule.HTTP != nil {
				for _, path := range rule.HTTP.Paths {
					svc := path.Backend.Service
					if svc != nil {
						rulePairs = append(rulePairs,
							fmt.Sprintf("%s%s → %s:%d", host, path.Path, svc.Name, svc.Port.Number))
					}
				}
			}
		}
		rulesStr := "<none>"
		if len(rulePairs) == 1 {
			rulesStr = rulePairs[0]
		} else if len(rulePairs) > 1 {
			rulesStr = fmt.Sprintf("%s  +%d more", rulePairs[0], len(rulePairs)-1)
		}

		age := k8sres.AgeString(ing.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      ing.Name,
			Namespace: ing.Namespace,
			Values:    []string{ing.Name, addrStr, rulesStr, age},
			Raw:       ing,
		})
	}
	return rows
}

// Columns: NAME, DATA, AGE
func BuildConfigMapRows(cms []*corev1.ConfigMap) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(cms))
	for _, c := range cms {
		age := k8sres.AgeString(c.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      c.Name,
			Namespace: c.Namespace,
			Values:    []string{c.Name, fmt.Sprintf("%d", len(c.Data)), age},
			Raw:       c,
		})
	}
	return rows
}

// Columns: NAME, TYPE, DATA, AGE
func BuildSecretRows(secrets []*corev1.Secret) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(secrets))
	for _, s := range secrets {
		age := k8sres.AgeString(s.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      s.Name,
			Namespace: s.Namespace,
			Values:    []string{s.Name, string(s.Type), fmt.Sprintf("%d", len(s.Data)), age},
			Raw:       s,
		})
	}
	return rows
}

// Columns: NAME, STATUS, VOLUME, CAPACITY, AGE
func BuildPVCRows(pvcs []*corev1.PersistentVolumeClaim) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(pvcs))
	for _, p := range pvcs {
		cap := ""
		if storage, ok := p.Status.Capacity[corev1.ResourceStorage]; ok {
			cap = storage.String()
		}
		phase := string(p.Status.Phase)
		age := k8sres.AgeString(p.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      p.Name,
			Namespace: p.Namespace,
			Status:    phase,
			Values:    []string{p.Name, phase, p.Spec.VolumeName, cap, age},
			Raw:       p,
		})
	}
	return rows
}

// Columns: NAME, CAPACITY, ACCESS MODES, STATUS, AGE
func BuildPVRows(pvs []*corev1.PersistentVolume) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(pvs))
	for _, p := range pvs {
		cap := ""
		if storage, ok := p.Spec.Capacity[corev1.ResourceStorage]; ok {
			cap = storage.String()
		}
		modes := make([]string, 0, len(p.Spec.AccessModes))
		for _, m := range p.Spec.AccessModes {
			modes = append(modes, string(m))
		}
		phase := string(p.Status.Phase)
		age := k8sres.AgeString(p.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:   p.Name,
			Status: phase,
			Values: []string{p.Name, cap, strings.Join(modes, ","), phase, age},
			Raw:    p,
		})
	}
	return rows
}

// Columns: NAME, DESIRED, CURRENT, READY, AGE
func BuildReplicaSetRows(sets []*appsv1.ReplicaSet) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(sets))
	for _, s := range sets {
		desired := int32(0)
		if s.Spec.Replicas != nil {
			desired = *s.Spec.Replicas
		}
		age := k8sres.AgeString(s.CreationTimestamp)
		rows = append(rows, k8sres.ResourceRow{
			Name:      s.Name,
			Namespace: s.Namespace,
			Values: []string{
				s.Name,
				fmt.Sprintf("%d", desired),
				fmt.Sprintf("%d", s.Status.Replicas),
				fmt.Sprintf("%d", s.Status.ReadyReplicas),
				age,
			},
			Raw: s,
		})
	}
	return rows
}

// Columns: LAST SEEN, COUNT, AGE, TYPE, REASON, OBJECT, MESSAGE
func BuildEventRows(evts []*corev1.Event) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(evts))
	for _, e := range evts {
		rows = append(rows, k8sres.ResourceRow{
			Name:       e.InvolvedObject.Name,
			Namespace:  e.Namespace,
			Status:     e.Type,
			SortByTime: e.LastTimestamp.Time,
			Values: []string{
				k8sres.AgeString(e.LastTimestamp),
				strconv.Itoa(int(e.Count)),
				k8sres.AgeString(e.FirstTimestamp),
				e.Type,
				e.Reason,
				e.InvolvedObject.Name,
				e.Message,
			},
			Raw: e,
		})
	}
	return rows
}

// Columns: NAME, CHART, VERSION, READY, SUSPENDED, AGE
func BuildHelmReleaseRows(releases []*unstructured.Unstructured) []k8sres.ResourceRow {
	rows := make([]k8sres.ResourceRow, 0, len(releases))
	for _, u := range releases {
		spec, _ := u.Object["spec"].(map[string]interface{})
		chartSpec, _ := func() (map[string]interface{}, bool) {
			if spec == nil {
				return nil, false
			}
			chart, _ := spec["chart"].(map[string]interface{})
			if chart == nil {
				return nil, false
			}
			cs, ok := chart["spec"].(map[string]interface{})
			return cs, ok
		}()

		chart := "-"
		version := "-"
		if chartSpec != nil {
			if v, ok := chartSpec["chart"].(string); ok && v != "" {
				chart = v
			}
			if v, ok := chartSpec["version"].(string); ok && v != "" {
				version = v
			}
		}

		suspended := false
		if spec != nil {
			if v, ok := spec["suspend"].(bool); ok {
				suspended = v
			}
		}

		ready := "False"
		statusMsg := "-"
		status, _ := u.Object["status"].(map[string]interface{})
		if status != nil {
			if conditions, ok := status["conditions"].([]interface{}); ok {
				for _, c := range conditions {
					cm, _ := c.(map[string]interface{})
					if cm["type"] == "Ready" {
						if s, ok := cm["status"].(string); ok {
							ready = s
						}
						if msg, ok := cm["message"].(string); ok && msg != "" {
							statusMsg = msg
						}
						break
					}
				}
			}
		}

		statusKey := "NotReady"
		if suspended {
			statusKey = "Suspended"
		} else if ready == "True" {
			statusKey = "Ready"
		}

		suspendedStr := "False"
		if suspended {
			suspendedStr = "True"
		}

		age := k8sres.AgeString(u.GetCreationTimestamp())
		rows = append(rows, k8sres.ResourceRow{
			Name:      u.GetName(),
			Namespace: u.GetNamespace(),
			Status:    statusKey,
			Values:    []string{u.GetName(), chart, version, ready, statusMsg, suspendedStr, age},
			Raw:       u,
		})
	}
	return rows
}

// helpers

func deployStatus(d *appsv1.Deployment) string {
	if d.Status.AvailableReplicas == *d.Spec.Replicas {
		return "Running"
	}
	return "Pending"
}

func jobStatus(j *batchv1.Job) string {
	if j.Status.Succeeded > 0 {
		return "Succeeded"
	}
	if j.Status.Failed > 0 {
		return "Failed"
	}
	return "Running"
}

func nodeRoles(n *corev1.Node) string {
	roles := []string{}
	for k := range n.Labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			roles = append(roles, strings.TrimPrefix(k, "node-role.kubernetes.io/"))
		}
	}
	if len(roles) == 0 {
		return "<none>"
	}
	return strings.Join(roles, ",")
}

