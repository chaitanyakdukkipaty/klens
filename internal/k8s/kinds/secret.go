package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		{Header: "NAME", Width: 40, Flex: true},
		{Header: "TYPE", Width: 30},
		{Header: "DATA", Width: 8},
		{Header: "AGE", Width: 10},
	}
}

func (s secret) List(c Context) ([]Row, error) {
	secs, err := listTyped[*corev1.Secret](c, s.Meta().GVR, c.Namespace)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(secs))
	for _, sec := range secs {
		rows = append(rows, Row{
			Name:      sec.Name,
			Namespace: sec.Namespace,
			Values:    []string{sec.Name, string(sec.Type), fmt.Sprintf("%d", len(sec.Data)), k8s.AgeString(sec.CreationTimestamp)},
			Raw:       sec,
		})
	}
	return rows, nil
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
