package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/chaitanyak/klens/internal/cluster"
	appcfg "github.com/chaitanyak/klens/internal/config"
	k8sops "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/layout"
	"github.com/chaitanyak/klens/internal/ui/panels"
	"github.com/chaitanyak/klens/internal/ui/styles"
	"github.com/chaitanyak/klens/internal/ui/widgets"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unstructuredpkg "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ContentMode controls what is displayed in the content panel.
type ContentMode int

const (
	ModeTable ContentMode = iota
	ModeYAML
	ModeEditor
	ModeLogs
	ModeTopology
	ModeMetrics
)

// FocusTarget tracks which panel has keyboard focus.
type FocusTarget int

const (
	FocusNav FocusTarget = iota
	FocusContent
)

// Model is the root Bubbletea model.
type Model struct {
	layout          layout.Layout
	header          panels.Header
	nav             panels.NavPanel
	table           panels.ResourceTable
	yamlView        panels.YAMLViewer
	yamlEdit        panels.YAMLEditor
	logView         panels.LogViewer
	topology        panels.TopologyPanel
	metrics         panels.MetricsPanel
	confirm         widgets.ConfirmDialog
	scaleDialog     widgets.ScaleDialog
	namespacePicker widgets.NamespacePicker
	clusterPicker   widgets.ClusterPicker
	statusBar       panels.StatusBar
	focus           FocusTarget
	mode            ContentMode

	// Pending operation waiting for confirm dialog
	pendingOp pendingOpData

	// Cluster state
	clusterMgr        *cluster.Manager
	watcher           *k8sops.WatcherFactory
	logStreamer        *k8sops.LogStreamer
	metricsData       k8sops.MetricsUpdatedMsg
	msgCh             chan tea.Msg
	namespace         string
	clusterNamespaces []string // from cluster, may be empty if no permission
	appConfig         *appcfg.Config
	loading           bool
	reconnecting      bool
	statusMsg         string

	// readOnlyFlag is the value of the --readonly CLI flag (immutable after startup).
	readOnlyFlag bool
	// readOnly is the effective readonly state: readOnlyFlag || config.ReadOnly.
	// When true, all cluster-mutating operations are blocked.
	readOnly bool
}

// internal messages

type clusterReadyMsg struct {
	mgr               *cluster.Manager
	watcher           *k8sops.WatcherFactory
	ctx               string
	ns                string
	version           string
	clusterNamespaces []string
	appConfig         *appcfg.Config
}

type errMsg struct {
	err error
	ctx string // optional: cluster context name to show in header even on failure
}

type refreshMsg struct{}

// switchNamespaceMsg triggers a watcher restart for the new namespace.
type switchNamespaceMsg struct{ namespace string }

// New creates the initial app model. readOnly mirrors the --readonly CLI flag;
// the effective readonly state may also be set by the persisted config.
func New(readOnly bool) Model {
	ch := make(chan tea.Msg, 128)
	return Model{
		layout:          layout.New(80, 24),
		header:          panels.NewHeader(80).SetReadOnly(readOnly),
		nav:             panels.NewNavPanel(20, 22),
		table:           panels.NewResourceTable(60, 22),
		yamlView:        panels.NewYAMLViewer(60, 22),
		yamlEdit:        panels.NewYAMLEditor(60, 22),
		logView:         panels.NewLogViewer(60, 22),
		topology:        panels.NewTopologyPanel(60, 22),
		metrics:         panels.NewMetricsPanel(60, 22),
		confirm:         widgets.NewConfirmDialog(),
		scaleDialog:     widgets.NewScaleDialog(),
		namespacePicker: widgets.NewNamespacePicker(),
		clusterPicker:   widgets.NewClusterPicker(),
		statusBar:       panels.NewStatusBar(80),
		focus:           FocusNav,
		mode:            ModeTable,
		msgCh:           ch,
		namespace:       "default",
		loading:         true,
		readOnlyFlag:    readOnly,
		readOnly:        readOnly,
	}
}

// Init returns the first command.
func (m Model) Init() tea.Cmd {
	return m.connectCmd()
}

