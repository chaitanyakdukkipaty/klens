package k8s

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// RowContext carries cross-cutting state needed by row extractors.
// It is passed verbatim from caller (model) to descriptor.ListRows.
type RowContext struct {
	Metrics MetricsUpdatedMsg
	// PortForwardActive reports whether the named pod has at least one
	// active port-forward session. Optional — nil means "no PF state to
	// show", in which case the PF column renders an inactive marker.
	PortForwardActive func(namespace, name string) bool
}

// ListRowsFunc returns rows for a kind, given a watcher and namespace.
// Implementations type-assert internally to the concrete Kubernetes type.
type ListRowsFunc func(wf *WatcherFactory, namespace string, ctx RowContext) []ResourceRow

// FetchFunc retrieves a live object by name (used by the YAML viewer).
//
// The clientset is the kubernetes.Interface (not the concrete *Clientset) so
// tests can swap in fake.NewSimpleClientset.
type FetchFunc func(cs kubernetes.Interface, name, namespace string) (any, error)

// (legacy package-level Action/RegisterAction/LookupAction surface was removed
// alongside ResourceDescriptor.Actions and Supports*. Dispatch now lives in
// internal/k8s/kinds — every action site does `_, ok := kind.(Capability)`.)

// BuildXRayFunc returns an XRay tree rooted at the named resource.
type BuildXRayFunc func(wf *WatcherFactory, namespace, name string) *TreeNode

// ResourceDescriptor describes a Kubernetes resource type.
//
// The metadata fields (Kind, Plural, Columns) are static and live in the
// Registry. The behavior closures (ListRows, Fetch, BuildXRay) are attached
// via SetHandlers from a registration site so that per-kind code can live in
// one place even when it depends on the UI layer (lipgloss-rendered
// percentages, etc.) that the k8s package itself doesn't import.
//
// Capability presence (logs, scale, delete, …) is no longer expressed on the
// descriptor — every action site checks via `kind.(kinds.Capability)` type
// assertion. The descriptor's only remaining job is the legacy listing /
// fetch / xray dispatch that the shim still bridges.
type ResourceDescriptor struct {
	Kind       string
	Plural     string
	APIGroup   string
	APIVersion string
	Namespaced bool
	Aliases    []string
	// NavGroup is the sidebar category (kinds.Meta.Group): Workloads /
	// Network / Config / Storage / Access / Cluster / Helm.
	NavGroup string
	Columns  []Column

	// Behavior — populated via SetHandlers, optional per kind.
	ListRows  ListRowsFunc
	Fetch     FetchFunc
	BuildXRay BuildXRayFunc
}

// Column defines a table column for a resource type.
// When Flex is true, Width is the minimum width and the column expands into
// any surplus horizontal space available to the table.
// When Scrollable is true, the resource table offers horizontal scrolling on
// this column (←/→ keys and horizontal wheel ticks shift its rune offset).
// At most one column per resource should be marked Scrollable; the first one
// wins if multiple are set.
//
// Render is the per-cell function: given the typed object and a RowContext,
// return the cell string. When set, the kind's List body collapses to a
// generic renderer that fills Row.Values via Render across every column. When
// nil, the kind hand-builds Row.Values inside List (legacy path, removed in
// plan 06 step 7).
type Column struct {
	Header     string
	Width      int
	Flex       bool
	Scrollable bool
	// SortType selects the comparator used when this column is the active
	// interactive sort key (see CellCompare). The zero value (SortString)
	// gives natural human ordering and is correct for most text columns.
	SortType SortType
	Render   func(obj runtime.Object, ctx RowContext) string
}

// SortType selects the comparator used when a column is the active sort key.
// The zero value, SortString, means natural human ordering; the others parse
// each cell into a comparable scalar so AGE sorts by elapsed time, RESTARTS by
// number, and CPU/MEM by quantity rather than by raw display text.
type SortType int

const (
	SortString   SortType = iota // natural string order ("pod-2" < "pod-10")
	SortTime                     // duration string ("5d", "2h3m"); blank/"n/a" sort last
	SortNumber                   // integer/float; thousands separators ignored
	SortCapacity                 // resource.Quantity ("256Mi", "1Gi") by byte value
)

