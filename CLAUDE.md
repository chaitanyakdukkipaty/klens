# klens — CLAUDE.md

Lightweight Kubernetes TUI combining k9s keyboard navigation with OpenLens-class GUI features. Pure terminal — no Electron, no WebView.

## Module

```
github.com/chaitanyak/klens
```

## Tech Stack

| Layer | Library |
|---|---|
| TUI framework | `charmbracelet/bubbletea` (Elm architecture) |
| Styling | `charmbracelet/lipgloss` |
| Widgets | `charmbracelet/bubbles` (viewport, textarea, spinner, etc.) |
| Kubernetes client | `k8s.io/client-go` v0.36.0 |
| Syntax highlight | `alecthomas/chroma/v2` |
| Charts | `guptarohit/asciigraph` |
| Diff | `pmezard/go-difflib` |

## Build & Run

```bash
go build ./...                        # check compilation
go run ./cmd/klens/                   # run TUI (requires kubeconfig)
go run ./cmd/klens/ --readonly        # run TUI in read-only mode
go run ./cmd/klens/ --version         # print version
go test ./...                         # run tests
```

## Architecture

```
cmd/klens/
  main.go           → parses --readonly/--version/--help; auto-wraps in tmux when not already
                      in a session; tea.NewProgram(app.New(readOnly)).Run()

internal/app/model.go         → root Bubbletea model; routes all msgs; delegates to child panels
internal/cluster/manager.go   → multi-cluster kubeconfig; lazy clientsets per context
internal/k8s/
  informers.go                → SharedIndexInformer factory; sends ResourceUpdatedMsg to app
  resources.go                → ResourceDescriptor registry (26 types + aliases)
  logs.go                     → multi-pod log fan-in via goroutine channels; LogGroup type; Start/StartGrouped; LogLine carries Group field for tab routing
  metrics.go                  → metrics-server REST polling; MetricsUpdatedMsg
  xray.go                     → ownerReference traversal; TreeNode builder
  operations.go               → delete/scale/rollout/drain/cordon
  portforward.go              → client-go SPDY port-forward (no subprocess)
  exec.go                     → remotecommand SPDY exec (pod attach)
  portforward.go              → SPDY port-forward sessions + PortForwardManager (active sessions registry)
internal/config/config.go     → persisted user preferences (namespace lists, last active namespace per cluster,
                                read_only flag); stored at ~/.config/klens/config.json
internal/ui/
  layout/layout.go            → panel sizing from terminal dimensions
  panels/                     → header, status_bar, nav_panel, resource_table, yaml_viewer, yaml_editor,
                                describe_viewer, log_viewer, xray_panel, metrics_panel
  widgets/                    → sparkline, diff_viewer, tree_renderer, confirm_dialog, scale_dialog,
                                namespace_picker, cluster_picker
  styles/styles.go            → Lipgloss style definitions (Kubernetes blue theme)
```

## Key Conventions

