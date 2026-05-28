// Package describe wraps k8s.io/kubectl/pkg/describe so the rest of the app
// only depends on a single chokepoint into that library's sizable graph.
//
// Describe returns the same human-readable text as `kubectl describe` for any
// kind kubectl knows about, and falls back to the generic describer (the same
// fallback kubectl uses) for arbitrary CRDs.
package describe

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/kubectl/pkg/describe"
)

// Settings used by every klens describe call. ChunkSize=500 matches kubectl's
// default and bounds the events tail on noisy namespaces.
var settings = describe.DescriberSettings{ShowEvents: true, ChunkSize: 500}

// Describe returns the kubectl-style describe output for the named resource.
// gvk.Version may be empty — in that case the cluster's preferred version is
// resolved via REST discovery.
func Describe(cfg *rest.Config, gvk schema.GroupVersionKind, namespace, name string) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("describe: nil rest config")
	}
	// Fast path: kubectl ships a hand-written describer for every built-in.
	if d, ok := describe.DescriberFor(gvk.GroupKind(), cfg); ok {
		return d.Describe(namespace, name, settings)
	}
	// Fallback for CRDs: build a RESTMapping via discovery and use the
	// generic describer (events + spec/status dump on Unstructured).
	mapping, err := restMapping(cfg, gvk)
	if err != nil {
		return "", fmt.Errorf("describe: %w", err)
	}
	d, ok := describe.GenericDescriberFor(mapping, cfg)
	if !ok {
		return "", fmt.Errorf("describe: no describer for %s", gvk)
	}
	return d.Describe(namespace, name, settings)
}

func restMapping(cfg *rest.Config, gvk schema.GroupVersionKind) (*meta.RESTMapping, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))
	versions := []string(nil)
	if gvk.Version != "" {
		versions = []string{gvk.Version}
	}
	return mapper.RESTMapping(gvk.GroupKind(), versions...)
}
