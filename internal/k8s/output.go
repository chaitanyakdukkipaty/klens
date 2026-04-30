package k8s

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StripMeta removes noisy fields before JSON output: ManagedFields and the
// last-applied-configuration annotation. Mutates the object in place.
func StripMeta(obj metav1.Object) {
	obj.SetManagedFields(nil)
	annos := obj.GetAnnotations()
	if _, ok := annos["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		delete(annos, "kubectl.kubernetes.io/last-applied-configuration")
		if len(annos) == 0 {
			obj.SetAnnotations(nil)
		} else {
			obj.SetAnnotations(annos)
		}
	}
}

// MarshalPretty returns indented JSON for any value.
func MarshalPretty(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// PrintPodsTable writes a plain-text pod table to w (no lipgloss styling).
func PrintPodsTable(w io.Writer, pods []corev1.Pod) {
	fmt.Fprintf(w, "%-50s %-20s %-8s %-16s %s\n", "NAME", "NAMESPACE", "READY", "STATUS", "AGE")
	for _, p := range pods {
		fmt.Fprintf(w, "%-50s %-20s %-8s %-16s %s\n",
			p.Name,
			p.Namespace,
			podReadyCount(p),
			string(p.Status.Phase),
			fmtAge(p.CreationTimestamp.Time),
		)
	}
}

// PrintDeploymentsTable writes a plain-text deployment table to w.
func PrintDeploymentsTable(w io.Writer, deps []appsv1.Deployment) {
	fmt.Fprintf(w, "%-50s %-20s %-8s %-12s %-12s %s\n", "NAME", "NAMESPACE", "READY", "UP-TO-DATE", "AVAILABLE", "AGE")
	for _, d := range deps {
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		ready := fmt.Sprintf("%d/%d", d.Status.ReadyReplicas, desired)
		fmt.Fprintf(w, "%-50s %-20s %-8s %-12d %-12d %s\n",
			d.Name,
			d.Namespace,
			ready,
			d.Status.UpdatedReplicas,
			d.Status.AvailableReplicas,
			fmtAge(d.CreationTimestamp.Time),
		)
	}
}

// PrintEventsTable writes a plain-text events table to w.
func PrintEventsTable(w io.Writer, events []corev1.Event) {
	fmt.Fprintf(w, "%-8s %-20s %-40s %-70s %s\n", "TYPE", "REASON", "OBJECT", "MESSAGE", "AGE")
	for _, ev := range events {
		obj := fmt.Sprintf("%s/%s", ev.InvolvedObject.Kind, ev.InvolvedObject.Name)
		msg := ev.Message
		if len(msg) > 68 {
			msg = msg[:65] + "..."
		}
		ts := ev.LastTimestamp.Time
		if ts.IsZero() {
			ts = ev.EventTime.Time
		}
		fmt.Fprintf(w, "%-8s %-20s %-40s %-70s %s\n",
			ev.Type, ev.Reason, obj, msg, fmtAge(ts),
		)
	}
}

// PrintNodesTable writes a plain-text node table to w.
func PrintNodesTable(w io.Writer, nodes []corev1.Node) {
	fmt.Fprintf(w, "%-50s %-10s %-30s %-10s %s\n", "NAME", "STATUS", "ROLES", "AGE", "VERSION")
	for _, n := range nodes {
		fmt.Fprintf(w, "%-50s %-10s %-30s %-10s %s\n",
			n.Name,
			nodeStatus(n),
			nodeRoles(n),
			fmtAge(n.CreationTimestamp.Time),
			n.Status.NodeInfo.KubeletVersion,
		)
	}
}

func nodeStatus(n corev1.Node) string {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			if c.Status == corev1.ConditionTrue {
				return "Ready"
			}
			return "NotReady"
		}
	}
	return "Unknown"
}

func nodeRoles(n corev1.Node) string {
	var roles []string
	for k := range n.Labels {
		if strings.HasPrefix(k, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(k, "node-role.kubernetes.io/")
			if role != "" {
				roles = append(roles, role)
			}
		}
	}
	if len(roles) == 0 {
		return "<none>"
	}
	sort.Strings(roles)
	return strings.Join(roles, ",")
}

// PrintStatefulSetsTable writes a plain-text statefulset table to w.
func PrintStatefulSetsTable(w io.Writer, sets []appsv1.StatefulSet) {
	fmt.Fprintf(w, "%-50s %-20s %-8s %s\n", "NAME", "NAMESPACE", "READY", "AGE")
	for _, s := range sets {
		desired := int32(1)
		if s.Spec.Replicas != nil {
			desired = *s.Spec.Replicas
		}
		fmt.Fprintf(w, "%-50s %-20s %-8s %s\n",
			s.Name, s.Namespace, fmt.Sprintf("%d/%d", s.Status.ReadyReplicas, desired),
			fmtAge(s.CreationTimestamp.Time),
		)
	}
}

// PrintPVCsTable writes a plain-text PVC table to w.
func PrintPVCsTable(w io.Writer, pvcs []corev1.PersistentVolumeClaim) {
	fmt.Fprintf(w, "%-50s %-20s %-10s %-12s %s\n", "NAME", "NAMESPACE", "STATUS", "CAPACITY", "AGE")
	for _, p := range pvcs {
		cap := ""
		if q, ok := p.Spec.Resources.Requests[corev1.ResourceStorage]; ok {
			cap = q.String()
		}
		fmt.Fprintf(w, "%-50s %-20s %-10s %-12s %s\n",
			p.Name, p.Namespace, string(p.Status.Phase), cap,
			fmtAge(p.CreationTimestamp.Time),
		)
	}
}

// PrintLogNDJSON writes one log line as NDJSON: {"pod":"...","container":"...","ts":"...","line":"..."}\n
func PrintLogNDJSON(w io.Writer, pod, container, line string) {
	type entry struct {
		Pod       string `json:"pod"`
		Container string `json:"container"`
		Ts        string `json:"ts"`
		Line      string `json:"line"`
	}
	b, _ := json.Marshal(entry{
		Pod:       pod,
		Container: container,
		Ts:        time.Now().UTC().Format(time.RFC3339),
		Line:      line,
	})
	fmt.Fprintf(w, "%s\n", b)
}

func podReadyCount(p corev1.Pod) string {
	total := len(p.Spec.Containers)
	ready := 0
	for _, cs := range p.Status.ContainerStatuses {
		if cs.Ready {
			ready++
		}
	}
	return fmt.Sprintf("%d/%d", ready, total)
}

// FmtAge formats a duration since t as a human-readable string (e.g. 5m, 3h, 2d).
func FmtAge(t time.Time) string {
	return fmtAge(t)
}

func fmtAge(t time.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
