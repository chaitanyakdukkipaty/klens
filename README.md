# klens

Kubernetes TUI with k9s-style keyboard navigation and OpenLens-class visual richness — topology trees, metrics sparklines, multi-pod log streaming, a YAML editor with diff preview, and a built-in AI copilot. Pure terminal. No Electron. No WebView.

## Features

- **Resource browser** — 30+ Kubernetes resource types with filterable, sortable tables
- **YAML viewer & editor** — syntax-highlighted viewer; vim-style editor with diff preview before applying (`ctrl+s`)
- **Log streaming** — multi-pod fan-in; space-select multiple pods and stream all logs at once
- **Topology trees** — visual ownerReference traversal: `Ingress → Service → Pod`, `Deployment → ReplicaSet → Pod`
- **Metrics panel** — ASCII sparklines for CPU and memory via metrics-server (degrades gracefully if not installed)
- **AI copilot** — natural language cluster operations via Claude Code CLI (`ctrl+a`); no API key required
- **Multi-cluster** — lazy clientset per kubeconfig context; namespace preferences persisted per cluster

## Install

```bash
go install github.com/chaitanyakdukkipaty/klens/cmd/klens@latest
```

Or build from source:

```bash
git clone git@github.com:chaitanyakdukkipaty/klens.git
cd klens
go build -o klens ./cmd/klens/
```

## Usage

```bash
klens
```

Requires a valid `~/.kube/config`. Switches clusters with the cluster picker; `ctrl+r` reconnects.

### AI copilot (optional)

The copilot shells out to the `claude` CLI — no Anthropic SDK or API key needed.

```bash
brew install claude   # or see claude.ai/code
claude login
```

All other features work without it.

## Keyboard shortcuts

| Key | Action |
|---|---|
| `tab` | cycle focus: nav ↔ content |
| `enter` | focus resource table |
| `↑↓` / `jk` | navigate |
| `/` | filter |
| `y` | view YAML |
| `e` | edit YAML |
| `l` | stream logs (multi-pod with `space`-select) |
| `t` | topology tree |
| `m` | metrics |
| `d` | delete (with confirmation) |
| `ctrl+a` | AI copilot |
| `ctrl+r` | reconnect / refresh |
| `esc` | back to table |
| `q` | quit |

## Configuration

Preferences (last active namespace per cluster, namespace lists) are stored at `~/.config/klens/config.json`.

## Tech stack

| Layer | Library |
|---|---|
| TUI | [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) |
| Styling | [charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) |
| Kubernetes | [k8s.io/client-go](https://github.com/kubernetes/client-go) v0.36.0 |
| Syntax highlight | [alecthomas/chroma](https://github.com/alecthomas/chroma) |
| Charts | [guptarohit/asciigraph](https://github.com/guptarohit/asciigraph) |

## License

MIT
