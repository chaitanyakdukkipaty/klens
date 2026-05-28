package kinds

import (
	"context"
	"testing"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// TestPodListColumnsAlignment guards the parallel-array invariant: every row
// produced by pod.List has exactly len(Columns) values. The legacy
// equivalent was TestRowsAlignWithColumns; this is the same check moved
// next to its kind.
func TestPodListColumnsAlignment(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns", CreationTimestamp: metav1.Now()},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "main"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	})
	p := pod{}
	rows, err := p.List(Context{
		Ctx:       context.Background(),
		Namespace: "ns",
		Lister:    k8s.NewFakeLister(cs),
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("List: got %d rows want 1", len(rows))
	}
	if got, want := len(rows[0].Values), len(p.Columns()); got != want {
		t.Errorf("row 0: %d values vs %d columns; values=%v", got, want, rows[0].Values)
	}
}

// TestPodFetchAndDelete walks the Fetch → Delete sequence end-to-end through
// the fake clientset, mirroring the live use case where the user opens YAML
// and then deletes the pod.
func TestPodFetchAndDelete(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
	})
	c := Context{Ctx: context.Background(), Clientset: cs, Namespace: "ns"}
	p := pod{}

	obj, err := p.Fetch(c.Ctx, c, "ns", "p1")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, ok := obj.(*corev1.Pod); !ok {
		t.Fatalf("Fetch returned %T, want *corev1.Pod", obj)
	}

	if err := p.Delete(c, "ns", "p1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := cs.CoreV1().Pods("ns").Get(context.Background(), "p1", metav1.GetOptions{}); err == nil {
		t.Errorf("expected Get to fail after Delete")
	}
}

// TestPodApply patches the pod via merge-patch JSON and confirms the change
// landed. Exercises the Applier capability that the model now dispatches
// via type assertion → kinds.ApplyCmd.
func TestPodApply(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns", Labels: map[string]string{"a": "1"}},
	})
	c := Context{Ctx: context.Background(), Clientset: cs}
	body := []byte(`{"metadata":{"labels":{"a":"2"}}}`)
	if err := (pod{}).Apply(c, "ns", "p1", body); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got, err := cs.CoreV1().Pods("ns").Get(context.Background(), "p1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Labels["a"] != "2" {
		t.Errorf("label a after apply: %q, want 2", got.Labels["a"])
	}
}

// TestPodContainerPorts confirms the ports come from the cached pod object.
// Exercises the PortForwarder capability.
func TestPodContainerPorts(t *testing.T) {
	cs := fake.NewSimpleClientset(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p1", Namespace: "ns"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "main",
				Ports: []corev1.ContainerPort{
					{Name: "http", ContainerPort: 8080},
					{ContainerPort: 9090},
				},
			}},
		},
	})
	c := Context{Ctx: context.Background(), Lister: k8s.NewFakeLister(cs)}
	ports, err := (pod{}).ContainerPorts(c, "ns", "p1")
	if err != nil {
		t.Fatalf("ContainerPorts: %v", err)
	}
	if len(ports) != 2 {
		t.Fatalf("got %d ports, want 2", len(ports))
	}
	if ports[0].Name != "http" || ports[0].Port != 8080 {
		t.Errorf("port[0] = %+v", ports[0])
	}
}

// TestPodLogTargets validates the single-target shape: a Pod streams from
// itself, not from any controller chain.
func TestPodLogTargets(t *testing.T) {
	targets, err := (pod{}).LogTargets(Context{}, "ns", "p1")
	if err != nil {
		t.Fatalf("LogTargets: %v", err)
	}
	if len(targets) != 1 || targets[0].Namespace != "ns" || targets[0].Pod != "p1" {
		t.Errorf("targets = %+v", targets)
	}
}

// TestPodCapabilities — every key Pod participates in (yaml view, logs,
// attach, delete, port-forward, metrics, apply, xray) is a capability
// interface that pod{} must implement. Scaler must not be satisfied: pods
// don't scale. Catches regressions in pod's interface satisfaction (a
// missed method would silently disable the UI hint).
func TestPodCapabilities(t *testing.T) {
	k, ok := Default.Resolve("Pod")
	if !ok {
		t.Fatalf("kinds.Default has no Pod registered")
	}
	must(t, "Logger", func() bool { _, ok := any(k).(Logger); return ok })
	must(t, "Attacher", func() bool { _, ok := any(k).(Attacher); return ok })
	must(t, "Deleter", func() bool { _, ok := any(k).(Deleter); return ok })
	must(t, "Killer", func() bool { _, ok := any(k).(Killer); return ok })
	must(t, "PortForwarder", func() bool { _, ok := any(k).(PortForwarder); return ok })
	must(t, "MetricsSupporter", func() bool { _, ok := any(k).(MetricsSupporter); return ok })
	must(t, "Applier", func() bool { _, ok := any(k).(Applier); return ok })
	must(t, "XRayer", func() bool { _, ok := any(k).(XRayer); return ok })
	mustNot(t, "Scaler", func() bool { _, ok := any(k).(Scaler); return ok })
}

func must(t *testing.T, name string, f func() bool) {
	t.Helper()
	if !f() {
		t.Errorf("expected Pod to implement %s", name)
	}
}

func mustNot(t *testing.T, name string, f func() bool) {
	t.Helper()
	if f() {
		t.Errorf("expected Pod to NOT implement %s", name)
	}
}