- **Bubbletea message flow**: Informers run in background goroutines; they send to `msgCh chan tea.Msg`; `WatchCmd` relays these to the Bubbletea loop. Never mutate model state outside `Update()`.
- **Lazy clientsets**: `cluster.Manager` creates a `*kubernetes.Clientset` on first use per context. On cluster switch, stop old `WatcherFactory` with `wf.Stop()` before creating a new one.
- **Content modes**: `app.Model.mode` (ModeTable, ModeYAML, ModeEditor, ModeLogs, ModeXRay, ModeMetrics, ModeDescribe) controls which panel `contentView()` renders.
- **Focus vs action keys**: `app.Model.focus` (FocusNav / FocusContent) controls which panel `↑↓/jk` navigate. Action keys (`y`, `d`, `l`, `x`, `m`, `a`, `s`, `ctrl+d`, `ctrl+k`) work regardless of focus — they always operate on the selected table row. Edit-YAML is reached by pressing `e` from inside the YAML viewer (ModeYAML), not from the table — `handleYAMLViewKeys` in `internal/app/model.go` owns that transition.
- **Describe view**: `d` opens a kubectl-style describe view (ModeDescribe) for every registered kind. The thin wrapper around `k8s.io/kubectl/pkg/describe` lives in `internal/k8s/describe/` so the rest of the app keeps only one chokepoint into that library's heavy transitive graph. The view supports `/` regex filter, `n`/`N` match navigation, `c` copy-all, `ctrl+s` save-to-file (`$KLENS_DUMP_DIR` or `~/.klens/dumps`), `F` fullscreen, `esc` peel/back.
- **Read-only mode**: `--readonly` CLI flag or `read_only: true` in config.json blocks all mutating operations (delete, scale, edit-apply, attach). The flag overrides the config but never forces it off.
- **Metrics degradation**: if metrics-server not installed (404 on metrics API), show "n/a" — never block resource browsing.
- **lipgloss constraint**: `MarginLeft()` breaks `Width()` in lipgloss v1.1.0 — use `PaddingLeft()` for all indented panel elements.
- **YAML editor modal states**: `internal/ui/panels/yaml_editor.go` implements vim-style Normal/Insert/DiffConfirm/Applying states. In Normal mode both `hjkl` and arrow keys navigate; `i/a/A/o/O` enter Insert mode; `ctrl+s` opens diff preview. In Insert mode all input goes directly to the `textarea` widget.
- **Log viewer states**: `internal/ui/panels/log_viewer.go` has layered state — filterOn (/ input), searchOn (ctrl+f input), podFilter (1-9 solo), tabGroups (multi-group). `HasActiveState()` and `HandleEsc()` let the root model peel one layer per `esc` instead of exiting log mode immediately. `LogGroup` / `StartGrouped()` in `logs.go` carry the group name through `LogLine.Group` so the viewer can route lines to tabs.
- **JSON colorization**: `tryColorizeJSON` in `log_viewer.go` Chroma-highlights lines that are valid JSON (dracula theme, terminal256 formatter); colorCache is a parallel slice to `lines` so re-colorizing on `J` toggle only re-renders lines, not restreams data.
- **Scrollable columns**: `k8s.Column.Scrollable` is a per-column opt-in (parallel to `Flex`). At most one column per resource sets it; `ResourceTable` reads it through `scrollableColIdx()` and exposes `←/→`, horizontal-wheel ticks, and an `esc`-peel layer on `hScroll`. Adding the flag to any new resource's column is the only step required to enable horizontal scrolling.
- **Resource table scrollbar**: `ResourceTable.View()` reserves the rightmost inner column for a `renderScrollbar` thumb driven by `scrollStart()` over `len(t.filtered)` — works for both key navigation and mouse-wheel cursor moves. The title also appends a `i/N · P%` muted label via `cursorPositionLabel()`.
- **klog suppression**: klog is silenced at startup via `klog.SetOutput(io.Discard)` — suppress before any client-go initialization to avoid noisy stderr.

## Keyboard Shortcuts

### Global

| Key | Action |
|---|---|
| `tab` | cycle focus nav ↔ content |
| `enter` | move focus to resource table |
| `↑↓` / `jk` | navigate |
| `/` | filter |
| `y` | view YAML (press `e` from this view to edit) |
| `d` | describe (kubectl-style, all kinds) |
| `l` | logs (or multi-pod logs with space-selected rows) |
| `x` | xray (multi-hop tree; Pod, all workload controllers, Service, Ingress, ServiceAccount) |
| `m` | metrics |
| `ctrl+d` | delete (graceful — respects `terminationGracePeriodSeconds`) |
| `ctrl+k` | kill (force, grace=0) — red confirm dialog; only Pods see distinct on-wire semantics |
| `a` | attach / exec into pod (tmux: new window; non-tmux: suspend TUI) |
| `s` | scale (Deployments / StatefulSets) |
| `shift+f` / `f` | port-forward (Pods) — modal dialog picks remote/local ports |
| `ctrl+f` | active port-forwards list (anywhere except ModeLogs) |
| `ctrl+r` | reconnect / refresh (stops watcher, reruns full connect) |
| `ctrl+n` | namespace picker |
| `ctrl+o` | cluster context picker |
| `ctrl+z` | rollback last YAML apply (in YAML view) |
| `ctrl+s` | YAML diff preview / save (in YAML editor) |
| `ctrl+v` | paste into filter / search / editor inputs |
| `←/→` | scroll horizontally on columns marked `Scrollable` (e.g. Event MESSAGE); horizontal trackpad wheel does the same |
| `:` | command palette (TODO) |
| `esc` | back to table (or peel log viewer state) |
| `q` | quit |

### Describe viewer

