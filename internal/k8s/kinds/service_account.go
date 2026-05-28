package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// serviceAccount preserves the pre-migration behavior: Fetch is wired so the
// YAML view works, but List returns nil because no row builder existed. It
// also implements XRayer so users can see which Secrets / ImagePullSecrets
// the SA references and which are missing.
type serviceAccount struct{}

func (serviceAccount) Meta() Meta {
	return Meta{
		Kind:       "ServiceAccount",
		Plural:     "serviceaccounts",
		Aliases:    []string{"sa"},
		Namespaced: true,
		GVR:        k8s.ServiceAccountGVR,
	}
}

func (serviceAccount) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "SECRETS", Width: 10},
		{Header: "AGE", Width: 10},
	}
}

func (serviceAccount) List(Context) ([]Row, error) { return nil, nil }

func (serviceAccount) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("serviceAccount.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().ServiceAccounts(ns).Get(ctx, name, metav1.GetOptions{})
}

// XRay lists the Secrets + ImagePullSecrets this ServiceAccount references,
// marking any whose name is not present in the namespace Secret cache as
// "missing" — the canonical "why is image-pull failing?" trail.
func (serviceAccount) XRay(c Context, ns, name string) (*k8s.TreeNode, error) {
	sas, err := listTyped[*corev1.ServiceAccount](c, k8s.ServiceAccountGVR, ns)
	if err != nil {
		return nil, err
	}
	var sa *corev1.ServiceAccount
	for _, x := range sas {
		if x.Name == name {
			sa = x
			break
		}
	}
	if sa == nil {
		return nil, nil
	}
	secs, err := listTyped[*corev1.Secret](c, k8s.SecretGVR, ns)
	if err != nil {
		return nil, err
	}
	present := secretNames(secs)
	root := &k8s.TreeNode{Kind: "ServiceAccount", Name: sa.Name}
	for _, ref := range sa.Secrets {
		root.Children = append(root.Children, lookupNode("Secret", ref.Name, present[ref.Name]))
	}
	for _, ref := range sa.ImagePullSecrets {
		root.Children = append(root.Children, lookupNode("ImagePullSecret", ref.Name, present[ref.Name]))
	}
	return root, nil
}

func init() { register(serviceAccount{}) }
