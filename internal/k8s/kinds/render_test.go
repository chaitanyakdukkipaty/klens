package kinds

import (
	"context"
	"testing"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// recordingLister captures every (gvr, ns) pair List was invoked with so a
// test can assert what namespace listVia forwarded.
type recordingLister struct {
	lastGVR schema.GroupVersionResource
	lastNS  string
}

func (r *recordingLister) List(_ context.Context, gvr schema.GroupVersionResource, ns string) ([]runtime.Object, error) {
	r.lastGVR = gvr
	r.lastNS = ns
	return nil, nil
}

// TestListViaForcesEmptyNSForClusterScoped guards the regression where
// cluster-scoped kinds (Namespace, Node, PersistentVolume, ClusterRole…)
// were filtered to zero rows whenever the user had a real namespace
// selected. listVia forces ns="" when Meta().Namespaced is false.
func TestListViaForcesEmptyNSForClusterScoped(t *testing.T) {
	rl := &recordingLister{}
	c := Context{Ctx: context.Background(), Namespace: "default", Lister: rl}

	for _, kind := range []string{"Namespace", "Node", "PersistentVolume", "ClusterRole", "ClusterRoleBinding", "StorageClass"} {
		t.Run(kind, func(t *testing.T) {
			k, ok := Lookup(kind)
			if !ok {
				t.Fatalf("Lookup(%q): not registered", kind)
			}
			// Three of these (ClusterRole/ClusterRoleBinding/StorageClass) have
			// List returning nil directly, bypassing listVia. Drive listVia
			// explicitly so the assertion exercises the cluster-scoped branch
			// for every cluster-scoped kind, not just the ones that wired List
			// through listVia.
			if _, err := listVia(k, c); err != nil {
				t.Fatalf("listVia: %v", err)
			}
			if rl.lastNS != "" {
				t.Errorf("cluster-scoped %s: listVia passed ns=%q, want \"\"", kind, rl.lastNS)
			}
		})
	}
}

// TestListViaForwardsNamespaceForNamespaced confirms the regression fix
// doesn't over-correct — namespaced kinds still receive c.Namespace verbatim.
func TestListViaForwardsNamespaceForNamespaced(t *testing.T) {
	rl := &recordingLister{}
	c := Context{Ctx: context.Background(), Namespace: "kube-system", Lister: rl}

	k, ok := Lookup("Pod")
	if !ok {
		t.Fatalf("Lookup(Pod): not registered")
	}
	if _, err := listVia(k, c); err != nil {
		t.Fatalf("listVia: %v", err)
	}
	if rl.lastNS != "kube-system" {
		t.Errorf("namespaced Pod: listVia passed ns=%q, want kube-system", rl.lastNS)
	}
}

// _ verifies recordingLister satisfies k8s.Lister at compile time.
var _ k8s.Lister = (*recordingLister)(nil)