// Update handles all messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Namespace picker intercepts key events when visible (modal).
	if m.namespacePicker.IsVisible() {
		if _, ok := msg.(tea.KeyMsg); ok {
			var cmd tea.Cmd
			m.namespacePicker, cmd = m.namespacePicker.Update(msg)
			return m, cmd
		}
	}

	// Cluster picker intercepts key events when visible (modal).
	if m.clusterPicker.IsVisible() {
		if _, ok := msg.(tea.KeyMsg); ok {
			var cmd tea.Cmd
			m.clusterPicker, cmd = m.clusterPicker.Update(msg)
			return m, cmd
		}
	}

	// Confirm dialog intercepts all input when visible (modal).
	if m.confirm.IsVisible() {
		var cmd tea.Cmd
		m.confirm, cmd = m.confirm.Update(msg)
		return m, cmd
	}

	// Scale dialog intercepts all input when visible (modal).
	if m.scaleDialog.IsVisible() {
		var cmd tea.Cmd
		m.scaleDialog, cmd = m.scaleDialog.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.layout = m.layout.Update(msg.Width, msg.Height)
		m = m.resizePanels()
		return m, nil

	case clusterReadyMsg:
		m.loading = false
		m.reconnecting = false
		m.clusterMgr = msg.mgr
		m.watcher = msg.watcher
		m.namespace = msg.ns
		m.clusterNamespaces = msg.clusterNamespaces
		m.appConfig = msg.appConfig
		m.readOnly = m.readOnlyFlag || msg.appConfig.ReadOnly
		m.header = m.header.SetCluster(msg.ctx).SetNamespace(msg.ns).SetVersion(msg.version).SetReadOnly(m.readOnly)
		m.nav = m.nav.SetFocused(true)
		m.table = m.table.SetKind(m.nav.ActiveKind()).SetSyncing(true)
		m.setStatusBarKind(m.nav.ActiveKind())
		return m, tea.Batch(
			k8sops.WatchCmd(m.msgCh),
			m.buildTableCmd(),
			k8sops.MetricsTickCmd(),
		)

	case k8sops.CacheSyncedMsg:
		m.table = m.table.SetSyncing(false)
		return m, tea.Batch(k8sops.WatchCmd(m.msgCh), m.buildTableCmd())

	case switchNamespaceMsg:
		return m.switchNamespace(msg.namespace)

	case widgets.NamespacePickedMsg:
		if msg.Save && m.appConfig != nil && m.clusterMgr != nil {
			m.appConfig.AddNamespace(m.clusterMgr.ActiveContext(), msg.Namespace)
			_ = m.appConfig.Save()
		}
		return m, func() tea.Msg { return switchNamespaceMsg{namespace: msg.Namespace} }

	case widgets.NamespaceRemovedMsg:
		if m.appConfig != nil && m.clusterMgr != nil {
			m.appConfig.RemoveNamespace(m.clusterMgr.ActiveContext(), msg.Namespace)
			_ = m.appConfig.Save()
		}
		return m, nil

	case widgets.NamespacePickerCancelMsg:
		return m, nil

	case widgets.ClusterPickedMsg:
		return m.switchContext(msg.Context)

	case widgets.ClusterPickerCancelMsg:
		return m, nil

	case errMsg:
		m.loading = false
		if msg.ctx != "" {
			m.header = m.header.SetCluster(msg.ctx)
		}
		m.statusMsg = msg.err.Error()
		m.statusBar = m.statusBar.SetMessage(m.statusMsg)
		return m, nil

	case k8sops.ResourceUpdatedMsg:
		cmds := []tea.Cmd{k8sops.WatchCmd(m.msgCh)}
		if msg.Kind == m.nav.ActiveKind() && m.mode == ModeTable {
			cmds = append(cmds, m.buildTableCmd())
		}
		return m, tea.Batch(cmds...)

	case k8sops.AccessDeniedMsg:
		// Show the error immediately only if the user is currently viewing that resource.
		if msg.Kind == m.nav.ActiveKind() && m.mode == ModeTable {
			m.statusBar = m.statusBar.SetMessage("Access denied: cannot list " + msg.Kind)
		}
		return m, k8sops.WatchCmd(m.msgCh)

	case refreshMsg:
		rows := m.listRows(m.nav.ActiveKind())
		m.table = m.table.WithRows(rows)
		// Show access-denied when navigating to a forbidden resource; restore
		// the last operation status when navigating to an accessible one.
		if m.watcher != nil && m.watcher.IsAccessDenied(m.nav.ActiveKind()) {
			m.statusBar = m.statusBar.SetMessage("Access denied: cannot list " + m.nav.ActiveKind())
		} else {
			m.statusBar = m.statusBar.SetMessage(m.statusMsg)
		}
		return m, nil

	case k8sops.MetricsTick:
		if m.clusterMgr != nil {
			cs, _ := m.clusterMgr.ActiveClientset()
			if cs != nil {
				return m, k8sops.FetchMetricsCmd(cs, m.namespace, m.metricsData)
			}
		}
		return m, k8sops.MetricsTickCmd()

	case k8sops.MetricsUpdatedMsg:
		m.metricsData = msg
		if m.mode == ModeMetrics {
			var cmd tea.Cmd
			m.metrics, cmd = m.metrics.Update(msg)
			return m, tea.Batch(cmd, k8sops.MetricsTickCmd())
		}
		if m.mode == ModeTable && m.nav.ActiveKind() == "Pod" {
			m.table = m.table.WithRows(m.listRows("Pod"))
		}
		return m, k8sops.MetricsTickCmd()

	case k8sops.LogLineMsg:
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(msg)
		if m.mode == ModeLogs {
			return m, tea.Batch(cmd, m.logStreamer.ReadCmd())
		}
		return m, cmd

	case panels.YAMLFetchedMsg:
		if msg.Err != nil {
			m.statusBar = m.statusBar.SetMessage("yaml: " + msg.Err.Error())
			return m, nil
		}
		var cmd tea.Cmd
		m.yamlView, cmd = m.yamlView.Update(msg)
		m.mode = ModeYAML
		m.focus = FocusContent
		return m, cmd

	case panels.YAMLAppliedMsg:
		m.mode = ModeYAML
		m.statusBar = m.statusBar.SetMessage(fmt.Sprintf("Applied %s/%s", msg.Kind, msg.Name))
		return m, m.buildTableCmd()

	case panels.YAMLApplyErrMsg:
		m.statusBar = m.statusBar.SetMessage("apply error: " + msg.Err.Error())
		m.mode = ModeEditor
		return m, nil

	case k8sops.OperationResultMsg:
		if msg.Success {
			m.statusBar = m.statusBar.SetMessage(fmt.Sprintf("%s %s: done", msg.Operation, msg.Resource))
		} else {
			m.statusBar = m.statusBar.SetMessage(fmt.Sprintf("%s failed: %v", msg.Operation, msg.Err))
		}
		m.mode = ModeTable
		return m, m.buildTableCmd()

	case k8sops.AttachFinishedMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("attach %s: %v", msg.Pod, msg.Err)
		} else {
			m.statusMsg = fmt.Sprintf("attach session ended: %s", msg.Pod)
		}
		m.statusBar = m.statusBar.SetMessage(m.statusMsg)
		return m, nil

	case k8sops.TmuxWindowOpenedMsg:
		if msg.Err != nil {
			m.statusBar = m.statusBar.SetMessage("attach: " + msg.Err.Error())
		} else {
			m.statusMsg = fmt.Sprintf("attached to %s", msg.Session.Pod)
			m.statusBar = m.statusBar.SetMessage(m.statusMsg)
		}
		return m, nil

	case widgets.ConfirmResult:
		if msg.Confirmed {
			return m.executeConfirmedOp(msg)
		}
		return m, nil

	case widgets.ScaleResult:
		if !msg.Confirmed {
			return m, nil
		}
		cs, err := m.clusterMgr.ActiveClientset()
		if err != nil {
			m.statusBar = m.statusBar.SetMessage("no client: " + err.Error())
			return m, nil
		}
		return m, k8sops.ScaleCmd(cs, msg.Kind, msg.Name, msg.Namespace, msg.Replicas)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	// When nav filter input is open, route all keys to nav handler.
	if m.focus == FocusNav && m.nav.FilterActive() {
		prev := m.nav.ActiveKind()
		var cmd tea.Cmd
		m.nav, cmd = m.nav.Update(msg)
		if m.nav.ActiveKind() != prev {
			m.table = m.table.SetKind(m.nav.ActiveKind())
			m.setStatusBarKind(m.nav.ActiveKind())
			return m, tea.Batch(cmd, m.buildTableCmd())
		}
		return m, cmd
	}

	// Global keys work in any mode
	switch msg.String() {
	case "q", "ctrl+c":
		m.stopAll()
		return m, tea.Quit
	case "esc":
		// In the YAML editor's Insert mode, ESC switches to Normal — don't exit to table.
		if m.mode == ModeEditor && m.yamlEdit.IsInsertMode() {
			var cmd tea.Cmd
			m.yamlEdit, cmd = m.yamlEdit.Update(msg)
			return m, cmd
		}
		// If table has any filter (active or committed), let the table handle ESC to clear it.
		if m.mode == ModeTable && m.table.HasFilter() {
			var cmd tea.Cmd
			m.table, cmd = m.table.Update(msg)
			return m, cmd
		}
		if m.mode != ModeTable {
			m.mode = ModeTable
			if m.logStreamer != nil {
				m.logStreamer.Stop()
				m.logStreamer = nil
			}
			return m, nil
		}
		if m.focus == FocusContent {
			m.focus = FocusNav
			m.nav = m.nav.SetFocused(true)
			m.table = m.table.SetFocused(false).ClearSelection()
			return m, nil
		}
	case "ctrl+n":
		if m.clusterMgr != nil {
			clusterNs := m.clusterNamespaces
			if len(clusterNs) == 0 && m.watcher != nil {
				clusterNs = m.watcher.ListNamespaces()
			}
			savedNs := []string(nil)
			if m.appConfig != nil {
				savedNs = m.appConfig.SavedNamespaces(m.clusterMgr.ActiveContext())
			}
			m.namespacePicker = m.namespacePicker.Show(clusterNs, savedNs)
		}
		return m, nil
	case "ctrl+k":
		if m.clusterMgr != nil {
			m.clusterPicker = m.clusterPicker.Show(m.clusterMgr.Contexts(), m.clusterMgr.ActiveContext())
		}
		return m, nil
	case "ctrl+r":
		if m.watcher != nil {
			m.watcher.Stop()
		}
		if m.logStreamer != nil {
			m.logStreamer.Stop()
			m.logStreamer = nil
		}
		m.loading = true
		m.reconnecting = true
		m.mode = ModeTable
		return m, m.connectCmd()
	}

	// Mode-specific keys
	if m.mode == ModeTable {
		if m.focus == FocusContent {
			return m.handleTableKeys(msg)
		}
		// Action keys fire even when nav panel has focus (but not during filter input)
		if !m.nav.FilterActive() {
			switch msg.String() {
			case "y", "e", "l", "t", "m", "d", "a", "s":
				return m.handleTableKeys(msg)
			}
		}
	}

	if m.mode == ModeYAML {
		return m.handleYAMLViewKeys(msg)
	}

	if m.mode == ModeEditor {
		var cmd tea.Cmd
		m.yamlEdit, cmd = m.yamlEdit.Update(msg)
		return m, cmd
	}

	if m.mode == ModeLogs {
		var cmd tea.Cmd
		m.logView, cmd = m.logView.Update(msg)
		return m, cmd
	}

	if m.mode == ModeTopology {
		var cmd tea.Cmd
		m.topology, cmd = m.topology.Update(msg)
		return m, cmd
	}

	if m.mode == ModeMetrics {
		var cmd tea.Cmd
		m.metrics, cmd = m.metrics.Update(msg)
		return m, cmd
	}

	// Table + nav routing
	switch msg.String() {
	case "tab":
		if m.focus == FocusNav {
			m.focus = FocusContent
		} else {
			m.focus = FocusNav
			m.table = m.table.ClearSelection()
		}
		m.nav = m.nav.SetFocused(m.focus == FocusNav)
		m.table = m.table.SetFocused(m.focus == FocusContent)
		return m, nil
	case "enter", "right":
		if m.focus == FocusNav {
			m.focus = FocusContent
			m.nav = m.nav.SetFocused(false)
			m.table = m.table.SetFocused(true)
			return m, nil
		}
	case "left":
		if m.focus == FocusContent {
			m.focus = FocusNav
			m.nav = m.nav.SetFocused(true)
			m.table = m.table.SetFocused(false).ClearSelection()
			return m, nil
		}
	}

	var cmd tea.Cmd
	if m.focus == FocusNav {
		prev := m.nav.ActiveKind()
		m.nav, cmd = m.nav.Update(msg)
		if m.nav.ActiveKind() != prev {
			m.table = m.table.SetKind(m.nav.ActiveKind())
			m.setStatusBarKind(m.nav.ActiveKind())
			return m, tea.Batch(cmd, m.buildTableCmd())
		}
	} else {
		m.table, cmd = m.table.Update(msg)
	}
	return m, cmd
}