// CellCompare returns -1, 0, or +1 comparing two rendered cell values under
// the given SortType. ANSI styling is stripped first so colored cells (%CPU,
// STATUS) compare on their plain text. Mirrors k9s's model1.Less branches; a
// 0 result lets callers fall back to a stable tie-break (e.g. RowLess).
func CellCompare(st SortType, a, b string) int {
	a = strings.TrimSpace(ansi.Strip(a))
	b = strings.TrimSpace(ansi.Strip(b))
	switch st {
	case SortTime:
		return cmpInt64(durationToSeconds(a), durationToSeconds(b))
	case SortNumber:
		return cmpNatural(strings.ReplaceAll(a, ",", ""), strings.ReplaceAll(b, ",", ""))
	case SortCapacity:
		return cmpInt64(capacityToNumber(a), capacityToNumber(b))
	default:
		return cmpNatural(a, b)
	}
}

// cmpNatural is a symmetric three-way wrapper over naturalLess: values that
// are naturally equal but textually distinct (e.g. "a1" vs "a01") return 0 so
// the caller's tie-break decides, keeping the ordering antisymmetric.
func cmpNatural(a, b string) int {
	switch {
	case naturalLess(a, b):
		return -1
	case naturalLess(b, a):
		return 1
	default:
		return 0
	}
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// durationToSeconds parses a Kubernetes human duration ("5d", "2h3m", "47s")
// into seconds. Empty/unknown values map to MaxInt64 so they sort last on an
// ascending sort, matching k9s.
func durationToSeconds(d string) int64 {
	if d == "" || d == "n/a" || d == "<unknown>" {
		return math.MaxInt64
	}
	var total, n int64
	for _, r := range d {
		switch r {
		case 'y':
			total += n * 365 * 24 * 3600
			n = 0
		case 'd':
			total += n * 24 * 3600
			n = 0
		case 'h':
			total += n * 3600
			n = 0
		case 'm':
			total += n * 60
			n = 0
		case 's':
			total += n
			n = 0
		default:
			if r >= '0' && r <= '9' {
				n = n*10 + int64(r-'0')
			}
		}
	}
	return total
}

// capacityToNumber parses a resource.Quantity ("256Mi", "1Gi", "500m") into
// its integer value. Unparseable or blank cells sort as 0.
func capacityToNumber(s string) int64 {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	return q.Value()
}

// ResourceRow is a single row in the resource table.
type ResourceRow struct {
	Name       string
	Namespace  string
	Status     string // used as color key for status styling
	Age        string
	SortByTime time.Time   // when non-zero, WithRows sorts descending by this instead of Name
	Values     []string    // ordered display values matching column layout; when set, buildRow uses these
	Extra      []string    // additional column values (legacy)
	Raw        interface{} // underlying k8s object
}

// RowLess orders two rows by (namespace, name) using natural ordering, so
// "pod-2" sorts before "pod-10" instead of after it. This mirrors k9s's
// default comparator (model1.Less → sortorder.NaturalLess) and its fqn
// tie-break: rows are uniquely identified by namespace/name, so the order is
// total and sort.Slice stays deterministic across refreshes. Namespace is the
// primary key for forward-compatibility with an all-namespaces view; in the
// current single-namespace view it is constant and the comparison degrades to
// name ordering.
func RowLess(a, b ResourceRow) bool {
	if a.Namespace != b.Namespace {
		return naturalLess(a.Namespace, b.Namespace)
	}
	return naturalLess(a.Name, b.Name)
}

// naturalLess reports whether a < b under natural (human) ordering: maximal
// runs of digits compare by numeric value (ignoring leading zeros), all other
// bytes compare bytewise. Kept dependency-free; swap in
// github.com/fvbommel/sortorder.NaturalLess for byte-exact k9s parity.
func naturalLess(a, b string) bool {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		ca, cb := a[ia], b[ib]
		da := ca >= '0' && ca <= '9'
		db := cb >= '0' && cb <= '9'
		if da && db {
			na, nb := ia, ib
			for ia < len(a) && a[ia] >= '0' && a[ia] <= '9' {
				ia++
			}
			for ib < len(b) && b[ib] >= '0' && b[ib] <= '9' {
				ib++
			}
			sa := strings.TrimLeft(a[na:ia], "0")
			sb := strings.TrimLeft(b[nb:ib], "0")
			if len(sa) != len(sb) {
				return len(sa) < len(sb)
			}
			if sa != sb {
				return sa < sb
			}
			continue // equal numeric run; keep scanning
		}
		if ca != cb {
			return ca < cb
		}
		ia++
		ib++
	}
	return len(a)-ia < len(b)-ib
}

