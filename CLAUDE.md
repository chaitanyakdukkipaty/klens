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
  layout/layout.go            → positioned panel rects (Rect{X,Y,Width,Height}) from terminal dimensions;
                                Header / Nav / TabBar / Content / Status / Fullscreen
  hit/hit.go                  → hit-region registry: zones (header/nav/tabbar/content/status) → local coords;
                                rebuilt per mouse event from the live layout (model.hitMap)
  keymap/keymap.go            → labels-only keybinding catalog (renders the ? overlay; dispatch stays in handlers)
  panels/                     → header (clickable chips + ☰), tab_bar (mode tabs), status_bar, nav_panel
                                (grouped, collapsible), resource_table, yaml_viewer, yaml_editor,
                                describe_viewer, log_viewer, xray_panel, metrics_panel
  widgets/                    → sparkline, diff_viewer, tree_renderer, confirm_dialog, scale_dialog,
                                namespace_picker, cluster_picker, app_menu, keybindings_overlay,
                                settings_overlay
  styles/styles.go            → semantic Palette + presets (klens-dark, catppuccin-mocha); exported styles
                                rebuilt by styles.Apply; derived package styles re-register via RegisterOnApply
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
- **Log viewer states**: `internal/ui/panels/log_viewer.go` has layered state — viewer-wide filterOn (/ input), searchOn (ctrl+f input), autoScroll, paused, wrap, and previous flags; per-group podFilter (1-9 solo, single-group mode only), drag-select, and search-match positions. Filter and search query apply to every tab/stripe (single `/` typing covers all groups); scrolling up in *any* group disables autoscroll *everywhere*. The `esc` peel order is: drag → search input → filter input → pod-solo → search query → filter query → split layout. `s` (pause) is deliberately not peeled by esc — the user resumes with `s`. While paused, incoming lines are buffered into `pendingLines` (capped at `maxLogLines`, drop-oldest) and the channel keeps draining via the root's self-rechained `ReadCmd`. `LogGroup` / `StartGrouped()` in `logs.go` carry the group name through `LogLine.Group` so the viewer can route lines to tabs. `LogStreamer.SetPrevious(true)` plumbs `PodLogOptions.Previous=true` (with `Follow=false`) through `streamContainer`; previous-logs streams are one-shot — the retry loop exits on natural EOF rather than reconnecting. The root model caches `m.logGroups` so `p` (LogPreviousToggleMsg) can rebuild a fresh streamer against the same composition without re-resolving from table selection.
- **JSON colorization**: `tryColorizeJSON` in `log_viewer.go` Chroma-highlights lines that are valid JSON (dracula theme, terminal256 formatter); colorCache is a parallel slice to `lines` so re-colorizing on `J` toggle only re-renders lines, not restreams data.
- **Scrollable columns**: `k8s.Column.Scrollable` is a per-column opt-in (parallel to `Flex`). At most one column per resource sets it; `ResourceTable` reads it through `scrollableColIdx()` and exposes `←/→`, horizontal-wheel ticks, and an `esc`-peel layer on `hScroll`. Adding the flag to any new resource's column is the only step required to enable horizontal scrolling.
- **Resource table scrollbar**: `ResourceTable.View()` reserves the rightmost inner column for a `renderScrollbar` thumb driven by `scrollStart()` over `len(t.filtered)` — works for both key navigation and mouse-wheel cursor moves. The title also appends a `i/N · P%` muted label via `cursorPositionLabel()`.
- **Row ordering & sort**: `ResourceTable.sortRows()` (called by `WithRows` and the sort keys) is the single sort site. Default order is k9s-parity: newest-first for time-stamped kinds (`ResourceRow.SortByTime`, only Event today), else natural `(namespace, name)` via `k8s.RowLess` — natural so `pod-2` precedes `pod-10`, with the fqn as a total-order tie-break so refreshes don't reorder. Interactive column sort: `shift+→` (alias `>`) cycles `sortColIdx` forward (−1 = default), `shift+←` cycles it backward (`cycleSortColumnBack`); `shift+↑` / `shift+↓` force ascending / descending on the active column (`setSortDir`), while `<` flips it (`toggleSortDir`); all reset on kind switch. (Shift, not ctrl: macOS reserves ctrl+arrows for Spaces / Mission Control, so they never reach the terminal.) Clicking a column header (`HandleHeaderClickAt`, routed from the left-click handler in `model.go` before drag-select) sorts by that column — a click on the already-active column toggles direction, a click on another column selects it ascending. `columnAtX` maps the click's inner-X to a column using `headerColTextOffset` (1 border + 1 `TableHeader` left-pad) and `computeColWidths`, folding each trailing separator into the preceding column's hit-area. The active column compares via `k8s.CellCompare(col.SortType, a, b)` (ANSI-stripped), tie-breaking back to `RowLess`. `k8s.Column.SortType` (`SortString` default / `SortTime` / `SortNumber` / `SortCapacity`) is a per-column opt-in — set it on a column to make the sort compare that column by duration/number/quantity instead of raw text; the header shows a `▲`/`▼` arrow and the title a `· sort COL ▲` label.
- **klog suppression**: klog is silenced at startup via `klog.SetOutput(io.Discard)` — suppress before any client-go initialization to avoid noisy stderr.
- **Mouse routing**: all clicks resolve through `m.hitMap().At(x, y)` → (zone, panel-local coords); never hand-compute offsets at a call site. Panels receive coordinates relative to their outer rect (the table additionally takes border-inner Y, i.e. `ly-1`). Drag continuation deliberately bypasses the zone gate (`m.contentRect().Local`) so in-flight drags keep tracking outside the panel.
- **Hover**: `tea.MouseModeAllMotion` + the main.go `tea.WithFilter` dropping motion events whose `resolveHover` target set is unchanged. Hover is render-only (header chips underline, tabs tint, nav/table rows background-tint via `Palette.Hover`); suppressed while loading, under modals, or when `disable_hover` is set in config.json.
- **Tab bar**: `panels.TabBar` is a pure value built per render/hit-test by `buildTabBar()`; `segments()` is shared by View and TabAt so pixels and click targets can't drift. Tab click = the matching action key on the selected row; Table tab = esc; unsupported tabs dim via capability interfaces; multi-selection dims every single-resource view (only Logs composes); the editor adds a ● badge on the YAML tab and dirty switches confirm via the `discard-edits` pending op.
- **Theming**: never hardcode hex in panels/widgets — add a semantic role to `styles.Palette`. Pre-built package-level styles derived from palette colors must be rebuilt inside a `styles.RegisterOnApply(func(){…})` hook or they go stale on a live theme switch (Settings → Theme).
- **Nav groups**: `kinds.Meta.Group` is required (Workloads / Network / Config / Storage / Access / Cluster / Helm); the shim copies it to `ResourceDescriptor.NavGroup` and the sidebar builds its collapsible sections from it. Group fold/unfold is self-contained — it never moves focus or exits the current mode. Only the active kind shows a live count/fault dot (no informers for inactive kinds).
- **Overlays on the modal stack**: long-lived overlays (☰ menu, keybindings, settings, pf list) register with the `keyOrMouse` / `keyPressOnly` Handles predicate so informer updates and ticks flow past while they're open; only short-lived fully-blocking dialogs (confirm, scale) take every message.
- **Settings persistence**: config.json keys `theme`, `read_only`, `disable_hover`; `widgets.SettingsChanged` carries the full snapshot, the model applies + saves. The `--readonly` flag forces the effective state and renders the settings row inert.

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
| `ctrl+z` | rollback last YAML apply (in YAML view); toggle faults filter (in Events table) |
| `ctrl+s` | YAML diff preview / save (in YAML editor) |
| `ctrl+v` | paste into filter / search / editor inputs |
| `←/→` | scroll horizontally on columns marked `Scrollable` (e.g. Event MESSAGE); horizontal trackpad wheel does the same |
| `shift+→` / `shift+←` | cycle the table sort column forward / backward (default → col 0 → … → last → default); `>` is a fallback alias for `shift+→` |
| `shift+↑` / `shift+↓` | sort the active column ascending / descending (no-op in default order); `<` flips direction |
| click column header | sort by that column; click again to toggle asc/desc |
| `:` | command palette (TODO) |
| `?` | keybindings overlay (from table; also via ☰ menu) |
| click `⎈ cluster ▾` / `ns ▾` | open cluster / namespace picker (header chips) |
| click `☰` | app menu: Keybindings, Settings (theme / read-only / hover) |
| click mode tab | switch view (Table·esc YAML·y Logs·l X-Ray·x Metrics·m Describe·d); `[⛶]` fullscreen |
| click/drag scrollbar | jump / drag-scroll the table |
| `esc` | back to table (or peel log viewer state) |
| `q` | quit |

