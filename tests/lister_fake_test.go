package klenstests

import (
	"context"
	"testing"

	k8s "github.com/chaitanyak/klens/internal/k8s"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestFakeListerReturnsSeededObjects proves the test-side adapter: three
// pods seeded into a fake clientset come back through Lister.List as three
// runtime.Object values. Locks in the contract so a future change to the
// dispatch switch can't silently drop a kind without a test surfacing it.
func TestFakeListerReturnsSeededObjects(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "c", Namespace: "ns"}},
	)
	l := k8s.NewFakeLister(cs)

	objs, err := l.List(context.Background(), k8s.PodGVR, "ns")
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("List returned %d objects, want 3", len(objs))
	}
	for _, o := range objs {
		if _, ok := o.(*corev1.Pod); !ok {
			t.Errorf("List returned %T, want *corev1.Pod", o)
		}
	}

	// "all" must collapse to "" so the fake's cluster-wide query matches —
	// otherwise it would treat "all" as a literal namespace name and return
	// zero results.
	objs, err = l.List(context.Background(), k8s.PodGVR, "all")
	if err != nil {
		t.Fatalf("List(all) returned error: %v", err)
	}
	if len(objs) != 3 {
		t.Fatalf("List(all) returned %d objects, want 3", len(objs))
	}
}
