package kinds

import (
	"context"
	"fmt"
	"strings"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// node is cluster-scoped. It implements MetricsSupporter so the help bar
// shows the "m" hint and the metrics panel can open. List renders status /
// roles / kubelet version from the informer cache.
type node struct{}

func (node) Meta() Meta {
	return Meta{
		Kind:       "Node",
		Plural:     "nodes",
		Aliases:    []string{"no"},
		Namespaced: false,
		GVR:        k8s.NodeGVR,
	}
}

func (node) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: nodeName},
		{Header: "STATUS", Width: 14, Render: nodeStatusCell},
		{Header: "ROLES", Width: 20, Render: nodeRolesCell},
		{Header: "VERSION", Width: 16, Render: nodeVersion},
		{Header: "AGE", Width: 10, Render: nodeAge},
	}
}

func (n node) List(c Context) ([]k8s.ResourceRow, error) { return listVia(n, c) }

func (node) RowStatus(o runtime.Object) string { return nodeStatusCell(o, k8s.RowContext{}) }

func nodeName(o runtime.Object, _ k8s.RowContext) string { return o.(*corev1.Node).Name }

func nodeStatus(nd *corev1.Node) string {
	if nd.Spec.Unschedulable {
		return "SchedulingDisabled"
	}
	for _, cd := range nd.Status.Conditions {
		if cd.Type == corev1.NodeReady && cd.Status == corev1.ConditionTrue {
			return "Ready"
		}
	}
	return "NotReady"
}

func nodeStatusCell(o runtime.Object, _ k8s.RowContext) string {
	return nodeStatus(o.(*corev1.Node))
}

func nodeRolesCell(o runtime.Object, _ k8s.RowContext) string {
	return nodeRoles(o.(*corev1.Node))
}

func nodeVersion(o runtime.Object, _ k8s.RowContext) string {
	return o.(*corev1.Node).Status.NodeInfo.KubeletVersion
}

func nodeAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*corev1.Node).CreationTimestamp)
}

func (node) Fetch(ctx context.Context, c Context, _, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("node.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
}

// MetricsKey marks Node as openable in the metrics panel. Cluster-scoped, so
// the namespace component is empty.
func (node) MetricsKey(_, name string) string { return name }

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

func init() { register(node{}) }
