package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// secret implements Deleter + Applier.
type secret struct{}

func (secret) Meta() Meta {
	return Meta{
		Kind:       "Secret",
		Plural:     "secrets",
		Aliases:    []string{"sec"},
		Namespaced: true,
		GVR:        k8s.SecretGVR,
	}
}

func (secret) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 40, Flex: true, Render: secretName},
		{Header: "TYPE", Width: 30, Render: secretType},
		{Header: "DATA", Width: 8, Render: secretData},
		{Header: "AGE", Width: 10, Render: secretAge},
	}
}

func (s secret) List(c Context) ([]k8s.ResourceRow, error) { return listVia(s, c) }

func secretName(o runtime.Object, _ k8s.RowContext) string { return o.(*corev1.Secret).Name }

func secretType(o runtime.Object, _ k8s.RowContext) string {
	return string(o.(*corev1.Secret).Type)
}

func secretData(o runtime.Object, _ k8s.RowContext) string {
	return fmt.Sprintf("%d", len(o.(*corev1.Secret).Data))
}

func secretAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*corev1.Secret).CreationTimestamp)
}

func (secret) Fetch(ctx context.Context, c Context, ns, name string) (Object, error) {
	if c.Clientset == nil {
		return nil, fmt.Errorf("secret.Fetch: no Clientset")
	}
	return c.Clientset.CoreV1().Secrets(ns).Get(ctx, name, metav1.GetOptions{})
}

func (secret) Delete(c Context, ns, name string) error {
	if c.Clientset == nil {
		return fmt.Errorf("secret.Delete: no Clientset")
	}
	grace := int64(0)
	return c.Clientset.CoreV1().Secrets(ns).Delete(c.Ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &grace})
}

func (secret) Apply(c Context, ns, name string, body []byte) error {
	if c.Clientset == nil {
		return fmt.Errorf("secret.Apply: no Clientset")
	}
	_, err := c.Clientset.CoreV1().Secrets(ns).Patch(
		c.Ctx, name, types.MergePatchType, body,
		metav1.PatchOptions{FieldManager: "klens"})
	return err
}

func init() { register(secret{}) }
