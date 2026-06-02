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

// TestPodStatusPhases exercises the k9s-parity Phase logic: init-container
// phases, terminated exit codes, the Completed→Running rescue, and the
// Terminating override.
func TestPodStatusPhases(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	started := true
	cases := []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{
			name: "init crashloop wins over phase",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "init"}}},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					InitContainerStatuses: []corev1.ContainerStatus{{
						Name:  "init",
						State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
					}},
				},
			},
			want: "Init:CrashLoopBackOff",
		},
		{
			name: "container terminated exit code",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{{
						Name:  "main",
						State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 137}},
					}},
				},
			},
			want: "ExitCode:137",
		},
		{
			name: "completed container rescued to running",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "a"}, {Name: "b"}}},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{Name: "a", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Completed"}}},
						{Name: "b", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
					},
				},
			},
			want: "Running",
		},
		{
			name: "sidecar started+ready does not block",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers:     []corev1.Container{{Name: "main"}},
					InitContainers: []corev1.Container{{Name: "side", RestartPolicy: &always}},
				},
				Status: corev1.PodStatus{
					Phase:                 corev1.PodRunning,
					InitContainerStatuses: []corev1.ContainerStatus{{Name: "side", Started: &started, Ready: true}},
					ContainerStatuses:     []corev1.ContainerStatus{{Name: "main", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
				},
			},
			want: "Running",
		},
		{
			name: "deletion timestamp -> terminating",
			pod: func() *corev1.Pod {
				now := metav1.Now()
				return &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning},
				}
			}(),
			want: "Terminating",
		},
	}
	for _, tc := range cases {
		if got := podStatus(tc.pod); got != tc.want {
			t.Errorf("%s: podStatus = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestPodReadyCountsSidecar verifies sidecar (restartable) init containers are
// counted in READY/RESTARTS, like kubectl and k9s, while ordinary init
// containers are not.
func TestPodReadyCountsSidecar(t *testing.T) {
	always := corev1.ContainerRestartPolicyAlways
	p := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "main"}},
			InitContainers: []corev1.Container{
				{Name: "side", RestartPolicy: &always}, // counts
				{Name: "setup"},                        // run-once, does not count
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses:     []corev1.ContainerStatus{{Name: "main", Ready: true, RestartCount: 1}},
			InitContainerStatuses: []corev1.ContainerStatus{{Name: "side", Ready: true, RestartCount: 2}, {Name: "setup"}},
		},
	}
	ready, total, restarts := podReadyCounts(p)
	if ready != 2 || total != 2 {
		t.Errorf("ready/total = %d/%d, want 2/2 (main + sidecar)", ready, total)
	}
	if restarts != 3 {
		t.Errorf("restarts = %d, want 3 (1 main + 2 sidecar)", restarts)
	}
}

// TestPodColumnSortTypePropagates locks the integration seam: SortType set in
// pod.Columns() must survive the shim into the descriptor that Resolve returns
// (sortRows reads it from there, not from the Kind). It also pins the column /
// Row.Values index alignment the sort relies on.
func TestPodColumnSortTypePropagates(t *testing.T) {
	desc, ok := k8s.Resolve("Pod")
	if !ok {
		t.Fatal("Resolve(Pod) not registered")
	}
	byHeader := map[string]k8s.SortType{}
	for _, c := range desc.Columns {
		byHeader[c.Header] = c.SortType
	}
	if byHeader["AGE"] != k8s.SortTime {
		t.Errorf("AGE SortType = %d, want SortTime (%d)", byHeader["AGE"], k8s.SortTime)
	}
	if byHeader["RESTARTS"] != k8s.SortNumber {
		t.Errorf("RESTARTS SortType = %d, want SortNumber (%d)", byHeader["RESTARTS"], k8s.SortNumber)
	}
	if byHeader["NAME"] != k8s.SortString {
		t.Errorf("NAME SortType = %d, want SortString (%d)", byHeader["NAME"], k8s.SortString)
	}
}
