package kinds

import (
	"testing"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// TestHelmReleaseRowColumnsAlignment guards the parallel-array invariant for
// HelmRelease. The general tests/panels_kinds_test.go alignment loop can't
// cover this kind because FakeLister doesn't serve the dynamic GVR;
// exercising RenderRows directly is the simplest equivalent.
func TestHelmReleaseRowColumnsAlignment(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]interface{}{
		"metadata": map[string]interface{}{"name": "x", "namespace": "ns"},
	}}
	h := helmRelease{}
	rows := RenderRows(h, []runtime.Object{u}, k8s.RowContext{})
	if len(rows) != 1 {
		t.Fatalf("RenderRows: got %d rows want 1", len(rows))
	}
	if got, want := len(rows[0].Values), len(h.Columns()); got != want {
		t.Errorf("row values=%d columns=%d; values=%v", got, want, rows[0].Values)
	}
}