func (m Model) handleTableKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	// When filter input is open, all keys go to the table's filter handler.
	if m.table.FilterActive() {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	switch msg.String() {
	case "enter":
		m.focus = FocusContent
		m.nav = m.nav.SetFocused(false)
		m.table = m.table.SetFocused(true)
		return m, nil
	case "left":
		m.focus = FocusNav
		m.nav = m.nav.SetFocused(true)
		m.table = m.table.SetFocused(false).ClearSelection()
		return m, nil
	case "y":
		row := m.table.SelectedRow()
		if row != nil {
			if m.nav.ActiveKind() == "HelmRelease" {
				if u, ok := row.Raw.(*unstructuredpkg.Unstructured); ok {
					return m, panels.FetchHelmReleaseYAMLCmd(u)
				}
			} else if m.clusterMgr != nil {
				cs, _ := m.clusterMgr.ActiveClientset()
				if cs != nil {
					return m, panels.FetchYAMLCmd(cs, m.nav.ActiveKind(), row.Name, row.Namespace)
				}
			}
		}
	case "e":
		if m.readOnly {
			m.statusBar = m.statusBar.SetMessage("read-only mode – use 'y' to view YAML")
			return m, nil
		}
		row := m.table.SelectedRow()
		if row != nil {
			if m.nav.ActiveKind() == "HelmRelease" {
				if u, ok := row.Raw.(*unstructuredpkg.Unstructured); ok {
					return m, panels.FetchHelmReleaseYAMLCmd(u)
				}
			} else if m.clusterMgr != nil {
				cs, _ := m.clusterMgr.ActiveClientset()
				if cs != nil {
					return m, panels.FetchYAMLCmd(cs, m.nav.ActiveKind(), row.Name, row.Namespace)
				}
			}
		}
	case "l":
		kind := m.nav.ActiveKind()
		selectedNames := m.table.SelectedPods()
		var pods []string
		if m.watcher != nil {
			seen := make(map[string]bool)
			for _, name := range selectedNames {
				for _, p := range k8sops.ResolvePodNames(kind, name, m.namespace, m.watcher) {
					if !seen[p] {
						seen[p] = true
						pods = append(pods, p)
					}
				}
			}
		} else {
			pods = selectedNames
		}
		if len(pods) > 0 && m.clusterMgr != nil {
			cs, _ := m.clusterMgr.ActiveClientset()
			if cs != nil {
				if m.logStreamer != nil {
					m.logStreamer.Stop()
				}
				streamer := k8sops.NewLogStreamer(cs, m.namespace)
				streamer.Start(pods)
				m.logStreamer = streamer
				m.logView = m.logView.SetPods(pods)
				m.mode = ModeLogs
				m.focus = FocusContent
				return m, streamer.ReadCmd()
			}
		}
	case "t":
		row := m.table.SelectedRow()
		if row != nil && m.watcher != nil {
			tree := m.buildTopology(m.nav.ActiveKind(), row.Name)
			m.topology = m.topology.SetTree(m.nav.ActiveKind(), row.Name, tree)
			m.mode = ModeTopology
			m.focus = FocusContent
		}
	case "m":
		row := m.table.SelectedRow()
		if row != nil {
			key := row.Namespace + "/" + row.Name
			rm := m.metricsData.Pods[key]
			m.metrics = m.metrics.SetResource(row.Name, row.Namespace, rm)
			if m.watcher != nil {
				for _, pod := range m.watcher.ListPods(row.Namespace) {
					if pod.Name == row.Name {
						cpuReqM, cpuLimM, memReqB, memLimB := panels.PodResourceTotals(pod)
						m.metrics = m.metrics.SetLimits(cpuReqM, cpuLimM, memReqB, memLimB)
						break
					}
				}
			}
			m.mode = ModeMetrics
			m.focus = FocusContent
		}
	case "d":
		if m.readOnly {
			m.statusBar = m.statusBar.SetMessage("read-only mode")
			return m, nil
		}
		kind := m.nav.ActiveKind()
		rows := m.table.SelectedRows()
		if len(rows) > 0 {
			var targets []deleteTarget
			for _, row := range rows {
				targets = append(targets, deleteTarget{name: row.Name, namespace: row.Namespace})
			}
			var action, resource string
			if len(targets) == 1 {
				action, resource = "Delete", targets[0].name
			} else {
				action = fmt.Sprintf("Delete %d", len(targets))
				resource = kind + "s"
			}
			m.confirm = m.confirm.Show(action, resource)
			m.pendingOp = pendingOpData{op: "delete", kind: kind, targets: targets}
		}
	case "s":
		if m.readOnly {
			m.statusBar = m.statusBar.SetMessage("read-only mode")
			return m, nil
		}
		if m.nav.ActiveKind() == "HelmRelease" {
			row := m.table.SelectedRow()
			if row != nil {
				m.confirm = m.confirm.Show("Suspend", row.Name)
				m.pendingOp = pendingOpData{op: "suspend", kind: "HelmRelease", name: row.Name, namespace: row.Namespace}
			}
		} else {
			rd, _ := k8sops.Resolve(m.nav.ActiveKind())
			if rd.SupportsScale {
				row := m.table.SelectedRow()
				if row != nil {
					current := currentReplicas(row)
					m.scaleDialog = m.scaleDialog.Show(m.nav.ActiveKind(), row.Name, row.Namespace, current)
				}
			}
		}
	case "r":
		if m.readOnly {
			m.statusBar = m.statusBar.SetMessage("read-only mode")
			return m, nil
		}
		if m.nav.ActiveKind() == "HelmRelease" {
			row := m.table.SelectedRow()
			if row != nil {
				m.confirm = m.confirm.Show("Resume", row.Name)
				m.pendingOp = pendingOpData{op: "resume", kind: "HelmRelease", name: row.Name, namespace: row.Namespace}
			}
		}
	case "a":
		if m.readOnly {
			m.statusBar = m.statusBar.SetMessage("read-only mode")
			return m, nil
		}
		row := m.table.SelectedRow()
		if row == nil {
			m.statusBar = m.statusBar.SetMessage("no pod selected")
			break
		}
		if m.nav.ActiveKind() != "Pod" {
			m.statusBar = m.statusBar.SetMessage("attach only available for Pods")
			break
		}
		var container string
		if pod, ok := row.Raw.(*corev1.Pod); ok && len(pod.Spec.Containers) > 0 {
			container = pod.Spec.Containers[0].Name
		}
		if os.Getenv("TMUX") != "" {
			return m, k8sops.TmuxAttachWindowCmd(row.Namespace, row.Name, container)
		}
		// Non-tmux fallback: suspend TUI and exec directly.
		if m.clusterMgr == nil {
			break
		}
		cs, err := m.clusterMgr.ActiveClientset()
		if err != nil || cs == nil {
			m.statusBar = m.statusBar.SetMessage("no cluster connection")
			break
		}
		cfg, err := m.clusterMgr.ActiveRestConfig()
		if err != nil {
			m.statusBar = m.statusBar.SetMessage("rest config: " + err.Error())
			break
		}
		return m, k8sops.AttachCmd(cs, cfg, row.Namespace, row.Name)
	}
	// Pass remaining keys to table
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) handleYAMLViewKeys(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "e":
		if m.readOnly {
			m.statusBar = m.statusBar.SetMessage("read-only mode")
			return m, nil
		}
		kind, name, ns := m.yamlView.ResourceInfo()
		m.yamlEdit = m.yamlEdit.LoadYAML(kind, name, ns, m.yamlView.RawYAML())
		m.mode = ModeEditor
		return m, nil
	}
	var cmd tea.Cmd
	m.yamlView, cmd = m.yamlView.Update(msg)
	return m, cmd
}

