package kinds

import (
	"context"
	"fmt"
	"strings"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// node is cluster-scoped. SupportsMetrics is wired via MetricsSupporter so
// the help bar shows the "m" hint and the metrics panel can open. List
// renders status / roles / kubelet version from the informer cache.
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
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "STATUS", Width: 14},
		{Header: "ROLES", Width: 20},
		{Header: "VERSION", Width: 16},
		{Header: "AGE", Width: 10},
	}
}

func (n node) List(c Context) ([]Row, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("node.List: no Lister")
	}
	objs, err := c.Lister.List(c.Ctx, n.Meta().GVR, "")
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(objs))
	for _, o := range objs {
		nd, ok := o.(*corev1.Node)
		if !ok {
			continue
		}
		status := "NotReady"
		for _, cd := range nd.Status.Conditions {
			if cd.Type == corev1.NodeReady && cd.Status == corev1.ConditionTrue {
				status = "Ready"
			}
		}
		if nd.Spec.Unschedulable {
			status = "SchedulingDisabled"
		}
		rows = append(rows, Row{
			Name:   nd.Name,
			Status: status,
			Values: []string{nd.Name, status, nodeRoles(nd), nd.Status.NodeInfo.KubeletVersion, k8s.AgeString(nd.CreationTimestamp)},
			Raw:    nd,
		})
	}
	return rows, nil
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