The footer never lists y/l/x/m/d — those live in the tab bar. Sidebar groups:
`h/←` fold, `l/→` unfold, `enter`/`space` toggle on a header; cursor on a
header doesn't change the active kind.

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
| `↑↓` / `jk` / `pgup` / `pgdn` | scroll |
| `/` | filter lines (viewer-wide; hides non-matching across every tab/stripe) |
| `ctrl+f` | inline search (viewer-wide; highlights matches) |
| `n` / `N` | next / prev search match (focused group) |
| `s` | pause / resume streaming (buffered, capped at `maxLogLines`) |
| `a` | toggle autoscroll (viewer-wide; on-enable snaps every group to bottom) |
| `w` | toggle line wrap (ANSI-aware via `ansi.Wrap`) |
| `c` | copy focused group's visible (post-filter) lines to clipboard |
| `ctrl+s` | save focused group's visible lines to `$KLENS_DUMP_DIR/logs-<group>-<ts>.log` (default `~/.klens/dumps/`) |
| `p` | toggle previous-container logs (re-streams with `Previous=true`) |
| `1`–`9` | solo pod (single-group) or jump to tab (multi-group) |
| `0` | show all pods / return to first tab |
| `tab` | cycle tabs / stripe focus (multi-group mode) |
| `v` | cycle layout: tabs → horizontal split → vertical split → tabs |
| `J` | toggle JSON pretty-print + Chroma colorization |
| `F` | fullscreen |
| `esc` | peel state: drag → search input → filter input → pod-solo → search query → filter query → split layout → exit logs |

