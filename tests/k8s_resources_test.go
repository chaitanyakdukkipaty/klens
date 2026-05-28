// Package klenstests holds black-box unit tests for the klens packages.
//
// Tests live here, separate from the production code under internal/, and use
// only the exported APIs of each package. Importing internal/ui/panels here
// triggers its init() so handler registration is exercised end-to-end.
package klenstests

import (
	"strings"
	"testing"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	_ "github.com/chaitanyak/klens/internal/k8s/kinds" // triggers shim registration for migrated kinds (Pod, Namespace, …)
	_ "github.com/chaitanyak/klens/internal/ui/panels" // triggers handler registration for unmigrated kinds
)

// TestResolveRoundtrip — every Kind, Plural, and alias resolves back to the
// same descriptor. Catches typos in alias entries.
func TestResolveRoundtrip(t *testing.T) {
	for _, rd := range k8s.Registry {
		if got, ok := k8s.Resolve(rd.Kind); !ok || got.Kind != rd.Kind {
			t.Errorf("Resolve(%q) = %+v, %v; want kind %q", rd.Kind, got, ok, rd.Kind)
		}
		if got, ok := k8s.Resolve(rd.Plural); !ok || got.Kind != rd.Kind {
			t.Errorf("Resolve(%q) = %+v, %v; want kind %q", rd.Plural, got, ok, rd.Kind)
		}
		for _, a := range rd.Aliases {
			if got, ok := k8s.Resolve(a); !ok || got.Kind != rd.Kind {
				t.Errorf("Resolve(%q) = %+v, %v; want kind %q", a, got, ok, rd.Kind)
			}
		}
		if got, ok := k8s.Resolve(strings.ToUpper(rd.Kind)); !ok || got.Kind != rd.Kind {
			t.Errorf("Resolve(%q) case-insensitive failed", rd.Kind)
		}
	}
}

// TestRegistryColumnsWellFormed — every column has a non-empty header,
// positive width, and headers are unique within a kind.
func TestRegistryColumnsWellFormed(t *testing.T) {
	for _, rd := range k8s.Registry {
		if len(rd.Columns) == 0 {
			t.Errorf("kind %s: no columns defined", rd.Kind)
			continue
		}
		seen := make(map[string]bool)
		for i, c := range rd.Columns {
			if c.Header == "" {
				t.Errorf("kind %s: column %d has empty header", rd.Kind, i)
			}
			if c.Width <= 0 {
				t.Errorf("kind %s: column %d %q has non-positive width %d", rd.Kind, i, c.Header, c.Width)
			}
			if seen[c.Header] {
				t.Errorf("kind %s: duplicate column header %q", rd.Kind, c.Header)
			}
			seen[c.Header] = true
		}
	}
}

// TestRegistryAliasesUnique — no alias collides across kinds. A collision
// would make Resolve("foo") silently bind to whichever descriptor was
// registered first.
func TestRegistryAliasesUnique(t *testing.T) {
	owners := make(map[string]string)
	for _, rd := range k8s.Registry {
		tokens := append([]string{rd.Kind, rd.Plural}, rd.Aliases...)
		for _, tok := range tokens {
			key := strings.ToLower(tok)
			if prev, ok := owners[key]; ok && prev != rd.Kind {
				t.Errorf("alias collision: %q claimed by both %s and %s", tok, prev, rd.Kind)
			}
			owners[key] = rd.Kind
		}
	}
}

// TestSetHandlersOnUnknownKind — registering against an unknown kind reports
// the failure rather than silently succeeding.
func TestSetHandlersOnUnknownKind(t *testing.T) {
	if k8s.SetHandlers("NoSuchKind", nil, nil, nil) {
		t.Error("SetHandlers returned true for an unregistered kind")
	}
}

// TestEveryDescriptorHasFetch — every registered kind exposes a Fetch
// handler so the YAML viewer's "y" key can resolve the object. HelmRelease
// is exempt because its YAML comes from the cached unstructured
// (FetchHelmReleaseYAMLCmd), not a Get call. XRay presence is now an
// interface check in the kinds package and is covered by per-kind tests.
func TestEveryDescriptorHasFetch(t *testing.T) {
	for _, rd := range k8s.Registry {
		if rd.Fetch == nil && rd.Kind != "HelmRelease" {
			t.Errorf("kind %s: no Fetch handler registered", rd.Kind)
		}
	}
}
