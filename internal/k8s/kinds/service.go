package kinds

import (
	"context"
	"fmt"
	"strings"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// service implements Deleter + Applier + XRayer.
type service struct{}

func (service) Meta() Meta {
	return Meta{
		Kind:       "Service",
		Plural:     "services",
		Aliases:    []string{"svc"},
		Namespaced: true,
		GVR:        k8s.ServiceGVR,
	}
}

func (service) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: serviceName},
		{Header: "TYPE", Width: 14, Render: serviceType},
		{Header: "CLUSTER-IP", Width: 18, Render: serviceClusterIP},
		{Header: "PORT(S)", Width: 20, Render: servicePorts},
		{Header: "AGE", Width: 10, Render: serviceAge},
	}
}

func (s service) List(c Context) ([]k8s.ResourceRow, error) { return listVia(s, c) }

func serviceName(o runtime.Object, _ k8s.RowContext) string { return o.(*corev1.Service).Name }

func serviceType(o runtime.Object, _ k8s.RowContext) string {
	return string(o.(*corev1.Service).Spec.Type)
}

func serviceClusterIP(o runtime.Object, _ k8s.RowContext) string {
	return o.(*corev1.Service).Spec.ClusterIP
}

func servicePorts(o runtime.Object, _ k8s.RowContext) string {
	sv := o.(*corev1.Service)
	parts := make([]string, 0, len(sv.Spec.Ports))
	for _, p := range sv.Spec.Ports {
		parts = append(parts, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
	}
	return strings.Join(parts, ",")
}

func serviceAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*corev1.Service).CreationTimestamp)
}

func (service) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("service.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
}

func (service) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("service.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.CoreV1().Services(ns).Delete(c.Ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
}

func (service) Apply(c Context, ns, name string, body []byte) error {
	if c.Clientset == nil {
		return fmt.Errorf("service.Apply: no Clientset")
	}
	_, err := c.Clientset.CoreV1().Services(ns).Patch(
		c.Ctx, name, types.MergePatchType, body,
		metav1.PatchOptions{FieldManager: "klens"})
	return err
}

func (s service) XRay(c Context, ns, name string) (*k8s.TreeNode, error) {
	svcs, err := listTyped[*corev1.Service](c, s.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	var svc *corev1.Service
	for _, x := range svcs {
		if x.Name == name {
			svc = x
			break
		}
	}
	if svc == nil {
		return nil, nil
	}
	root := &k8s.TreeNode{Kind: "Service", Name: svc.Name, Status: string(svc.Spec.Type)}
	if svc.Spec.Selector == nil {
		return root, nil
	}
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return root, err
	}
	for _, p := range pods {
		if matchesLabelSelector(p.Labels, svc.Spec.Selector) {
			root.Children = append(root.Children, podTreeNode(p))
		}
	}
	return root, nil
}

func init() { register(service{}) }
