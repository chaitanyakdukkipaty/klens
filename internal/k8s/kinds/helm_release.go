package kinds

import (
	"context"
	"encoding/json"
	"fmt"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
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
		{Header: "NAME", Width: 36, Flex: true, Render: helmReleaseName},
		{Header: "CHART", Width: 24, Render: helmReleaseChart},
		{Header: "VERSION", Width: 12, Render: helmReleaseVersion},
		{Header: "READY", Width: 8, Render: helmReleaseReady},
		{Header: "STATUS", Width: 40, Render: helmReleaseStatusMsg},
		{Header: "SUSPENDED", Width: 10, Render: helmReleaseSuspended},
		{Header: "AGE", Width: 10, Render: helmReleaseAge},
	}
}

// RowStatus drives the color key (Suspended/Ready/NotReady).
func (helmRelease) RowStatus(o runtime.Object) string {
	u := o.(*unstructured.Unstructured)
	if helmReleaseIsSuspended(u) {
		return "Suspended"
	}
	if helmReleaseReadyCond(u) == "True" {
		return "Ready"
	}
	return "NotReady"
}

func helmReleaseSpec(u *unstructured.Unstructured) map[string]interface{} {
	s, _ := u.Object["spec"].(map[string]interface{})
	return s
}

func helmReleaseChartSpec(u *unstructured.Unstructured) map[string]interface{} {
	spec := helmReleaseSpec(u)
	if spec == nil {
		return nil
	}
	chartTop, _ := spec["chart"].(map[string]interface{})
	if chartTop == nil {
		return nil
	}
	cs, _ := chartTop["spec"].(map[string]interface{})
	return cs
}

func helmReleaseIsSuspended(u *unstructured.Unstructured) bool {
	spec := helmReleaseSpec(u)
	if spec == nil {
		return false
	}
	v, _ := spec["suspend"].(bool)
	return v
}

// helmReleaseReadyCond returns the Ready condition's status ("True"/"False"/etc.)
// from the resource's status.conditions, defaulting to "False".
func helmReleaseReadyCond(u *unstructured.Unstructured) string {
	status, _ := u.Object["status"].(map[string]interface{})
	if status == nil {
		return "False"
	}
	conds, _ := status["conditions"].([]interface{})
	for _, c := range conds {
		cm, _ := c.(map[string]interface{})
		if cm["type"] != "Ready" {
			continue
		}
		if s, ok := cm["status"].(string); ok {
			return s
		}
	}
	return "False"
}

// helmReleaseReadyMessage returns the Ready condition's message, or "-" if
// unset.
func helmReleaseReadyMessage(u *unstructured.Unstructured) string {
	status, _ := u.Object["status"].(map[string]interface{})
	if status == nil {
		return "-"
	}
	conds, _ := status["conditions"].([]interface{})
	for _, c := range conds {
		cm, _ := c.(map[string]interface{})
		if cm["type"] != "Ready" {
			continue
		}
		if msg, ok := cm["message"].(string); ok && msg != "" {
			return msg
		}
	}
	return "-"
}

func helmReleaseName(o runtime.Object, _ k8s.RowContext) string {
	return o.(*unstructured.Unstructured).GetName()
}

func helmReleaseChart(o runtime.Object, _ k8s.RowContext) string {
	cs := helmReleaseChartSpec(o.(*unstructured.Unstructured))
	if cs == nil {
		return "-"
	}
	if v, ok := cs["chart"].(string); ok && v != "" {
		return v
	}
	return "-"
}

func helmReleaseVersion(o runtime.Object, _ k8s.RowContext) string {
	cs := helmReleaseChartSpec(o.(*unstructured.Unstructured))
	if cs == nil {
		return "-"
	}
	if v, ok := cs["version"].(string); ok && v != "" {
		return v
	}
	return "-"
}

func helmReleaseReady(o runtime.Object, _ k8s.RowContext) string {
	return helmReleaseReadyCond(o.(*unstructured.Unstructured))
}

func helmReleaseStatusMsg(o runtime.Object, _ k8s.RowContext) string {
	return helmReleaseReadyMessage(o.(*unstructured.Unstructured))
}

func helmReleaseSuspended(o runtime.Object, _ k8s.RowContext) string {
	if helmReleaseIsSuspended(o.(*unstructured.Unstructured)) {
		return "True"
	}
	return "False"
}

func helmReleaseAge(o runtime.Object, _ k8s.RowContext) string {
	return k8s.AgeString(o.(*unstructured.Unstructured).GetCreationTimestamp())
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

func (h helmRelease) List(c Context) ([]k8s.ResourceRow, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("helmRelease.List: no Lister")
	}
	objs, err := c.Lister.List(c.Ctx, h.gvrFor(c), c.Namespace)
	if err != nil {
		return nil, err
	}
	return RenderRows(h, objs, c.RenderCtx()), nil
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

// Suspend toggles spec.suspend on the HelmRelease via merge-patch through
// the dynamic client. Used by both the suspend ("s") and resume ("r")
// action paths in model.go — the boolean is the only thing that differs.
func (h helmRelease) Suspend(c Context, ns, name string, suspend bool) error {
	if c.Dynamic == nil {
		return fmt.Errorf("helmRelease.Suspend: no Dynamic client")
	}
	body, _ := json.Marshal(map[string]any{"spec": map[string]any{"suspend": suspend}})
	_, err := c.Dynamic.Resource(h.gvrFor(c)).Namespace(ns).Patch(
		c.Ctx, name, types.MergePatchType, body, metav1.PatchOptions{})
	return err
}

func init() { register(helmRelease{}) }