// deleteTarget holds identifying info for one resource to be deleted.
type deleteTarget struct {
	name      string
	namespace string
}

// pendingOpData stores what operation the confirm dialog is for.
type pendingOpData struct {
	op        string
	kind      string
	name      string         // single-resource ops (scale, suspend, etc.)
	namespace string
	targets   []deleteTarget // multi-delete
}

func (m Model) executeConfirmedOp(result widgets.ConfirmResult) (Model, tea.Cmd) {
	cs, err := m.clusterMgr.ActiveClientset()
	if err != nil {
		m.statusBar = m.statusBar.SetMessage("no client: " + err.Error())
		return m, nil
	}
	switch m.pendingOp.op {
	case "delete":
		m.table = m.table.ClearSelection()
		var cmds []tea.Cmd
		for _, t := range m.pendingOp.targets {
			cmds = append(cmds, k8sops.DeleteCmd(cs, m.pendingOp.kind, t.name, t.namespace))
		}
		return m, tea.Batch(cmds...)
	case "suspend":
		if m.watcher != nil {
			return m, k8sops.SuspendHelmReleaseCmd(m.watcher.DynamicClient(), m.watcher.HelmReleaseGVR(), m.pendingOp.name, m.pendingOp.namespace)
		}
	case "resume":
		if m.watcher != nil {
			return m, k8sops.ResumeHelmReleaseCmd(m.watcher.DynamicClient(), m.watcher.HelmReleaseGVR(), m.pendingOp.name, m.pendingOp.namespace)
		}
	}
	return m, nil
}

