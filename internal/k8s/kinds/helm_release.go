package kinds

import (
	"context"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// helmRelease is the one kind whose GVR is discovered against the live
// cluster at startup — the API may serve v2, v2beta2, or v2beta1. The
// Meta().GVR returns the v2 default; List/Fetch prefer the live GVR carried
// in Context.HelmGVR so the informer's cache key matches what the model
// populated post-discovery.
type helmRelease struct{}

var helmReleaseDefaultGVR = schema.GroupVersionResource{
	Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases",
}

func (helmRelease) Meta() Meta {
	return Meta{
		Kind:       "HelmRelease",
		Plural:     "helmreleases",
		Aliases:    []string{"hr"},
		Namespaced: true,
		GVR:        helmReleaseDefaultGVR,
	}
}

func (helmRelease) Columns() []k8s.Column {
	return []k8s.Column{
		{Header: "NAME", Width: 36, Flex: true},
		{Header: "CHART", Width: 24},
		{Header: "VERSION", Width: 12},
		{Header: "READY", Width: 8},
		{Header: "STATUS", Width: 40},
		{Header: "SUSPENDED", Width: 10},
		{Header: "AGE", Width: 10},
	}
}

// gvrFor returns the live discovered GVR if Context carries one, else the
// default v2 GVR. Mirrors WatcherFactory.HelmReleaseGVR's "fall back to v2"
// behavior so calls don't fail before discovery completes.
func (h helmRelease) gvrFor(c Context) schema.GroupVersionResource {
	if c.HelmGVR.Resource != "" {
		return c.HelmGVR
	}
	return h.Meta().GVR
}

func (h helmRelease) List(c Context) ([]Row, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("helmRelease.List: no Lister")
	}
	objs, err := c.Lister.List(c.Ctx, h.gvrFor(c), c.Namespace)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, len(objs))
	for _, o := range objs {
		u, ok := o.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		rows = append(rows, helmReleaseRow(u))
	}
	return rows, nil
}

// Fetch returns the cached unstructured object via the Lister. The YAML
// viewer currently special-cases HelmRelease through FetchHelmReleaseYAMLCmd
// (it has the unstructured in hand from the table row); this Fetch is wired
// so SupportsYAML stays true through the shim path. Step 6 of plan 01
// removes the model's special-case and routes through here.
func (h helmRelease) Fetch(_ context.Context, c Context, ns, name string) (Object, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("helmRelease.Fetch: no Lister")
	}
	objs, err := c.Lister.List(c.Ctx, h.gvrFor(c), ns)
	if err != nil {
		return nil, err
	}
	for _, o := range objs {
		u, ok := o.(*unstructured.Unstructured)
		if !ok {
			continue
		}
		if u.GetName() == name {
			return u, nil
		}
	}
	return nil, fmt.Errorf("helmrelease %s/%s not in cache", ns, name)
}

// helmReleaseRow renders a single unstructured HelmRelease into a Row.
// Status / chart / version / ready come from nested map fields the API
// emits; the helper does defensive type assertions on every level.
func helmReleaseRow(u *unstructured.Unstructured) Row {
	spec, _ := u.Object["spec"].(map[string]interface{})
	chart := "-"
	version := "-"
	if spec != nil {
		if chartTop, ok := spec["chart"].(map[string]interface{}); ok {
			if cs, ok := chartTop["spec"].(map[string]interface{}); ok {
				if v, ok := cs["chart"].(string); ok && v != "" {
					chart = v
				}
				if v, ok := cs["version"].(string); ok && v != "" {
					version = v
				}
			}
		}
	}

	suspended := false
	if spec != nil {
		if v, ok := spec["suspend"].(bool); ok {
			suspended = v
		}
	}

	ready := "False"
	statusMsg := "-"
	if status, _ := u.Object["status"].(map[string]interface{}); status != nil {
		if conditions, ok := status["conditions"].([]interface{}); ok {
			for _, c := range conditions {
				cm, _ := c.(map[string]interface{})
				if cm["type"] == "Ready" {
					if s, ok := cm["status"].(string); ok {
						ready = s
					}
					if msg, ok := cm["message"].(string); ok && msg != "" {
						statusMsg = msg
					}
					break
				}
			}
		}
	}

	statusKey := "NotReady"
	switch {
	case suspended:
		statusKey = "Suspended"
	case ready == "True":
		statusKey = "Ready"
	}
	suspendedStr := "False"
	if suspended {
		suspendedStr = "True"
	}

	return Row{
		Name:      u.GetName(),
		Namespace: u.GetNamespace(),
		Status:    statusKey,
		Values:    []string{u.GetName(), chart, version, ready, statusMsg, suspendedStr, k8s.AgeString(u.GetCreationTimestamp())},
		Raw:       u,
	}
}

func init() { register(helmRelease{}) }