// Registry is the static list of all known resource types.
var Registry = []ResourceDescriptor{
	// Pod migrated to internal/k8s/kinds/pod.go (plan 01).
	// The shim re-registers an equivalent descriptor at init().
	// Deployment, StatefulSet migrated to internal/k8s/kinds/ (plan 01).
	// DaemonSet migrated to internal/k8s/kinds/daemonset.go (plan 01).
	// ReplicaSet migrated to internal/k8s/kinds/replicaset.go (plan 01).
	// Job, CronJob migrated to internal/k8s/kinds/ (plan 01).
	// Service, Endpoints, Ingress, ConfigMap, Secret, ServiceAccount,
	// PersistentVolumeClaim migrated to internal/k8s/kinds/ (plan 01).
	// HorizontalPodAutoscaler, NetworkPolicy, Role, RoleBinding migrated to internal/k8s/kinds/ (plan 01).
	// Cluster-scoped
	// Node, PersistentVolume migrated to internal/k8s/kinds/ (plan 01).
	// Namespace migrated to internal/k8s/kinds/namespace.go (plan 01).
	// ClusterRole, ClusterRoleBinding, StorageClass migrated to internal/k8s/kinds/ (plan 01).
	// Event migrated to internal/k8s/kinds/event.go (plan 01).
	// HelmRelease migrated to internal/k8s/kinds/helm_release.go (plan 01).
}

// GVR returns the GroupVersionResource for this descriptor. APIVersion
// defaults to "v1"; APIGroup defaults to "" (core). Resource is the Plural.
// HelmRelease's version is discovered at runtime — its descriptor's value is
// the v2 default; callers that care about the live version go through
// WatcherFactory.HelmReleaseGVR instead.
func (rd ResourceDescriptor) GVR() schema.GroupVersionResource {
	v := rd.APIVersion
	if v == "" {
		v = "v1"
	}
	return schema.GroupVersionResource{
		Group:    rd.APIGroup,
		Version:  v,
		Resource: rd.Plural,
	}
}

// aliasMap maps alias/kind (lowercase) → ResourceDescriptor index.
var aliasMap map[string]int

func init() {
	aliasMap = make(map[string]int, len(Registry)*3)
	for i, r := range Registry {
		aliasMap[strings.ToLower(r.Kind)] = i
		aliasMap[strings.ToLower(r.Plural)] = i
		for _, a := range r.Aliases {
			aliasMap[strings.ToLower(a)] = i
		}
	}
}

// Resolve returns the ResourceDescriptor for a given kind, plural, or alias.
func Resolve(input string) (ResourceDescriptor, bool) {
	i, ok := aliasMap[strings.ToLower(input)]
	if !ok {
		return ResourceDescriptor{}, false
	}
	return Registry[i], true
}

// RegisterDescriptor appends a fully-built descriptor to the Registry and
// extends aliasMap so future Resolve calls find it. Used by the kinds
// package shim (plan 01 migration) to register kinds that were removed
// from the static Registry slice. Returns false if any of the descriptor's
// keys (kind, plural, alias) is already taken — programmer error.
func RegisterDescriptor(rd ResourceDescriptor) bool {
	keys := []string{strings.ToLower(rd.Kind), strings.ToLower(rd.Plural)}
	for _, a := range rd.Aliases {
		keys = append(keys, strings.ToLower(a))
	}
	for _, k := range keys {
		if _, exists := aliasMap[k]; exists {
			return false
		}
	}
	Registry = append(Registry, rd)
	idx := len(Registry) - 1
	for _, k := range keys {
		aliasMap[k] = idx
	}
	return true
}

// SetHandlers attaches behavior closures to the descriptor for `kind`. Returns
// false if the kind is not registered. Pass nil for any handler that does not
// apply (e.g. BuildXRay for kinds without an XRayer implementation).
//
// This registration pattern lets per-kind glue live in one file while keeping
// the metadata Registry pure data. The kinds package's shim is the only
// remaining caller.
func SetHandlers(kind string, list ListRowsFunc, fetch FetchFunc, xray BuildXRayFunc) bool {
	i, ok := aliasMap[strings.ToLower(kind)]
	if !ok {
		return false
	}
	if list != nil {
		Registry[i].ListRows = list
	}
	if fetch != nil {
		Registry[i].Fetch = fetch
	}
	if xray != nil {
		Registry[i].BuildXRay = xray
	}
	return true
}

// AgeString converts a creation timestamp to a human-readable age string.
func AgeString(t metav1.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := metav1.Now().Sub(t.Time)
	switch {
	case d.Hours() >= 24*365:
		return fmt.Sprintf("%dy", int(d.Hours()/(24*365)))
	case d.Hours() >= 24:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d.Hours() >= 1:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d.Minutes() >= 1:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
