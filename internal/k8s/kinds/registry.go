package kinds

import (
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Registry holds the set of Kinds known to klens. There is one process-global
// instance (Default) populated at init time. Building a registry per cluster
// is unnecessary because Kinds are stateless — cluster identity lives in the
// Context, not in the Kind value.
type Registry struct {
	mu    sync.RWMutex
	kinds []Kind
	byKey map[string]Kind
}

// Default is the global registry. shim.go's init() registers every migrated
// Kind here at package load time.
var Default = newRegistry()

func newRegistry() *Registry {
	return &Registry{byKey: map[string]Kind{}}
}

// Register adds k to the registry, keyed by its Meta().Kind, Plural, and
// every Alias (case-insensitive). Idempotent on a given key — later
// registrations overwrite earlier ones, which matters only if two Kinds
// claim the same alias (a programmer error; the test catches it).
func (r *Registry) Register(k Kind) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.kinds = append(r.kinds, k)
	m := k.Meta()
	r.byKey[strings.ToLower(m.Kind)] = k
	r.byKey[strings.ToLower(m.Plural)] = k
	for _, a := range m.Aliases {
		r.byKey[strings.ToLower(a)] = k
	}
}

// Resolve returns the Kind registered under `name`, matched against kind /
// plural / alias (case-insensitive).
func (r *Registry) Resolve(name string) (Kind, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	k, ok := r.byKey[strings.ToLower(name)]
	return k, ok
}

// All returns a snapshot of every registered Kind in registration order.
// Used by the InformerRegistry wiring to enumerate (gvr, kind) pairs.
func (r *Registry) All() []Kind {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Kind, len(r.kinds))
	copy(out, r.kinds)
	return out
}

// Lookup is a convenience wrapper around Default.Resolve.
func Lookup(name string) (Kind, bool) { return Default.Resolve(name) }

// All returns every Kind in the default registry.
func All() []Kind { return Default.All() }

// ResolveByGVK returns the Kind whose Meta matches gvk's Group, Version, and
// Kind. The Kind name lookup is the primary key (every registered Kind has a
// unique Meta.Kind); when gvk.Group / gvk.Version are non-empty they
// additionally have to match the Kind's GVR. APIVersion-less references
// (common for stripped-down Events) match by Kind name alone.
func (r *Registry) ResolveByGVK(gvk schema.GroupVersionKind) (Kind, bool) {
	if gvk.Kind == "" {
		return nil, false
	}
	k, ok := r.Resolve(gvk.Kind)
	if !ok {
		return nil, false
	}
	m := k.Meta()
	if gvk.Group != "" && gvk.Group != m.GVR.Group {
		return nil, false
	}
	if gvk.Version != "" && gvk.Version != m.GVR.Version {
		return nil, false
	}
	return k, true
}

// LookupByGVK is a convenience wrapper around Default.ResolveByGVK.
func LookupByGVK(gvk schema.GroupVersionKind) (Kind, bool) { return Default.ResolveByGVK(gvk) }
