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
  topology.go                 → ownerReference traversal; TreeNode builder
  operations.go               → delete/scale/rollout/drain/cordon
  portforward.go              → client-go SPDY port-forward (no subprocess)
  exec.go                     → remotecommand SPDY exec (pod attach)
  portforward.go              → SPDY port-forward sessions + PortForwardManager (active sessions registry)
internal/config/config.go     → persisted user preferences (namespace lists, last active namespace per cluster,
                                read_only flag); stored at ~/.config/klens/config.json
internal/ui/
  layout/layout.go            → panel sizing from terminal dimensions
  panels/                     → header, status_bar, nav_panel, resource_table, yaml_viewer, yaml_editor,
                                log_viewer, topology_panel, metrics_panel
  widgets/                    → sparkline, diff_viewer, tree_renderer, confirm_dialog, scale_dialog,
                                namespace_picker, cluster_picker
  styles/styles.go            → Lipgloss style definitions (Kubernetes blue theme)
```

## Key Conventions

- **Bubbletea message flow**: Informers run in background goroutines; they send to `msgCh chan tea.Msg`; `WatchCmd` relays these to the Bubbletea loop. Never mutate model state outside `Update()`.
- **Lazy clientsets**: `cluster.Manager` creates a `*kubernetes.Clientset` on first use per context. On cluster switch, stop old `WatcherFactory` with `wf.Stop()` before creating a new one.
- **Content modes**: `app.Model.mode` (ModeTable, ModeYAML, ModeEditor, ModeLogs, ModeTopology, ModeMetrics) controls which panel `contentView()` renders.
- **Focus vs action keys**: `app.Model.focus` (FocusNav / FocusContent) controls which panel `↑↓/jk` navigate. Action keys (`y`, `e`, `l`, `t`, `m`, `d`, `a`, `s`) work regardless of focus — they always operate on the selected table row.
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
| `y` | view YAML |
| `e` | edit YAML |
| `l` | logs (or multi-pod logs with space-selected rows) |
| `t` | topology |
| `m` | metrics |
| `d` | delete (with confirmation) |
| `a` | attach / exec into pod (tmux: new window; non-tmux: suspend TUI) |
| `s` | scale (Deployments / StatefulSets) |
| `shift+f` / `f` | port-forward (Pods) — modal dialog picks remote/local ports |
| `ctrl+f` | active port-forwards list (anywhere except ModeLogs) |
| `ctrl+r` | reconnect / refresh (stops watcher, reruns full connect) |
| `ctrl+n` | namespace picker |
| `ctrl+k` | cluster context picker |
| `ctrl+z` | rollback last YAML apply (in YAML view) |
| `ctrl+s` | YAML diff preview / save (in YAML editor) |
| `ctrl+v` | paste into filter / search / editor inputs |
| `←/→` | scroll horizontally on columns marked `Scrollable` (e.g. Event MESSAGE); horizontal trackpad wheel does the same |
| `:` | command palette (TODO) |
| `esc` | back to table (or peel log viewer state) |
| `q` | quit |

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

1. Add a `ResourceDescriptor` entry to `internal/k8s/resources.go` `Registry` slice
2. Add a `List*` method to `internal/k8s/informers.go` `WatcherFactory`
3. Add a `Build*Rows` function to `internal/ui/panels/resource_table.go`
4. Add a `case "Kind":` to `app.Model.listRows()` in `internal/app/model.go`
5. Add a `FetchObject` case in `internal/ui/panels/yaml_viewer.go` `fetchObject()`

## Adding Topology to a Resource

1. Set `SupportsTopology: true` in the `ResourceDescriptor` (drives the `t` hint in the status bar)
2. Add `Build*Topology(obj, wf *WatcherFactory) *TreeNode` to `internal/k8s/topology.go`
   - Ingress: `Ingress → Rule (host) → Route (path → svc:port) → Service → Pod`
   - Service: `Service → Pod` (via selector matching)
   - Deployment: `Deployment → ReplicaSet → Pod` (via OwnerReferences)
3. Add a `case "Kind":` to `app.Model.buildTopology()` in `internal/app/model.go`
