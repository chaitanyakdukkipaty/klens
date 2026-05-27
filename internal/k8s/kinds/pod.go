package kinds

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// pod exercises every capability interface end-to-end: Logger, Attacher,
// Deleter, PortForwarder, Applier. It does NOT implement Scaler (pods don't
// scale) or Topologer (no per-Pod tree). The shim's Supports* derivation
// reads "Pod implements Applier" → SupportsYAML=true (via Fetch in any
// case), Deleter → SupportsDeletion=true, Logger → SupportsLogs=true,
// PortForwarder → SupportsPortForward=true, Attacher → SupportsAttach=true.
type pod struct{}

func (pod) Meta() Meta {
	return Meta{
		Kind:       "Pod",
		Plural:     "pods",
		Aliases:    []string{"po"},
		Namespaced: true,
		GVR:        k8s.PodGVR,
	}
}

func (pod) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "PF", Width: 3},
		{Header: "READY", Width: 6},
		{Header: "STATUS", Width: 15},
		{Header: "RESTARTS", Width: 9},
		{Header: "AGE", Width: 6},
		{Header: "CPU", Width: 6},
		{Header: "%CPU/R", Width: 7},
		{Header: "%CPU/L", Width: 7},
		{Header: "MEM", Width: 7},
		{Header: "%MEM/R", Width: 7},
		{Header: "%MEM/L", Width: 7},
	}
}

func (p pod) List(c Context) ([]Row, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("pod.List: no Lister")
	}
	objs, err := c.Lister.List(c.Ctx, p.Meta().GVR, c.Namespace)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(objs))
	for _, o := range objs {
		pp, ok := o.(*corev1.Pod)
		if !ok {
			continue
		}
		rows = append(rows, podRow(pp, c.Metrics, c.PFActive))
	}
	return rows, nil
}

func (pod) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("pod.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().Pods(ns).Get(ctx, name, metav1.GetOptions{})
}

func (pod) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("pod.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.CoreV1().Pods(ns).Delete(c.Ctx, name, metav1.DeleteOptions{
		GracePeriodSeconds: &grace,
	})
}

func (pod) Apply(c Context, ns, name string, body []byte) error {
	if c.Clientset == nil {
		return fmt.Errorf("pod.Apply: no Clientset")
	}
	_, err := c.Clientset.CoreV1().Pods(ns).Patch(
		c.Ctx, name, types.MergePatchType, body,
		metav1.PatchOptions{FieldManager: "klens"})
	return err
}

// LogTargets returns the single (ns, name) tuple to stream. Controllers
// (Deployment, etc.) fan out across pods; a Pod target is just itself.
func (pod) LogTargets(_ Context, ns, name string) ([]LogTarget, error) {
	return []LogTarget{{Namespace: ns, Pod: name}}, nil
}

// MetricsKey returns the "ns/name" key into MetricsUpdatedMsg.Pods. Marks
// Pod as openable in the metrics panel (SupportsMetrics=true via shim).
func (pod) MetricsKey(ns, name string) string { return ns + "/" + name }

// Attach is a placeholder until step 6 wires the *rest.Config through
// Context. The legacy model.actionAttach path drives the live session
// today; this method exists so Pod satisfies Attacher (the shim derives
// SupportsAttach from interface satisfaction). When step 6 lands,
// model.go calls Attach instead of k8s.AttachCmd directly.
func (pod) Attach(c Context, ns, name string) tea.Cmd {
	return func() tea.Msg {
		return k8s.AttachFinishedMsg{Pod: name, Err: fmt.Errorf("pod.Attach: not wired (use model.actionAttach until step 6)")}
	}
}

