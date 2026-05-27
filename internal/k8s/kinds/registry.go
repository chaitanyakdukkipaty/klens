package kinds

import (
	"strings"
	"sync"
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