func (m Model) switchNamespace(ns string) (Model, tea.Cmd) {
	if m.watcher != nil {
		m.watcher.Stop()
	}
	if m.logStreamer != nil {
		m.logStreamer.Stop()
		m.logStreamer = nil
	}
	m.namespace = ns
	m.header = m.header.SetNamespace(ns)
	m.mode = ModeTable
	m.focus = FocusNav
	m.table = m.table.ClearSelection()

	if m.appConfig != nil {
		m.appConfig.SetLastNamespace(m.clusterMgr.ActiveContext(), ns)
		_ = m.appConfig.Save()
	}

	cs, err := m.clusterMgr.ActiveClientset()
	if err != nil {
		m.statusBar = m.statusBar.SetMessage("namespace switch: " + err.Error())
		return m, nil
	}
	nsCfg, _ := m.clusterMgr.ActiveRestConfig()
	wf := k8sops.NewWatcherFactory(cs, nsCfg, ns, m.msgCh)
	wf.Start()
	m.watcher = wf
	m.table = m.table.SetSyncing(true)
	return m, tea.Batch(
		k8sops.WatchCmd(m.msgCh),
		m.buildTableCmd(),
	)
}

func (m Model) switchContext(ctx string) (Model, tea.Cmd) {
	if m.watcher != nil {
		m.watcher.Stop()
	}
	if m.logStreamer != nil {
		m.logStreamer.Stop()
		m.logStreamer = nil
	}
	m.mode = ModeTable
	m.focus = FocusNav

	if err := m.clusterMgr.SwitchContext(ctx); err != nil {
		m.statusBar = m.statusBar.SetMessage("context switch: " + err.Error())
		return m, nil
	}

	ns := m.clusterMgr.ActiveNamespace()
	if m.appConfig != nil {
		if saved := m.appConfig.GetLastNamespace(ctx); saved != "" {
			ns = saved
		}
	}
	m.namespace = ns
	m.header = m.header.SetCluster(ctx).SetNamespace(ns)
	m.clusterNamespaces = nil

	cs, err := m.clusterMgr.ActiveClientset()
	if err != nil {
		m.statusBar = m.statusBar.SetMessage("context switch: " + err.Error())
		return m, nil
	}
	restCfg, _ := m.clusterMgr.ActiveRestConfig()
	wf := k8sops.NewWatcherFactory(cs, restCfg, ns, m.msgCh)
	wf.Start()
	m.watcher = wf
	m.table = m.table.SetSyncing(true)
	return m, tea.Batch(
		k8sops.WatchCmd(m.msgCh),
		m.buildTableCmd(),
		k8sops.MetricsTickCmd(),
	)
}

