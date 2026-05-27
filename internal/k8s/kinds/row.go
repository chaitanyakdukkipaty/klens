package kinds

import k8s "github.com/chaitanyak/klens/internal/k8s"

// Row is the table row produced by Kind.List. Aliased to k8s.ResourceRow so
// the existing resource_table widget can render it without translation —
// the shim's ListRows closure returns these directly into the table's
// pipeline. When plan 06 lands (per-column Render funcs), this stays put
// and Columns() gains the render closures.
type Row = k8s.ResourceRow