| Key | Action |
|---|---|
| `↑↓` / `jk`     | scroll one line |
| `pgup` / `pgdn` | page |
| `g` / `G`       | top / bottom |
| `/`             | open filter input (case-insensitive regex; hides non-matching lines on enter) |
| `n` / `N`       | next / prev match |
| `c`             | copy full describe output to clipboard |
| `ctrl+s`        | save to `$KLENS_DUMP_DIR/<kind>-<name>-<ts>.txt` (default `~/.klens/dumps/`) |
| `F`             | toggle fullscreen |
| `esc`           | peel state: cancel input → clear filter → back to table |

### Log viewer

| Key | Action |
|---|---|
| `↑↓` / `jk` | scroll |
| `g` / `G` | top / bottom (G also re-enables auto-scroll) |
| `/` | filter lines (hides non-matching) |
| `ctrl+f` | inline search (highlights matches) |
| `n` / `N` | next / prev search match |
| `1`–`9` | solo pod (single-group) or jump to tab (multi-group) |
| `0` | show all pods / return to first tab |
| `tab` | cycle tabs (multi-group mode) |
| `J` | toggle JSON pretty-print + Chroma colorization |
| `esc` | peel state: cancel input → clear search → clear pod filter → clear filter → exit logs |

## Adding a New Resource Type

Create `internal/k8s/kinds/<kind>.go`. Implement `Meta()`, `Columns()` (with a
`Render func(runtime.Object, k8s.RowContext) string` per column), and
`Fetch()`. `List(c)` collapses to a one-liner — `return listVia(k, c)` — for
every kind whose read path is the standard Lister; an explicit List body is
only required for kinds with dynamic GVR discovery (HelmRelease) or anything
that bypasses `Lister`. Implement any capability interfaces the kind supports
(`Deleter`, `Killer`, `Scaler`, `Logger`, `Applier`, `XRayer`,
`PortForwarder`, `Attacher`, `MetricsSupporter`). `Killer` is force-delete
(grace=0) — implement only when "kill" is meaningfully distinct from "delete"
(currently just Pod); for everything else, `KillCmd` falls back to
`DeleteCmd`. Implement the optional `RowStatuser` /
`RowSortByTimer` / `RowNamer` interfaces when the row's color key, sort order,
or display name should diverge from the object's metadata. Register in the
package `init()` via `register(k)`; the shim in `kinds/shim.go` builds the
legacy `k8s.ResourceDescriptor` from the Kind and its capability satisfaction.

### Adding a Column

Append one `{Header, Width, Flex, Render}` to the kind's `Columns()` slice.
`Render` takes `(runtime.Object, k8s.RowContext)` and returns the cell string;
type-assert to the kind's typed Go shape inside. `RowContext.Metrics` and
`RowContext.PortForwardActive` are the only cross-cutting state available to
cells — add a field to `k8s.RowContext` only when a concrete column needs it.
Nothing else changes; `listVia` fills `Row.Values` positionally from each
Render closure.

## Adding XRay to a Resource

Implement the `XRayer` capability interface on the Kind:

```go
func (k myKind) XRay(c Context, ns, name string) (*k8s.TreeNode, error)
```

The shim in `kinds/shim.go` picks it up automatically — no descriptor flag,
no switch statement in `model.go`. Building blocks live in
`internal/k8s/kinds/xray_helpers.go` (`lookupNode`, `containerNode`,
`buildVolumeNode`, `ownedByUID`, `podTreeNode`, status helpers).

Reference trees currently shipped:
- Pod (`pod_xray.go`): `Pod → {Container × N → CM/Secret env-refs}` + `ServiceAccount` + `Volume → CM/Secret/PVC` (full k9s parity)
- Deployment / ReplicaSet: `Deployment → ReplicaSet → Pod` (owner-ref)
- StatefulSet / DaemonSet / Job: `<controller> → Pod` (owner-ref)
- CronJob: `CronJob → Job → Pod` (owner-ref both hops)
- Service: `Service → Pod` (selector match)
- Ingress: `Ingress → Rule → Route → Service → Pod`
- ServiceAccount: `ServiceAccount → Secret × N, ImagePullSecret × M`

Missing referents (dangling CM / Secret / PVC / SA / Secret-ref) render with
`TreeNode.Status = "missing"`, styled dim-red via `styles.StatusStyle`. Do
not omit them — surfacing the gap is the whole point of the view.