func (m Model) buildTopology(kind, name string) *k8sops.TreeNode {
	if m.watcher == nil {
		return nil
	}
	switch kind {
	case "Deployment":
		deps := m.watcher.ListDeployments(m.namespace)
		for _, d := range deps {
			if d.Name == name {
				return k8sops.BuildDeploymentTopology(d, m.watcher)
			}
		}
	case "Service":
		svcs := m.watcher.ListServices(m.namespace)
		for _, s := range svcs {
			if s.Name == name {
				return k8sops.BuildServiceTopology(s, m.watcher)
			}
		}
	case "Ingress":
		ings := m.watcher.ListIngresses(m.namespace)
		for _, ing := range ings {
			if ing.Name == name {
				return k8sops.BuildIngressTopology(ing, m.watcher)
			}
		}
	}
	return nil
}

func (m *Model) setStatusBarKind(kind string) {
	m.statusBar = m.statusBar.SetActiveKind(kind)
	rd, _ := k8sops.Resolve(kind)
	help := []panels.HelpItem{
		{Key: "↑↓/jk", Desc: "navigate"},
		{Key: "enter", Desc: "select"},
		{Key: "/", Desc: "filter"},
	}
	if rd.SupportsYAML {
		help = append(help, panels.HelpItem{Key: "y", Desc: "yaml"})
		if !m.readOnly {
			help = append(help, panels.HelpItem{Key: "e", Desc: "edit"})
		}
	}
	if rd.SupportsLogs {
		help = append(help, panels.HelpItem{Key: "l", Desc: "logs"})
	}
	if rd.SupportsTopology {
		help = append(help, panels.HelpItem{Key: "t", Desc: "topology"})
	}
	if rd.SupportsMetrics {
		help = append(help, panels.HelpItem{Key: "m", Desc: "metrics"})
	}
	if !m.readOnly {
		if rd.SupportsAttach {
			help = append(help, panels.HelpItem{Key: "a", Desc: "attach"})
		}
		if rd.SupportsScale {
			help = append(help, panels.HelpItem{Key: "s", Desc: "scale"})
		}
		if kind == "HelmRelease" {
			help = append(help,
				panels.HelpItem{Key: "s", Desc: "suspend"},
				panels.HelpItem{Key: "r", Desc: "resume"},
			)
		}
		if rd.SupportsDeletion || kind == "HelmRelease" {
			help = append(help, panels.HelpItem{Key: "d", Desc: "delete"})
		}
	}
	help = append(help,
		panels.HelpItem{Key: "ctrl+r", Desc: "refresh"},
		panels.HelpItem{Key: "q", Desc: "quit"},
	)
	m.statusBar = m.statusBar.SetHelp(help)
}

