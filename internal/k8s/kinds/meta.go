package kinds

import "k8s.io/apimachinery/pkg/runtime/schema"

// Meta is the static identity card for a Kind. It bundles the fields the
// legacy ResourceDescriptor exposed (Kind, Plural, Aliases, Namespaced,
// APIGroup/APIVersion) into the single typed shape every kinds.Kind returns.
type Meta struct {
	Kind    string
	Plural  string
	Aliases []string
	// Group is the navigation category this kind appears under in the
	// sidebar (Workloads / Network / Config / Storage / Access / Cluster /
	// Helm). Required for new kinds — ungrouped kinds land in "Other".
	Group      string
	Namespaced bool
	GVR        schema.GroupVersionResource
}
