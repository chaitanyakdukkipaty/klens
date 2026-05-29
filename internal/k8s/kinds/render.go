package kinds

import (
	"fmt"
	"time"

	k8s "github.com/chaitanyak/klens/internal/k8s"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RowStatuser is the optional interface a Kind implements to populate
// Row.Status — the color key the resource table uses to render the STATUS
// column. Kinds without a colored status (StatefulSet, ConfigMap, …) leave
// it unimplemented; RenderRows then writes the empty string and the table
// renders the cells uncolored. The function is pure: given the typed object,
// return the status string. Same shape as a per-cell Render closure.
//
// Invariant: the string returned here must equal the STATUS column's Render
// output for the same object, because the table colors any cell whose value
// matches row.Status exactly. Diverging produces an uncolored STATUS cell
// and no other visible failure — easy to miss.
type RowStatuser interface {
	Kind
	RowStatus(obj runtime.Object) string
}

// RowSortByTimer is the optional interface a Kind implements to set
// Row.SortByTime so the table renders entries newest-first. Only Event
// uses this today.
type RowSortByTimer interface {
	Kind
	RowSortByTime(obj runtime.Object) time.Time
}

// RowNamer is the optional interface a Kind implements when the row's
// display Name should differ from the object's metadata Name. No kind
// uses this today; it remains for future use.
type RowNamer interface {
	Kind
	RowName(obj runtime.Object) string
}

// FaultRowMarker is the events-style "is this row a fault" predicate. A
// kind that implements it opts the table into the ctrl+z faults toggle
// (today: Event only — Type ∈ {Warning, Error}). Returning false from
// IsFaultRow filters the row out when faults mode is on.
type FaultRowMarker interface {
	Kind
	IsFaultRow(obj runtime.Object) bool
}

// InvolvedObjectResolver is the events-style "this row points to another
// resource" hook. Today only Event satisfies it; if a future kind references
// another (e.g. PVC → PV) the same hook generalizes.
type InvolvedObjectResolver interface {
	Kind
	InvolvedObject(obj runtime.Object) (gvk schema.GroupVersionKind, namespace, name string, ok bool)
}

// RenderRows turns a slice of typed objects into Rows by walking each kind's
// Columns and invoking the per-cell Render closure. Name/Namespace default to
// the object's metadata; Status/SortByTime/Name can be customized via the
// optional RowStatuser / RowSortByTimer / RowNamer interfaces.
//
// Every Column passed in must have a non-nil Render. Kinds whose List returns
// nil (passthrough YAML-only kinds without informers) are allowed to leave
// Render nil on their Columns because they never reach this function.
func RenderRows(k Kind, objs []runtime.Object, rctx k8s.RowContext) []k8s.ResourceRow {
	cols := k.Columns()
	rows := make([]k8s.ResourceRow, 0, len(objs))
	statuser, _ := any(k).(RowStatuser)
	sorter, _ := any(k).(RowSortByTimer)
	namer, _ := any(k).(RowNamer)
	for _, obj := range objs {
		vals := make([]string, len(cols))
		for j, c := range cols {
			vals[j] = c.Render(obj, rctx)
		}
		row := k8s.ResourceRow{Values: vals, Raw: obj}
		if m, err := meta.Accessor(obj); err == nil {
			row.Name = m.GetName()
			row.Namespace = m.GetNamespace()
		}
		if namer != nil {
			row.Name = namer.RowName(obj)
		}
		if statuser != nil {
			row.Status = statuser.RowStatus(obj)
		}
		if sorter != nil {
			row.SortByTime = sorter.RowSortByTime(obj)
		}
		rows = append(rows, row)
	}
	return rows
}

// listVia is the default List body every Kind reuses: lift typed objects
// from the Lister, hand them to RenderRows. A kind's List collapses to
// `return listVia(k, c)` once every column in Columns() has a non-nil
// Render. Kinds whose List has a non-standard read path (HelmRelease's
// dynamic GVR discovery) keep their own override.
//
// The namespace passed to Lister.List is forced to "" for cluster-scoped
// kinds even when the caller's Context carries a selected namespace — the
// informer cache filter would otherwise reject every cluster-scoped object
// whose own GetNamespace() is empty, silently producing zero rows.
func listVia(k Kind, c Context) ([]k8s.ResourceRow, error) {
	if c.Lister == nil {
		return nil, fmt.Errorf("%s.List: no Lister", k.Meta().Kind)
	}
	ns := c.Namespace
	if !k.Meta().Namespaced {
		ns = ""
	}
	objs, err := c.Lister.List(c.Ctx, k.Meta().GVR, ns)
	if err != nil {
		return nil, err
	}
	return RenderRows(k, objs, c.RenderCtx()), nil
}
