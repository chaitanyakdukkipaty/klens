package k8s

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Lister reads cluster state through a substitutable seam. Production uses
// CachedLister (informer cache); tests use FakeLister wrapping
// fake.NewSimpleClientset. Watch is intentionally not on the interface — the
// TUI already gets change notifications through WatcherFactory's out-of-band
// msgCh, and no caller has asked for watch through this seam.
type Lister interface {
	List(ctx context.Context, gvr schema.GroupVersionResource, ns string) ([]runtime.Object, error)
}

// GVR constants for the informer-backed built-in kinds. Sharing them between
// adapters keeps the CachedLister and FakeLister switches in lockstep, and
// gives the future kinds package (plan 1) a single source of truth to import.
//
// HelmRelease's GVR is discovered at runtime against the cluster — see
// WatcherFactory.HelmReleaseGVR — so it has no constant here.
var (
	PodGVR                   = corev1.SchemeGroupVersion.WithResource("pods")
	ServiceGVR               = corev1.SchemeGroupVersion.WithResource("services")
	NodeGVR                  = corev1.SchemeGroupVersion.WithResource("nodes")
	NamespaceGVR             = corev1.SchemeGroupVersion.WithResource("namespaces")
	ConfigMapGVR             = corev1.SchemeGroupVersion.WithResource("configmaps")
	SecretGVR                = corev1.SchemeGroupVersion.WithResource("secrets")
	ServiceAccountGVR        = corev1.SchemeGroupVersion.WithResource("serviceaccounts")
	PersistentVolumeGVR      = corev1.SchemeGroupVersion.WithResource("persistentvolumes")
	PersistentVolumeClaimGVR = corev1.SchemeGroupVersion.WithResource("persistentvolumeclaims")
	EventGVR                 = corev1.SchemeGroupVersion.WithResource("events")

	DeploymentGVR  = appsv1.SchemeGroupVersion.WithResource("deployments")
	StatefulSetGVR = appsv1.SchemeGroupVersion.WithResource("statefulsets")
	DaemonSetGVR   = appsv1.SchemeGroupVersion.WithResource("daemonsets")
	ReplicaSetGVR  = appsv1.SchemeGroupVersion.WithResource("replicasets")

	JobGVR     = batchv1.SchemeGroupVersion.WithResource("jobs")
	CronJobGVR = batchv1.SchemeGroupVersion.WithResource("cronjobs")

	IngressGVR = networkingv1.SchemeGroupVersion.WithResource("ingresses")
)
