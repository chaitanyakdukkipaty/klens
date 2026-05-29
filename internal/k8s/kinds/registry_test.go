package kinds

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TestLookupByGVKResolvesByKindAlone covers the common case Events produce:
// InvolvedObject.APIVersion is often empty on stripped-down references. The
// Kind name alone has to suffice when Group/Version are blank.
func TestLookupByGVKResolvesByKindAlone(t *testing.T) {
	gvk := schema.GroupVersionKind{Kind: "Pod"}
	k, ok := LookupByGVK(gvk)
	if !ok {
		t.Fatal("LookupByGVK(Pod) not resolved")
	}
	if k.Meta().Kind != "Pod" {
		t.Errorf("resolved kind = %q, want Pod", k.Meta().Kind)
	}
}

// TestLookupByGVKVerifiesGroupVersion makes sure a populated APIVersion
// disqualifies a mismatching Kind. Useful when an operator emits an event
// pointing at a CRD with the same shortname as a builtin.
func TestLookupByGVKVerifiesGroupVersion(t *testing.T) {
	// Deployment is in apps/v1 — passing it explicitly should still resolve.
	if k, ok := LookupByGVK(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}); !ok {
		t.Errorf("apps/v1/Deployment not resolved")
	} else if k.Meta().Kind != "Deployment" {
		t.Errorf("resolved %q, want Deployment", k.Meta().Kind)
	}
	// But a wrong group must fail.
	if _, ok := LookupByGVK(schema.GroupVersionKind{Group: "wrong.example.com", Version: "v1", Kind: "Deployment"}); ok {
		t.Error("LookupByGVK with mismatching group resolved a Kind it shouldn't have")
	}
}

// TestLookupByGVKUnknownKind returns ok=false for a kind the registry doesn't
// know about. This is the "event references a CRD we don't track" path —
// the app shows the kind name in the status bar instead of jumping.
func TestLookupByGVKUnknownKind(t *testing.T) {
	if _, ok := LookupByGVK(schema.GroupVersionKind{Kind: "HelmRelease.invented"}); ok {
		t.Error("LookupByGVK resolved a kind that isn't registered")
	}
	if _, ok := LookupByGVK(schema.GroupVersionKind{}); ok {
		t.Error("LookupByGVK with empty GVK returned ok=true")
	}
}
