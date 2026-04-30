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
| AI Copilot | `claude` CLI subprocess (Claude Code CLI; no SDK dependency) |

## Build & Run

```bash
go build ./...                        # check compilation
go run ./cmd/klens/               # run (requires kubeconfig)
go test ./...                         # run tests
# AI copilot requires Claude Code CLI installed and authenticated:
#   brew install claude  (or see claude.ai/code)
#   claude login
```

## Architecture

```
cmd/klens/main.go         → tea.NewProgram(app.New(), WithAltScreen, WithMouseCellMotion)
internal/app/model.go         → root Bubbletea model; routes all msgs; delegates to child panels
internal/cluster/manager.go   → multi-cluster kubeconfig; lazy clientsets per context
internal/k8s/
  informers.go                → SharedIndexInformer factory; sends ResourceUpdatedMsg to app
  resources.go                → ResourceDescriptor registry (all 30+ types + aliases)
  logs.go                     → multi-pod log fan-in via goroutine channels
  metrics.go                  → metrics-server REST polling; MetricsUpdatedMsg
  topology.go                 → ownerReference traversal; TreeNode builder
  operations.go               → delete/scale/rollout/drain/cordon
  portforward.go              → client-go SPDY port-forward (no subprocess)
  exec.go                     → remotecommand SPDY exec
internal/config/config.go     → persisted user preferences (namespace lists, last active namespace per cluster); stored at ~/.config/klens/config.json
internal/ai/
  client.go                   → shells out to `claude` CLI via os/exec; PlanCmd returns CopilotResponseMsg
  tools.go                    → toolDef schemas (one per klens operation); IsDestructive classifier
  context.go                  → CopilotStep, ClusterContext, BuildSystemPrompt
internal/ui/
  layout/layout.go            → panel sizing from terminal dimensions
  panels/                     → header, status_bar, nav_panel, resource_table, yaml_viewer, yaml_editor, log_viewer, topology_panel, metrics_panel, copilot_panel
  widgets/                    → sparkline, diff_viewer, tree_renderer, confirm_dialog, scale_dialog, namespace_picker, cluster_picker
  styles/styles.go            → Lipgloss style definitions (Kubernetes blue theme)
```

## Key Conventions

- **Bubbletea message flow**: Informers run in background goroutines; they send to `msgCh chan tea.Msg`; `WatchCmd` relays these to the Bubbletea loop. Never mutate model state outside `Update()`.
- **Lazy clientsets**: `cluster.Manager` creates a `*kubernetes.Clientset` on first use per context. On cluster switch, stop old `WatcherFactory` with `wf.Stop()` before creating a new one.
- **Content modes**: `app.Model.mode` (ModeTable, ModeYAML, ModeEditor, ModeLogs, ModeTopology, ModeMetrics, ModeCopilot) controls which panel `contentView()` renders.
- **Focus vs action keys**: `app.Model.focus` (FocusNav / FocusContent) controls which panel `↑↓/jk` navigate. Action keys (`y`, `e`, `l`, `t`, `m`, `d`) work regardless of focus — they always operate on the selected table row.
- **AI copilot**: shells out to the `claude` CLI binary; `ai.New()` returns an error if `claude` is not in `$PATH` — all other features work without it. No Anthropic SDK or API key needed; authentication is via `claude login`.
- **Metrics degradation**: if metrics-server not installed (404 on metrics API), show "n/a" — never block resource browsing.
- **lipgloss constraint**: `MarginLeft()` breaks `Width()` in lipgloss v1.1.0 — use `PaddingLeft()` for all indented panel elements.
- **YAML editor modal states**: `internal/ui/panels/yaml_editor.go` implements vim-style Normal/Insert/DiffConfirm/Applying states. In Normal mode both `hjkl` and arrow keys navigate; `i/a/A/o/O` enter Insert mode; `ctrl+s` opens diff preview. In Insert mode all input goes directly to the `textarea` widget.

## Keyboard Shortcuts

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
| `ctrl+a` | AI copilot |
| `ctrl+r` | reconnect / refresh (stops watcher, reruns full connect) |
| `:` | command palette (TODO) |
| `esc` | back to table |
| `q` | quit |

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
