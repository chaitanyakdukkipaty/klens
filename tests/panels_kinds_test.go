package klenstests

import (
	"context"
	"testing"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/k8s/kinds"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// rowAlignmentCase is one kind's fixture: a closure that builds rows for a
// minimally-populated zero object and a kind name to look up the descriptor.
type rowAlignmentCase struct {
	kind  string
	build func(t *testing.T) []k8s.ResourceRow
}

func int32Ptr(v int32) *int32 { return &v }

// viaKindsList drives a migrated kind through Kind.List against a FakeLister
// seeded with `objs`. Consolidates the boilerplate every post-migration test
// case otherwise duplicates.
func viaKindsList(t *testing.T, kind, ns string, objs ...runtime.Object) []k8s.ResourceRow {
	t.Helper()
	cs := fake.NewSimpleClientset(objs...)
	k, ok := kinds.Lookup(kind)
	if !ok {
		t.Fatalf("kinds.Lookup(%q): not registered", kind)
	}
	rows, err := k.List(kinds.Context{
		Ctx:       context.Background(),
		Namespace: ns,
		Lister:    k8s.NewFakeLister(cs),
	})
	if err != nil {
		t.Fatalf("kinds %s.List: %v", kind, err)
	}
	return rows
}

// TestRowsAlignWithColumns — for every kind that has a row builder, rows
// produced from a minimally-populated zero object have exactly len(Columns)
// Values. This test is the depth payoff: the parallel-array invariant
// (Columns ↔ Values index alignment) becomes a single failing test instead
// of a silent UI misalignment.
func TestRowsAlignWithColumns(t *testing.T) {
	now := metav1.Now()
	meta := metav1.ObjectMeta{Name: "x", Namespace: "ns", CreationTimestamp: now}

	cases := []rowAlignmentCase{
		{
			kind: "Pod",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "Pod", "ns",
					&corev1.Pod{ObjectMeta: meta, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}}})
			},
		},
		{
			kind: "Deployment",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "Deployment", "ns",
					&appsv1.Deployment{ObjectMeta: meta, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)}})
			},
		},
		{
			kind: "StatefulSet",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "StatefulSet", "ns",
					&appsv1.StatefulSet{ObjectMeta: meta, Spec: appsv1.StatefulSetSpec{Replicas: int32Ptr(1)}})
			},
		},
		{
			kind: "DaemonSet",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "DaemonSet", "ns", &appsv1.DaemonSet{ObjectMeta: meta})
			},
		},
		{
			kind: "ReplicaSet",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "ReplicaSet", "ns",
					&appsv1.ReplicaSet{ObjectMeta: meta, Spec: appsv1.ReplicaSetSpec{Replicas: int32Ptr(1)}})
			},
		},
		{
			kind: "Job",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "Job", "ns", &batchv1.Job{ObjectMeta: meta})
			},
		},
		{
			kind: "CronJob",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "CronJob", "ns",
					&batchv1.CronJob{ObjectMeta: meta, Spec: batchv1.CronJobSpec{Schedule: "* * * * *"}})
			},
		},
		{
			kind: "Service",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "Service", "ns", &corev1.Service{ObjectMeta: meta})
			},
		},
		{
			kind: "Ingress",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "Ingress", "ns", &networkingv1.Ingress{ObjectMeta: meta})
			},
		},
		{
			kind: "ConfigMap",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "ConfigMap", "ns", &corev1.ConfigMap{ObjectMeta: meta})
			},
		},
		{
			kind: "Secret",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "Secret", "ns", &corev1.Secret{ObjectMeta: meta})
			},
		},
		{
			kind: "Node",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "Node", "", &corev1.Node{ObjectMeta: meta})
			},
		},
		{
			kind: "PersistentVolumeClaim",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "PersistentVolumeClaim", "ns", &corev1.PersistentVolumeClaim{ObjectMeta: meta})
			},
		},
		{
			kind: "PersistentVolume",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "PersistentVolume", "", &corev1.PersistentVolume{ObjectMeta: meta})
			},
		},
		{
			kind: "Event",
			build: func(t *testing.T) []k8s.ResourceRow {
				return viaKindsList(t, "Event", "ns",
					&corev1.Event{ObjectMeta: meta, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "x"}})
			},
		},
		// HelmRelease's alignment is covered in internal/k8s/kinds/helm_release_test.go
		// — fake.NewSimpleClientset doesn't serve the dynamic GVR, so we
		// exercise the row-building inline from the kinds package instead.
	}

	for _, c := range cases {
		c := c
		t.Run(c.kind, func(t *testing.T) {
			rd, ok := k8s.Resolve(c.kind)
			if !ok {
				t.Fatalf("kind %q not in Registry", c.kind)
			}
			rows := c.build(t)
			if len(rows) == 0 {
				t.Fatalf("kind %s: no rows produced", c.kind)
			}
			for i, r := range rows {
				if got, want := len(r.Values), len(rd.Columns); got != want {
					t.Errorf("kind %s row %d: %d values vs %d columns; values=%v",
						c.kind, i, got, want, r.Values)
				}
			}
		})
	}
}

// TestListRowsHandlersRegisteredForExpectedKinds — every kind that has a
// builder closure registered should have a non-nil ListRows on its
// descriptor after panels init(). Catches a forgotten SetHandlers call.
func TestListRowsHandlersRegisteredForExpectedKinds(t *testing.T) {
	expectListRows := []string{
		"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet",
		"Job", "CronJob", "Service", "Ingress", "ConfigMap", "Secret",
		"Node", "PersistentVolumeClaim", "PersistentVolume", "Event", "HelmRelease",
	}
	for _, kind := range expectListRows {
		rd, ok := k8s.Resolve(kind)
		if !ok {
			t.Errorf("kind %q not registered in Registry", kind)
			continue
		}
		if rd.ListRows == nil {
			t.Errorf("kind %q: ListRows handler not registered", kind)
		}
	}

	expectFetch := []string{
		"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet",
		"Service", "Ingress", "ConfigMap", "Secret", "Node",
		"PersistentVolume", "PersistentVolumeClaim", "Job", "CronJob",
		"ServiceAccount", "Namespace",
	}
	for _, kind := range expectFetch {
		rd, ok := k8s.Resolve(kind)
		if !ok {
			t.Errorf("kind %q not registered", kind)
			continue
		}
		if rd.Fetch == nil {
			t.Errorf("kind %q: Fetch handler not registered", kind)
		}
	}

	expectXRay := []string{
		"Pod", "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet",
		"Job", "CronJob", "Service", "Ingress", "ServiceAccount",
	}
	for _, kind := range expectXRay {
		rd, ok := k8s.Resolve(kind)
		if !ok {
			t.Errorf("kind %q not registered", kind)
			continue
		}
		if rd.BuildXRay == nil {
			t.Errorf("kind %q: BuildXRay handler not registered", kind)
		}
	}
}
