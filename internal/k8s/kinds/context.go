package kinds

import (
	"context"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Context carries cross-cutting state every Kind method might need. Stateless
// Kind types take a Context per call so a single global registry can serve
// multi-cluster sessions — the model rebuilds Context on every cluster /
// namespace switch instead of rebuilding the registry.
//
// Lister is the read seam (informer cache in production, fake.NewSimpleClientset
// in tests). Clientset and Dynamic are the write seams for the operations
// the Kind exposes via capability interfaces (Delete, Scale, etc.).
//
// Metrics and PFActive are passed verbatim from the table render path so the
// Pod row builder can render the CPU/MEM percentages and the PF indicator
// without reaching back into the app package.
type Context struct {
	Ctx       context.Context
	Namespace string
	Lister    k8s.Lister
	Clientset kubernetes.Interface
	Dynamic   dynamic.Interface

	// Metrics carries the latest metrics-server snapshot (pods + nodes).
	// Nil-safe — kinds that don't render metrics ignore it.
	Metrics k8s.MetricsUpdatedMsg

	// PFActive reports whether a pod has at least one active port-forward
	// session. Optional — nil means "no PF state to render", which collapses
	// to the inactive marker for every row.
	PFActive func(ns, name string) bool

	// HelmGVR is the discovered HelmRelease GVR for the current cluster.
	// Populated by the model after WatcherFactory.HelmReleaseGVR resolves.
	HelmGVR schema.GroupVersionResource
}
