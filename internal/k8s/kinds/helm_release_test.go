package kinds

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestHelmReleaseRowColumnsAlignment guards the parallel-array invariant for
// HelmRelease. The general tests/panels_kinds_test.go alignment loop can't
// cover this kind because FakeLister doesn't serve the dynamic GVR;
// exercising the helper directly is the simplest equivalent.
func TestHelmReleaseRowColumnsAlignment(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "x", "namespace": "ns"},
	}}
	row := helmReleaseRow(u)
	if got, want := len(row.Values), len(helmRelease{}.Columns()); got != want {
		t.Errorf("row values=%d columns=%d; values=%v", got, want, row.Values)
	}
}