// currentReplicas extracts the replica count from the raw object in a ResourceRow.
func currentReplicas(row *k8sops.ResourceRow) int32 {
	switch raw := row.Raw.(type) {
	case *appsv1.Deployment:
		if raw.Spec.Replicas != nil {
			return *raw.Spec.Replicas
		}
	case *appsv1.StatefulSet:
		if raw.Spec.Replicas != nil {
			return *raw.Spec.Replicas
		}
	case *appsv1.ReplicaSet:
		if raw.Spec.Replicas != nil {
			return *raw.Spec.Replicas
		}
	}
	return 1
}

// View renders the full TUI.
func (m Model) View() string {
	if m.loading {
		return renderLoading(m.layout, m.reconnecting)
	}

	if m.namespacePicker.IsVisible() {
		return m.baseView() + "\n" + m.namespacePicker.View()
	}

	if m.clusterPicker.IsVisible() {
		return m.baseView() + "\n" + m.clusterPicker.View()
	}

	if m.confirm.IsVisible() {
		return m.baseView() + "\n" + m.confirm.View()
	}

	if m.scaleDialog.IsVisible() {
		return m.baseView() + "\n" + m.scaleDialog.View()
	}

	return m.baseView()
}

func (m Model) baseView() string {
	navView := m.nav.View()
	contentView := m.contentView()
	middle := layout.JoinPanels(navView, contentView)
	return lipgloss.JoinVertical(lipgloss.Left,
		m.header.View(),
		middle,
		m.statusBar.View(),
	)
}

func (m Model) contentView() string {
	switch m.mode {
	case ModeYAML:
		return m.yamlView.View()
	case ModeEditor:
		return m.yamlEdit.View()
	case ModeLogs:
		return m.logView.View()
	case ModeTopology:
		return m.topology.View()
	case ModeMetrics:
		return m.metrics.View()
	default:
		return m.table.View()
	}
}

func renderLoading(l layout.Layout, reconnecting bool) string {
	text := "  ⠋ Connecting to cluster…"
	if reconnecting {
		text = "  ⠋ Reconnecting…"
	}
	return styles.Primary.Render(text) + strings.Repeat("\n", l.Content().Height)
}

func (m Model) resizePanels() Model {
	navDim := m.layout.Nav()
	contentDim := m.layout.Content()
	m.header = m.header.SetWidth(m.layout.Header().Width)
	m.statusBar = m.statusBar.SetWidth(m.layout.Status().Width)
	m.nav = m.nav.SetSize(navDim.Width, navDim.Height)
	m.table = m.table.SetSize(contentDim.Width, contentDim.Height)
	m.yamlView = m.yamlView.SetSize(contentDim.Width, contentDim.Height)
	m.yamlEdit = m.yamlEdit.SetSize(contentDim.Width, contentDim.Height)
	m.logView = m.logView.SetSize(contentDim.Width, contentDim.Height)
	m.topology = m.topology.SetSize(contentDim.Width, contentDim.Height)
	m.metrics = m.metrics.SetSize(contentDim.Width, contentDim.Height)
	return m
}

