package klenstests

import (
	"testing"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/panels"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// rowAlignmentCase is one kind's fixture: a closure that builds rows for a
// minimally-populated zero object and a kind name to look up the descriptor.
type rowAlignmentCase struct {
	kind  string
	build func() []k8s.ResourceRow
}

func int32Ptr(v int32) *int32 { return &v }

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
			build: func() []k8s.ResourceRow {
				return panels.BuildPodRows(
					[]*corev1.Pod{{ObjectMeta: meta, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}}}},
					k8s.MetricsUpdatedMsg{},
					nil,
				)
			},
		},
		{
			kind: "Deployment",
			build: func() []k8s.ResourceRow {
				return panels.BuildDeploymentRows([]*appsv1.Deployment{{
					ObjectMeta: meta, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
				}})
			},
		},
		{
			kind: "StatefulSet",
			build: func() []k8s.ResourceRow {
				return panels.BuildStatefulSetRows([]*appsv1.StatefulSet{{
					ObjectMeta: meta, Spec: appsv1.StatefulSetSpec{Replicas: int32Ptr(1)},
				}})
			},
		},
		{
			kind: "DaemonSet",
			build: func() []k8s.ResourceRow {
				return panels.BuildDaemonSetRows([]*appsv1.DaemonSet{{ObjectMeta: meta}})
			},
		},
		{
			kind: "ReplicaSet",
			build: func() []k8s.ResourceRow {
				return panels.BuildReplicaSetRows([]*appsv1.ReplicaSet{{
					ObjectMeta: meta, Spec: appsv1.ReplicaSetSpec{Replicas: int32Ptr(1)},
				}})
			},
		},
		{
			kind: "Job",
			build: func() []k8s.ResourceRow {
				return panels.BuildJobRows([]*batchv1.Job{{ObjectMeta: meta}})
			},
		},
		{
			kind: "CronJob",
			build: func() []k8s.ResourceRow {
				return panels.BuildCronJobRows([]*batchv1.CronJob{{
					ObjectMeta: meta, Spec: batchv1.CronJobSpec{Schedule: "* * * * *"},
				}})
			},
		},
		{
			kind: "Service",
			build: func() []k8s.ResourceRow {
				return panels.BuildServiceRows([]*corev1.Service{{ObjectMeta: meta}})
			},
		},
		{
			kind: "Ingress",
			build: func() []k8s.ResourceRow {
				return panels.BuildIngressRows([]*networkingv1.Ingress{{ObjectMeta: meta}})
			},
		},
		{
			kind: "ConfigMap",
			build: func() []k8s.ResourceRow {
				return panels.BuildConfigMapRows([]*corev1.ConfigMap{{ObjectMeta: meta}})
			},
		},
		{
			kind: "Secret",
			build: func() []k8s.ResourceRow {
				return panels.BuildSecretRows([]*corev1.Secret{{ObjectMeta: meta}})
			},
		},
		{
			kind: "Node",
			build: func() []k8s.ResourceRow {
				return panels.BuildNodeRows([]*corev1.Node{{ObjectMeta: meta}})
			},
		},
		{
			kind: "PersistentVolumeClaim",
			build: func() []k8s.ResourceRow {
				return panels.BuildPVCRows([]*corev1.PersistentVolumeClaim{{ObjectMeta: meta}})
			},
		},
		{
			kind: "PersistentVolume",
			build: func() []k8s.ResourceRow {
				return panels.BuildPVRows([]*corev1.PersistentVolume{{ObjectMeta: meta}})
			},
		},
		{
			kind: "Event",
			build: func() []k8s.ResourceRow {
				return panels.BuildEventRows([]*corev1.Event{{
					ObjectMeta:     meta,
					InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "x"},
				}})
			},
		},
		{
			kind: "HelmRelease",
			build: func() []k8s.ResourceRow {
				return panels.BuildHelmReleaseRows([]*unstructured.Unstructured{{
					Object: map[string]interface{}{
						"metadata": map[string]interface{}{"name": "x", "namespace": "ns"},
					},
				}})
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.kind, func(t *testing.T) {
			rd, ok := k8s.Resolve(c.kind)
			if !ok {
				t.Fatalf("kind %q not in Registry", c.kind)
			}
			rows := c.build()
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

	expectTopology := []string{"Deployment", "Service", "Ingress"}
	for _, kind := range expectTopology {
		rd, ok := k8s.Resolve(kind)
		if !ok {
			t.Errorf("kind %q not registered", kind)
			continue
		}
		if rd.BuildTopology == nil {
			t.Errorf("kind %q: BuildTopology handler not registered", kind)
		}
	}
}