// ContainerPorts returns the pod's exposed container ports, sourced from
// the informer cache via the Lister so the dialog opens instantly without
// a fresh API call.
func (p pod) ContainerPorts(c Context, ns, name string) ([]ContainerPort, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("pod.ContainerPorts: no Lister")
	}
	objs, err := c.Lister.List(c.Ctx, p.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	for _, o := range objs {
		pp, ok := o.(*corev1.Pod)
		if !ok || pp.Name != name {
			continue
		}
		ports := []ContainerPort{}
		for _, cn := range pp.Spec.Containers {
			for _, port := range cn.Ports {
				ports = append(ports, ContainerPort{Name: port.Name, Port: port.ContainerPort})
			}
		}
		return ports, nil
	}
	return nil, fmt.Errorf("pod %s/%s not found in cache", ns, name)
}

// podResourceTotals duplicates panels.PodResourceTotals so kinds/pod.go is
// self-contained — calling out to panels for one helper would couple the
// kinds package to a UI helper that future kinds don't share. The body is
// identical; keep them in sync until step 6 deletes the panels copy.
func podResourceTotals(p *corev1.Pod) (cpuReqM, cpuLimM, memReqB, memLimB int64) {
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

var (
	podPFActiveStyle   = styles.Primary.Bold(true)
	podPFInactiveStyle = styles.Muted
	podPctFailedStyle  = lipgloss.NewStyle().Foreground(styles.ColorFailed)
	podPctPendingStyle = lipgloss.NewStyle().Foreground(styles.ColorPending)
)

func podPFMarker(active func(ns, name string) bool, ns, name string) string {
	if active != nil && active(ns, name) {
		return podPFActiveStyle.Render("Ⓟ")
	}
	return podPFInactiveStyle.Render("•")
}

func podPctColored(num, denom int64) string {
	if denom == 0 {
		return "~"
	}
	pct := num * 100 / denom
	s := fmt.Sprintf("%d", pct)
	switch {
	case pct >= 90:
		return podPctFailedStyle.Render(s)
	case pct >= 70:
		return podPctPendingStyle.Render(s)
	default:
		return s
	}
}

// podRow renders a single Pod into a Row. Mirrors the legacy
// panels.BuildPodRows logic so the table looks identical; the difference
// is ownership — the rendering now lives next to the Kind that produces
// it instead of in a generic resource_table file.
func podRow(p *corev1.Pod, metricsData k8s.MetricsUpdatedMsg, pfActive func(ns, name string) bool) Row {
	ready := 0
	total := len(p.Spec.Containers)
	restarts := 0
	status := string(p.Status.Phase)
	waitingFound := false
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
		restarts += int(cs.RestartCount)
		if !waitingFound && cs.State.Waiting != nil {
			status = cs.State.Waiting.Reason
			waitingFound = true
		}
	}
	if p.DeletionTimestamp != nil {
		status = "Terminating"
	}
	age := k8s.AgeString(p.CreationTimestamp)

	cpuStr, cpuRStr, cpuLStr := "n/a", "~", "~"
	memStr, memRStr, memLStr := "n/a", "~", "~"
	if rm := metricsData.Pods[p.Namespace+"/"+p.Name]; rm != nil {
		cpuM := int64(rm.CPULatest)
		memMi := int64(rm.MEMLatest) / (1024 * 1024)
		cpuStr = fmt.Sprintf("%d", cpuM)
		memStr = fmt.Sprintf("%d", memMi)
		cpuReqM, cpuLimM, memReqB, memLimB := podResourceTotals(p)
		cpuRStr = podPctColored(cpuM, cpuReqM)
		cpuLStr = podPctColored(cpuM, cpuLimM)
		memRStr = podPctColored(int64(rm.MEMLatest), memReqB)
		memLStr = podPctColored(int64(rm.MEMLatest), memLimB)
	}

	return Row{
		Name:      p.Name,
		Namespace: p.Namespace,
		Status:    status,
		Values: []string{
			p.Name, podPFMarker(pfActive, p.Namespace, p.Name), fmt.Sprintf("%d/%d", ready, total), status, fmt.Sprintf("%d", restarts), age,
			cpuStr, cpuRStr, cpuLStr, memStr, memRStr, memLStr,
		},
		Raw: p,
	}
}

func init() { register(pod{}) }
