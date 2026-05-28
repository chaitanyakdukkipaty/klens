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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// pod exercises every capability interface end-to-end: Logger, Attacher,
// Deleter, PortForwarder, Applier, MetricsSupporter, XRayer (defined in
// pod_xray.go). It does NOT implement Scaler (pods don't scale). Every
// action site type-asserts against these capability interfaces directly
// — there is no Supports* indirection.
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
		{Header: "NAME", Width: 40, Flex: true, Render: podName},
		{Header: "PF", Width: 3, Render: podPF},
		{Header: "READY", Width: 6, Render: podReady},
		{Header: "STATUS", Width: 15, Render: podStatusCell},
		{Header: "RESTARTS", Width: 9, Render: podRestarts},
		{Header: "AGE", Width: 6, Render: podAge},
		{Header: "CPU", Width: 6, Render: podCPU},
		{Header: "%CPU/R", Width: 7, Render: podCPUPctRequest},
		{Header: "%CPU/L", Width: 7, Render: podCPUPctLimit},
		{Header: "MEM", Width: 7, Render: podMEM},
		{Header: "%MEM/R", Width: 7, Render: podMEMPctRequest},
		{Header: "%MEM/L", Width: 7, Render: podMEMPctLimit},
	}
}

func (p pod) List(c Context) ([]k8s.ResourceRow, error) { return listVia(p, c) }

// RowStatus drives the colored STATUS column — must equal podStatusCell's
// output so the table's color key matches the rendered cell.
func (pod) RowStatus(o runtime.Object) string { return podStatusCell(o, k8s.RowContext{}) }

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
	return c.Clientset.CoreV1().Pods(ns).Delete(c.Ctx, name, metav1.DeleteOptions{})
}

// Kill force-deletes a pod with grace=0, skipping
// terminationGracePeriodSeconds. Backs ctrl+k in the UI.
func (pod) Kill(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("pod.Kill: no Clientset")
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
// Pod as openable in the metrics panel (interface satisfaction is checked
// in setStatusBarKind via `_, ok := kind.(MetricsSupporter)`).
func (pod) MetricsKey(ns, name string) string { return ns + "/" + name }

// Attach is a placeholder until plan 01 step 6 wires the *rest.Config
// through Context. The legacy model.actionAttach path drives the live
// session today; this method exists so Pod satisfies Attacher (and so
// the "a" UI hint shows up). When step 6 lands, model.go calls Attach
// instead of k8s.AttachCmd directly.
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

// podReadyCounts returns (ready, total, restarts) over the pod's container
// statuses. Shared by the READY and RESTARTS column renderers.
func podReadyCounts(p *corev1.Pod) (ready, total, restarts int) {
	total = len(p.Spec.Containers)
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
		restarts += int(cs.RestartCount)
	}
	return
}

// podStatus computes the STATUS column text: Phase by default, the first
// waiting reason if any container is waiting, "Terminating" if the pod is
// being deleted.
func podStatus(p *corev1.Pod) string {
	if p.DeletionTimestamp != nil {
		return "Terminating"
	}
	status := string(p.Status.Phase)
	for _, cs := range p.Status.ContainerStatuses {
		if cs.State.Waiting != nil {
			return cs.State.Waiting.Reason
		}
	}
	return status
}

func podName(o runtime.Object, _ k8s.RowContext) string { return o.(*corev1.Pod).Name }

func podPF(o runtime.Object, c k8s.RowContext) string {
	p := o.(*corev1.Pod)
	return podPFMarker(c.PortForwardActive, p.Namespace, p.Name)
}

func podReady(o runtime.Object, _ k8s.RowContext) string {
	ready, total, _ := podReadyCounts(o.(*corev1.Pod))
	return fmt.Sprintf("%d/%d", ready, total)
}

func podStatusCell(o runtime.Object, _ k8s.RowContext) string {
	return podStatus(o.(*corev1.Pod))
}

func podRestarts(o runtime.Object, _ k8s.RowContext) string {
	_, _, restarts := podReadyCounts(o.(*corev1.Pod))
	return fmt.Sprintf("%d", restarts)
}

func podAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*corev1.Pod).CreationTimestamp)
}

// podMetricsRow returns the metrics-server snapshot for the pod, or nil
// when metrics aren't available (no server, no sample yet).
func podMetricsRow(p *corev1.Pod, c k8s.RowContext) *k8s.ResourceMetrics {
	if c.Metrics.Pods == nil {
		return nil
	}
	return c.Metrics.Pods[p.Namespace+"/"+p.Name]
}

func podCPU(o runtime.Object, c k8s.RowContext) string {
	rm := podMetricsRow(o.(*corev1.Pod), c)
	if rm == nil {
		return "n/a"
	}
	return fmt.Sprintf("%d", int64(rm.CPULatest))
}

func podMEM(o runtime.Object, c k8s.RowContext) string {
	rm := podMetricsRow(o.(*corev1.Pod), c)
	if rm == nil {
		return "n/a"
	}
	return fmt.Sprintf("%d", int64(rm.MEMLatest)/(1024*1024))
}

func podCPUPctRequest(o runtime.Object, c k8s.RowContext) string {
	p := o.(*corev1.Pod)
	rm := podMetricsRow(p, c)
	if rm == nil {
		return "~"
	}
	cpuReqM, _, _, _ := podResourceTotals(p)
	return podPctColored(int64(rm.CPULatest), cpuReqM)
}

func podCPUPctLimit(o runtime.Object, c k8s.RowContext) string {
	p := o.(*corev1.Pod)
	rm := podMetricsRow(p, c)
	if rm == nil {
		return "~"
	}
	_, cpuLimM, _, _ := podResourceTotals(p)
	return podPctColored(int64(rm.CPULatest), cpuLimM)
}

func podMEMPctRequest(o runtime.Object, c k8s.RowContext) string {
	p := o.(*corev1.Pod)
	rm := podMetricsRow(p, c)
	if rm == nil {
		return "~"
	}
	_, _, memReqB, _ := podResourceTotals(p)
	return podPctColored(int64(rm.MEMLatest), memReqB)
}

func podMEMPctLimit(o runtime.Object, c k8s.RowContext) string {
	p := o.(*corev1.Pod)
	rm := podMetricsRow(p, c)
	if rm == nil {
		return "~"
	}
	_, _, _, memLimB := podResourceTotals(p)
	return podPctColored(int64(rm.MEMLatest), memLimB)
}

func init() { register(pod{}) }
