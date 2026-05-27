package kinds

import (
	"context"
	"fmt"
	"strings"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ingress implements Deleter + Topologer.
type ingress struct{}

func (ingress) Meta() Meta {
	return Meta{
		Kind:       "Ingress",
		Plural:     "ingresses",
		Aliases:    []string{"ing"},
		Namespaced: true,
		GVR:        k8s.IngressGVR,
	}
}

func (ingress) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 35, Flex: true, Render: ingressName},
		{Header: "ADDRESSES", Width: 22, Render: ingressAddresses},
		{Header: "RULES", Width: 40, Render: ingressRules},
		{Header: "AGE", Width: 10, Render: ingressAge},
	}
}

func (i ingress) List(c Context) ([]k8s.ResourceRow, error) { return listVia(i, c) }

func ingressName(o runtime.Object, _ k8s.RowContext) string {
	return o.(*networkingv1.Ingress).Name
}

func ingressAddresses(o runtime.Object, _ k8s.RowContext) string {
	ing := o.(*networkingv1.Ingress)
	var addrs []string
	for _, lb := range ing.Status.LoadBalancer.Ingress {
		if lb.IP != "" {
			addrs = append(addrs, lb.IP)
		} else if lb.Hostname != "" {
			addrs = append(addrs, lb.Hostname)
		}
	}
	if len(addrs) == 0 {
		return "<pending>"
	}
	return strings.Join(addrs, ",")
}

// ingressRulesSummary collapses an ingress's rules into a single display
// string: "host/path → svc:port" for the first rule, "+N more" suffix for
// any beyond. Exposed for direct unit testing.
func ingressRulesSummary(ing *networkingv1.Ingress) string {
	var rulePairs []string
	for _, rule := range ing.Spec.Rules {
		host := rule.Host
		if host == "" {
			host = "*"
		}
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			svc := path.Backend.Service
			if svc != nil {
				rulePairs = append(rulePairs,
					fmt.Sprintf("%s%s → %s:%d", host, path.Path, svc.Name, svc.Port.Number))
			}
		}
	}
	switch len(rulePairs) {
	case 0:
		return "<none>"
	case 1:
		return rulePairs[0]
	default:
		return fmt.Sprintf("%s  +%d more", rulePairs[0], len(rulePairs)-1)
	}
}

func ingressRules(o runtime.Object, _ k8s.RowContext) string {
	return ingressRulesSummary(o.(*networkingv1.Ingress))
}

func ingressAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*networkingv1.Ingress).CreationTimestamp)
}

func (ingress) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("ingress.Fetch: no Clientset")
	}
	return c.Clientset.NetworkingV1().Ingresses(ns).Get(ctx, name, metav1.GetOptions{})
}

func (ingress) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("ingress.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.NetworkingV1().Ingresses(ns).Delete(c.Ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
}

func (i ingress) Topology(c Context, ns, name string) (*k8s.TreeNode, error) {
	ings, err := listTyped[*networkingv1.Ingress](c, i.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	var ing *networkingv1.Ingress
	for _, x := range ings {
		if x.Name == name {
			ing = x
			break
		}
	}
	if ing == nil {
		return nil, nil
	}
	totalRoutes := 0
	for _, r := range ing.Spec.Rules {
		if r.HTTP != nil {
			totalRoutes += len(r.HTTP.Paths)
		}
	}
	root := &k8s.TreeNode{Kind: "Ingress", Name: ing.Name, Status: fmt.Sprintf("%d routes", totalRoutes)}

	svcs, err := listTyped[*corev1.Service](c, k8s.ServiceGVR, ns)
	if err != nil {
		return root, err
	}
	pods, err := listTyped[*corev1.Pod](c, k8s.PodGVR, ns)
	if err != nil {
		return root, err
	}

	for _, rule := range ing.Spec.Rules {
		host := rule.Host
		if host == "" {
			host = "*"
		}
		ruleNode := &k8s.TreeNode{Kind: "Rule", Name: host}
		if rule.HTTP == nil {
			root.Children = append(root.Children, ruleNode)
			continue
		}
		for _, path := range rule.HTTP.Paths {
			svcBackend := path.Backend.Service
			if svcBackend == nil {
				continue
			}
			routeNode := &k8s.TreeNode{
				Kind: "Route",
				Name: fmt.Sprintf("%s → %s:%d", path.Path, svcBackend.Name, svcBackend.Port.Number),
			}
			for _, svc := range svcs {
				if svc.Name != svcBackend.Name {
					continue
				}
				svcNode := &k8s.TreeNode{Kind: "Service", Name: svc.Name, Status: string(svc.Spec.Type)}
				if svc.Spec.Selector != nil {
					for _, p := range pods {
						if matchesLabelSelector(p.Labels, svc.Spec.Selector) {
							svcNode.Children = append(svcNode.Children, podTreeNode(p))
						}
					}
				}
				routeNode.Children = append(routeNode.Children, svcNode)
				break
			}
			ruleNode.Children = append(ruleNode.Children, routeNode)
		}
		root.Children = append(root.Children, ruleNode)
	}
	return root, nil
}

func init() { register(ingress{}) }