### Events view

The events table swaps the global help footer for an events-specific one
that drops the bindings the kind can't satisfy (`ctrl+d` / `e` / `a` — no
`Deleter` / `Applier` / `Attacher`) and adds three events-only keys. The
state these keys touch is event-only: it survives namespace switch but
resets on cluster switch and on kind switch (leaving Event).

| Key | Action |
|---|---|
| `ctrl+z` | toggle faults-only filter (rows whose `Type` ∈ {`Warning`, `Error`}); composes with `/` |
| `w` | toggle MESSAGE-column wrap (multi-line cells; disables `←/→` while on) |
| `o` | open the row's `InvolvedObject` in its own kind table; jumps cursor to the matching row |

Pressing `ctrl+d`, `e`, or `a` on an event row sets the status bar to
"not supported on Event" — explicit feedback in place of the silent no-op
that would otherwise reach the dispatch helpers.

## Adding a New Resource Type

Create `internal/k8s/kinds/<kind>.go`. Implement `Meta()` (including the
required `Group` — the sidebar category), `Columns()` (with a
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
or display name should diverge from the object's metadata. Two further
event-shaped optional interfaces — `FaultRowMarker` (opt into the table's
`ctrl+z` faults filter) and `InvolvedObjectResolver` (opt into the `o`
jump-to-target action) — exist for kinds whose rows have a fault/non-fault
predicate or reference another resource; today only Event implements them.
Register in the package `init()` via `register(k)`; the shim in
`kinds/shim.go` builds the legacy `k8s.ResourceDescriptor` from the Kind
and its capability satisfaction.

### Adding a Column

Append one `{Header, Width, Flex, Render}` to the kind's `Columns()` slice.
`Render` takes `(runtime.Object, k8s.RowContext)` and returns the cell string;
type-assert to the kind's typed Go shape inside. `RowContext.Metrics` and
`RowContext.PortForwardActive` are the only cross-cutting state available to
cells — add a field to `k8s.RowContext` only when a concrete column needs it.
Nothing else changes; `listVia` fills `Row.Values` positionally from each
Render closure. Set `SortType` (`SortTime` / `SortNumber` / `SortCapacity`) on
the column when its values aren't plain text — that's all interactive `>` sort
needs to compare it correctly; leaving it unset gives natural string order.

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
