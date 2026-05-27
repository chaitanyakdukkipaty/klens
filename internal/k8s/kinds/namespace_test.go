package kinds

import (
	"context"
	"testing"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestNamespaceListAndFetch exercises both read paths through fake clients —
// the Lister seam for List, the Clientset seam for Fetch. Together they
// confirm Namespace's two production code paths work end-to-end against
// the fake adapter the rest of the test suite uses.
func TestNamespaceListAndFetch(t *testing.T) {
	cs := fake.NewSimpleClientset(
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "default", CreationTimestamp: metav1.Now()},
			Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "kube-system", CreationTimestamp: metav1.Now()},
			Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceActive},
		},
	)
	c := Context{
		Ctx:       context.Background(),
		Lister:    k8s.NewFakeLister(cs),
		Clientset: cs,
	}

	n := namespace{}
	rows, err := n.List(c)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("List: got %d rows want 2", len(rows))
	}
	cols := n.Columns()
	for _, r := range rows {
		if got, want := len(r.Values), len(cols); got != want {
			t.Errorf("row %q: %d values vs %d columns; values=%v", r.Name, got, want, r.Values)
		}
		if r.Status != "Active" {
			t.Errorf("row %q: status %q, want Active", r.Name, r.Status)
		}
	}

	obj, err := n.Fetch(c.Ctx, c, "", "default")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		t.Fatalf("Fetch: got %T, want *corev1.Namespace", obj)
	}
	if ns.Name != "default" {
		t.Errorf("Fetch: got %q want default", ns.Name)
	}
}

// TestNamespaceDelete confirms Delete goes through the clientset path; the
// fake clientset records the deletion so a follow-up Get returns NotFound.
func TestNamespaceDelete(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "doomed"},
	})
	c := Context{Ctx: context.Background(), Clientset: cs}
	if err := (namespace{}).Delete(c, "", "doomed"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := cs.CoreV1().Namespaces().Get(context.Background(), "doomed", metav1.GetOptions{}); err == nil {
		t.Errorf("expected Get to fail after Delete")
	}
}

// TestNamespaceShimRegisteredDescriptor validates the keystone: the
// kinds.Default registry has namespace, AND k8s.Registry holds a
// shim-generated descriptor with the correct metadata + ListRows / Fetch
// wiring. Capability presence is no longer expressed as Supports* booleans;
// it's checked at every action site via type assertion against the Kind
// returned by kinds.Lookup.
func TestNamespaceShimRegisteredDescriptor(t *testing.T) {
	k, ok := Default.Resolve("Namespace")
	if !ok {
		t.Fatalf("kinds.Default has no Namespace registered")
	}
	if _, ok := any(k).(Deleter); !ok {
		t.Errorf("Namespace should implement Deleter")
	}
	if _, ok := any(k).(Scaler); ok {
		t.Errorf("Namespace should not implement Scaler")
	}
	rd, ok := k8s.Resolve("Namespace")
	if !ok {
		t.Fatalf("k8s.Registry has no Namespace — shim did not register")
	}
	if rd.Kind != "Namespace" || rd.Plural != "namespaces" {
		t.Errorf("shim descriptor: kind=%q plural=%q", rd.Kind, rd.Plural)
	}
	if rd.ListRows == nil {
		t.Errorf("ListRows handler not wired")
	}
	if rd.Fetch == nil {
		t.Errorf("Fetch handler not wired")
	}
}