func (m Model) connectCmd() tea.Cmd {
	ch := m.msgCh
	return func() tea.Msg {
		mgr, err := cluster.New()
		if err != nil {
			return errMsg{err: fmt.Errorf("kubeconfig: %w", err)}
		}
		cs, err := mgr.ActiveClientset()
		if err != nil {
			return errMsg{err: fmt.Errorf("connect: %w", err), ctx: mgr.ActiveContext()}
		}
		ns := mgr.ActiveNamespace()

		connectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// ServerVersion proves API server is reachable; no context API, use goroutine.
		type svResult struct {
			version string
			err     error
		}
		svCh := make(chan svResult, 1)
		go func() {
			sv, e := cs.Discovery().ServerVersion()
			if e != nil {
				svCh <- svResult{err: e}
				return
			}
			svCh <- svResult{version: sv.GitVersion}
		}()
		var serverVersion string
		select {
		case r := <-svCh:
			if r.err != nil {
				return errMsg{err: fmt.Errorf("api server unreachable: %w", r.err), ctx: mgr.ActiveContext()}
			}
			serverVersion = r.version
		case <-connectCtx.Done():
			return errMsg{err: fmt.Errorf("api server unreachable: connection timed out"), ctx: mgr.ActiveContext()}
		}

		// Try to list namespaces — silently degrade if the user lacks permission.
		var clusterNamespaces []string
		if nsList, nsErr := cs.CoreV1().Namespaces().List(connectCtx, metav1.ListOptions{}); nsErr == nil {
			for _, n := range nsList.Items {
				clusterNamespaces = append(clusterNamespaces, n.Name)
			}
		}

		cfg, _ := appcfg.Load()
		if cfg == nil {
			cfg = &appcfg.Config{
				Namespaces:    make(map[string][]string),
				LastNamespace: make(map[string]string),
			}
		}
		if saved := cfg.GetLastNamespace(mgr.ActiveContext()); saved != "" {
			ns = saved
		}

		connCfg, _ := mgr.ActiveRestConfig()
		wf := k8sops.NewWatcherFactory(cs, connCfg, ns, ch)
		wf.Start()

		return clusterReadyMsg{
			mgr:               mgr,
			watcher:           wf,
			ctx:               mgr.ActiveContext(),
			ns:                ns,
			version:           serverVersion,
			clusterNamespaces: clusterNamespaces,
			appConfig:         cfg,
		}
	}
}

func (m Model) buildTableCmd() tea.Cmd {
	return func() tea.Msg { return refreshMsg{} }
}

func (m Model) listRows(kind string) []k8sops.ResourceRow {
	if m.watcher == nil {
		return nil
	}
	ns := m.namespace
	switch kind {
	case "Pod":
		return panels.BuildPodRows(m.watcher.ListPods(ns), m.metricsData)
	case "Deployment":
		return panels.BuildDeploymentRows(m.watcher.ListDeployments(ns))
	case "StatefulSet":
		return panels.BuildStatefulSetRows(m.watcher.ListStatefulSets(ns))
	case "DaemonSet":
		return panels.BuildDaemonSetRows(m.watcher.ListDaemonSets(ns))
	case "ReplicaSet":
		return panels.BuildReplicaSetRows(m.watcher.ListReplicaSets(ns))
	case "Job":
		return panels.BuildJobRows(m.watcher.ListJobs(ns))
	case "CronJob":
		return panels.BuildCronJobRows(m.watcher.ListCronJobs(ns))
	case "Service":
		return panels.BuildServiceRows(m.watcher.ListServices(ns))
	case "Ingress":
		return panels.BuildIngressRows(m.watcher.ListIngresses(ns))
	case "ConfigMap":
		return panels.BuildConfigMapRows(m.watcher.ListConfigMaps(ns))
	case "Secret":
		return panels.BuildSecretRows(m.watcher.ListSecrets(ns))
	case "Node":
		return panels.BuildNodeRows(m.watcher.ListNodes())
	case "PersistentVolumeClaim":
		return panels.BuildPVCRows(m.watcher.ListPVCs(ns))
	case "PersistentVolume":
		return panels.BuildPVRows(m.watcher.ListPersistentVolumes())
	case "Event":
		return panels.BuildEventRows(m.watcher.ListEvents(ns))
	case "HelmRelease":
		return panels.BuildHelmReleaseRows(m.watcher.ListHelmReleases(ns))
	default:
		return nil
	}
}

func (m *Model) stopAll() {
	if m.watcher != nil {
		m.watcher.Stop()
	}
	if m.logStreamer != nil {
		m.logStreamer.Stop()
	}
}



