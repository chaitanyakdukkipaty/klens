package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/chaitanyak/klens/internal/cluster"
	appcfg "github.com/chaitanyak/klens/internal/config"
	k8sops "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/k8s/kinds"
	"github.com/chaitanyak/klens/internal/ui/hit"
	"github.com/chaitanyak/klens/internal/ui/layout"
	"github.com/chaitanyak/klens/internal/ui/modes"
	"github.com/chaitanyak/klens/internal/ui/panels"
	"github.com/chaitanyak/klens/internal/ui/styles"
	"github.com/chaitanyak/klens/internal/ui/widgets"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	unstructuredpkg "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// ContentMode controls what is displayed in the content panel.
type ContentMode int

const (
	ModeTable ContentMode = iota
	ModeYAML
	ModeEditor
	ModeLogs
	ModeXRay
	ModeMetrics
	ModeDescribe
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
	tableCtrl       modes.TableController
	yamlViewCtrl    modes.YAMLViewController
	yamlEditCtrl    modes.YAMLEditController
	logsCtrl        modes.LogsController
	xrayCtrl        modes.XRayController
	metricsCtrl     modes.MetricsController
	describeCtrl    modes.DescribeViewController
	confirm         widgets.ConfirmDialog
	scaleDialog     widgets.ScaleDialog
	sanitizeDialog  widgets.SanitizeDialog
	namespacePicker widgets.NamespacePicker
	clusterPicker   widgets.ClusterPicker
	containerPicker widgets.ContainerPicker
	contextMenu     widgets.ContextMenu
	pfDialog        widgets.PortForwardDialog
	pfList          widgets.PortForwardList
	statusBar       panels.StatusBar
	focus           FocusTarget
	mode            ContentMode

	// fullScreen, when true, hides header / nav / status and renders the
	// active content panel using the entire terminal. Toggled by `F` in
	// ModeYAML / ModeEditor / ModeLogs / ModeXRay / ModeMetrics. Reset to
	// false whenever the user returns to ModeTable.
	fullScreen bool

	// Pending operation waiting for confirm dialog
	pendingOp pendingOpData

	// Cluster state
	clusterMgr        *cluster.Manager
	watcher           *k8sops.WatcherFactory
	logStreamer       *k8sops.LogStreamer
	// logGroups is the pod-set composition that was streamed into the active
	// log session. Cached so `p` (previous-container logs) can rebuild a
	// fresh LogStreamer against the same pods/groups without re-resolving
	// from the table selection — which may have moved on.
	logGroups         []k8sops.LogGroup
	pfManager         *k8sops.PortForwardManager
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

	// syncing is true between a namespace/context switch and the first CacheSyncedMsg,
	// preventing stale informer data from populating the table during the transition.
	syncing bool

	// rollback stash: saved after a successful YAML apply so ctrl+z can revert.
	rollbackYAML string
	rollbackKind string
	rollbackName string
	rollbackNS   string

	// Events view state. Both flags are events-only and reset on kind switch
	// (leaving Event) and on cluster switch; they survive namespace switch.
	// eventFaultsOnly drives the ctrl+z faults-only filter; eventWrapMessage
	// drives the `w` MESSAGE-wrap toggle.
	eventFaultsOnly  bool
	eventWrapMessage bool

	// pendingJumpName, when non-empty after a switchKind triggered by `o` on
	// an Event row, asks the next buildTableCmd/refresh cycle to move the
	// table cursor to the row whose Name matches. Cleared on successful
	// match or after two refresh cycles have passed.
	pendingJumpName  string
	pendingJumpKind  string
	pendingJumpTries int
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

type clearStatusMsg struct{}

// switchNamespaceMsg triggers a watcher restart for the new namespace.
type switchNamespaceMsg struct{ namespace string }

func clearStatusAfterDelay(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return clearStatusMsg{}
	}
}

// New creates the initial app model. readOnly mirrors the --readonly CLI flag;
// the effective readonly state may also be set by the persisted config.
func New(readOnly bool) Model {
	ch := make(chan tea.Msg, 128)
	return Model{
		layout:          layout.New(80, 24),
		header:          panels.NewHeader(80).SetReadOnly(readOnly),
		nav:             panels.NewNavPanel(20, 22),
		tableCtrl:       modes.NewTableController(panels.NewResourceTable(60, 22)),
		yamlViewCtrl:    modes.NewYAMLViewController(panels.NewYAMLViewer(60, 22)),
		yamlEditCtrl:    modes.NewYAMLEditController(panels.NewYAMLEditor(60, 22)),
		logsCtrl:        modes.NewLogsController(panels.NewLogViewer(60, 22)),
		xrayCtrl:        modes.NewXRayController(panels.NewXRayPanel(60, 22)),
		metricsCtrl:     modes.NewMetricsController(panels.NewMetricsPanel(60, 22)),
		describeCtrl:    modes.NewDescribeViewController(panels.NewDescribeViewer(60, 22)),
		confirm:         widgets.NewConfirmDialog(),
		scaleDialog:     widgets.NewScaleDialog(),
		sanitizeDialog:  widgets.NewSanitizeDialog(),
		namespacePicker: widgets.NewNamespacePicker(),
		clusterPicker:   widgets.NewClusterPicker(),
		containerPicker: widgets.NewContainerPicker(),
		contextMenu:     widgets.NewContextMenu(),
		pfDialog:        widgets.NewPortForwardDialog(),
		pfList:          widgets.NewPortForwardList(),
		pfManager:       k8sops.NewPortForwardManager(),
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
	// Modals intercept input first. The stack order (namespace picker,
	// cluster picker, confirm, scale, context menu) matches the historical
	// priority of the if-block ladder this replaced. See modals.go.
	if cmd, handled := m.modals().Dispatch(msg); handled {
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
		m.statusMsg = ""
		m.statusBar = m.statusBar.SetMessage("")
		m.clusterMgr = msg.mgr
		m.watcher = msg.watcher
		m.namespace = msg.ns
		m.clusterNamespaces = msg.clusterNamespaces
		m.appConfig = msg.appConfig
		m.readOnly = m.readOnlyFlag || msg.appConfig.ReadOnly
		m.syncing = true
		m.header = m.header.SetCluster(msg.ctx).SetNamespace(msg.ns).SetVersion(msg.version).SetReadOnly(m.readOnly)
		m.nav = m.nav.SetFocused(true)
		m.tableCtrl = m.tableCtrl.SetKind(m.nav.ActiveKind()).SetSyncing(true)
		m.setStatusBarKind(m.nav.ActiveKind())
		return m, tea.Batch(
			k8sops.WatchCmd(m.msgCh),
			m.buildTableCmd(),
			k8sops.MetricsTickCmd(),
		)

	// (kindSyncing/setKindAndSync helpers are declared below near setStatusBarKind)

	case k8sops.CacheSyncedMsg:
		m.syncing = false
		// Pods are synced (the primary informer drives this message). Update the
		// table's syncing badge based on whether the *currently active* kind is
		// itself synced — Pod is always true here, but if the user resumed on a
		// non-Pod kind whose informer is still loading, the badge stays on.
		m.tableCtrl = m.tableCtrl.SetSyncing(m.kindSyncing(m.nav.ActiveKind()))
		return m, tea.Batch(k8sops.WatchCmd(m.msgCh), m.buildTableCmd())

	case k8sops.KindSyncedMsg:
		// A non-primary informer finished its initial LIST. Refresh the table
		// only if the user is currently looking at that kind — otherwise the
		// next ResourceUpdatedMsg from the coalesce loop will pick it up.
		if m.mode == ModeTable && m.nav.ActiveKind() == msg.Kind {
			m.tableCtrl = m.tableCtrl.SetSyncing(false)
			return m, tea.Batch(k8sops.WatchCmd(m.msgCh), m.buildTableCmd())
		}
		return m, k8sops.WatchCmd(m.msgCh)

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

	case widgets.ContainerPickedMsg:
		return m.attachToContainer(msg.Namespace, msg.Pod, msg.Container)

	case widgets.ContainerPickerCancelMsg:
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
		rows = m.maybeFaultsFilter(rows)
		m.tableCtrl = m.tableCtrl.WithRows(rows)
		m = m.maybeApplyPendingJump()
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

	case panels.LogAutoScrollTickMsg:
		if m.mode != ModeLogs || !m.logsCtrl.IsDragging() {
			return m, nil
		}
		m.logsCtrl = m.logsCtrl.AutoScrollStep()
		return m, panels.LogAutoScrollTickCmd()

	case panels.YAMLAutoScrollTickMsg:
		if m.mode != ModeYAML || !m.yamlViewCtrl.IsDragging() {
			return m, nil
		}
		m.yamlViewCtrl = m.yamlViewCtrl.AutoScrollStep()
		return m, panels.YAMLAutoScrollTickCmd()

	case panels.TableAutoScrollTickMsg:
		if m.mode != ModeTable || !m.tableCtrl.IsDragging() {
			return m, nil
		}
		m.tableCtrl = m.tableCtrl.AutoScrollStep()
		return m, panels.TableAutoScrollTickCmd()

	case k8sops.MetricsUpdatedMsg:
		m.metricsData = msg
		if m.mode == ModeMetrics {
			next, cmd := m.metricsCtrl.Update(msg)
			m.metricsCtrl = next.(modes.MetricsController)
			return m, tea.Batch(cmd, k8sops.MetricsTickCmd())
		}
		if m.mode == ModeTable && m.nav.ActiveKind() == "Pod" {
			// Patch only the metrics-related Values + Status on existing rows;
			// no sort, no filter recompute. New pods that appear between ticks
			// will arrive via the regular ResourceUpdatedMsg path.
			m.tableCtrl = m.tableCtrl.PatchValuesByName(m.listRows("Pod"))
		}
		return m, k8sops.MetricsTickCmd()

	case k8sops.LogLineMsg:
		next, cmd := m.logsCtrl.Update(msg)
		m.logsCtrl = next.(modes.LogsController)
		if m.mode == ModeLogs {
			return m, tea.Batch(cmd, m.logStreamer.ReadCmd())
		}
		return m, cmd

	case panels.LogPreviousToggleMsg:
		// Flip the streamer's Previous flag, re-seeding the viewer with empty
		// lines so the new source doesn't visually concatenate with the old.
		if m.mode != ModeLogs || m.clusterMgr == nil || len(m.logGroups) == 0 {
			return m, nil
		}
		cs, err := m.clusterMgr.ActiveClientset()
		if err != nil || cs == nil {
			m.statusBar = m.statusBar.SetMessage("logs: no client")
			return m, nil
		}
		var nextPrev bool
		if m.logStreamer != nil {
			nextPrev = !m.logStreamer.Previous()
			m.logStreamer.Stop()
		}
		streamer := k8sops.NewLogStreamer(cs, m.namespace)
		streamer.SetPrevious(nextPrev)
		streamer.StartGrouped(m.logGroups)
		m.logStreamer = streamer
		m.logsCtrl = m.logsCtrl.SetPanel(m.logsCtrl.Panel().SetPrevious(nextPrev))
		if nextPrev {
			m.statusBar = m.statusBar.SetMessage("streaming previous container logs")
		} else {
			m.statusBar = m.statusBar.SetMessage("streaming live logs")
		}
		return m, streamer.ReadCmd()

	case panels.YAMLFetchedMsg:
		if msg.Err != nil {
			m.statusBar = m.statusBar.SetMessage("yaml: " + msg.Err.Error())
			return m, nil
		}
		next, cmd := m.yamlViewCtrl.Update(msg)
		m.yamlViewCtrl = next.(modes.YAMLViewController)
		m.mode = ModeYAML
		m.focus = FocusContent
		return m, cmd

	case panels.DescribeFetchedMsg:
		if msg.Err != nil {
			m.statusBar = m.statusBar.SetMessage("describe: " + msg.Err.Error())
			return m, nil
		}
		next, cmd := m.describeCtrl.Update(msg)
		m.describeCtrl = next.(modes.DescribeViewController)
		m.mode = ModeDescribe
		m.focus = FocusContent
		return m, cmd

	case panels.ApplyYAMLRequest:
		if m.clusterMgr == nil {
			return m, nil
		}
		cs, err := m.clusterMgr.ActiveClientset()
		if err != nil || cs == nil {
			m.statusBar = m.statusBar.SetMessage("apply: no client")
			return m, nil
		}
		k, ok := kinds.Lookup(msg.Kind)
		if !ok {
			return m, nil
		}
		return m, kinds.ApplyCmd(k, m.depsFor(cs), msg.Namespace, msg.Name, msg.YAMLContent)

	case panels.YAMLAppliedMsg:
		m.rollbackYAML = m.yamlEditCtrl.Original()
		m.rollbackKind = msg.Kind
		m.rollbackName = msg.Name
		m.rollbackNS = msg.Namespace
		m.mode = ModeYAML
		m.statusBar = m.statusBar.SetMessage(fmt.Sprintf("Applied %s/%s — ctrl+z to rollback", msg.Kind, msg.Name))
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
		m.fullScreen = false
		return m, m.buildTableCmd()

	case k8sops.AttachFinishedMsg:
		if msg.Err != nil {
			m.statusMsg = fmt.Sprintf("attach %s: %v", msg.Pod, msg.Err)
			m.statusBar = m.statusBar.SetMessage(m.statusMsg)
			return m, nil
		}
		m.statusMsg = fmt.Sprintf("attach session ended: %s", msg.Pod)
		m.statusBar = m.statusBar.SetMessage(m.statusMsg)
		return m, clearStatusAfterDelay(5 * time.Second)

	case k8sops.TmuxWindowOpenedMsg:
		if msg.Err != nil {
			m.statusBar = m.statusBar.SetMessage("attach: " + msg.Err.Error())
			return m, nil
		}
		m.statusMsg = fmt.Sprintf("attached to %s", msg.Session.Pod)
		m.statusBar = m.statusBar.SetMessage(m.statusMsg)
		return m, clearStatusAfterDelay(5 * time.Second)

	case clearStatusMsg:
		m.statusMsg = ""
		m.statusBar = m.statusBar.SetMessage("")
		return m, nil

	case widgets.ConfirmResult:
		if msg.Confirmed {
			return m.executeConfirmedOp(msg)
		}
		return m, nil

	case widgets.SanitizeRequest:
		if !msg.Confirmed {
			return m, nil
		}
		return m.executeSanitize(msg.Namespace)

	case widgets.ScaleResult:
		if !msg.Confirmed {
			return m, nil
		}
		cs, err := m.clusterMgr.ActiveClientset()
		if err != nil {
			m.statusBar = m.statusBar.SetMessage("no client: " + err.Error())
			return m, nil
		}
		k, ok := kinds.Lookup(msg.Kind)
		if !ok {
			return m, nil
		}
		cmd := kinds.ScaleCmd(k, m.depsFor(cs), msg.Namespace, msg.Name, msg.Replicas)
		if cmd == nil {
			m.statusBar = m.statusBar.SetMessage("scale not supported for " + msg.Kind)
			return m, nil
		}
		return m, cmd

	case widgets.PortForwardRequest:
		if !msg.Confirmed {
			return m, nil
		}
		return m.startPortForward(msg)

	case widgets.PortForwardListAction:
		if msg.Closed {
			return m, nil
		}
		if msg.Stop {
			m.pfManager.Stop(msg.SessionID)
			m.pfList = m.pfList.SetSessions(m.pfManager.List())
			m.statusBar = m.statusBar.SetMessage("port-forward stopped: " + msg.SessionID)
			// Refresh pod table so the PF column updates immediately.
			if m.mode == ModeTable && m.nav.ActiveKind() == "Pod" {
				return m, m.buildTableCmd()
			}
		}
		return m, nil

	case k8sops.PortForwardReadyMsg:
		if msg.Err != nil {
			m.statusBar = m.statusBar.SetMessage("port-forward failed: " + msg.Err.Error())
			return m, nil
		}
		m.pfManager.Add(msg.Session)
		m.statusBar = m.statusBar.SetMessage(fmt.Sprintf(
			"port-forward 127.0.0.1:%d → %s/%s:%d  (ctrl+f to manage)",
			msg.Session.LocalPort, msg.Session.Namespace, msg.Session.PodName, msg.Session.RemotePort))
		if m.mode == ModeTable && m.nav.ActiveKind() == "Pod" {
			return m, m.buildTableCmd()
		}
		return m, nil

	case k8sops.PortForwardClosedMsg:
		m.pfManager.Remove(msg.ID)
		if msg.Err != nil {
			m.statusBar = m.statusBar.SetMessage("port-forward closed: " + msg.ID + " — " + msg.Err.Error())
		} else {
			m.statusBar = m.statusBar.SetMessage("port-forward closed: " + msg.ID)
		}
		if m.pfList.IsVisible() {
			m.pfList = m.pfList.SetSessions(m.pfManager.List())
		}
		if m.mode == ModeTable && m.nav.ActiveKind() == "Pod" {
			return m, m.buildTableCmd()
		}
		return m, nil

	case widgets.ContextMenuPickedMsg:
		m.contextMenu = m.contextMenu.Hide()
		return m.dispatchTableAction(msg.Action)

	case widgets.ContextMenuCancelMsg:
		m.contextMenu = m.contextMenu.Hide()
		return m, nil

	case tea.PasteMsg:
		// Bracketed paste — route to whichever input is currently capturing.
		switch m.mode {
		case ModeLogs:
			next, cmd := m.logsCtrl.Update(msg)
			m.logsCtrl = next.(modes.LogsController)
			return m, cmd
		case ModeEditor:
			next, cmd := m.yamlEditCtrl.Update(msg)
			m.yamlEditCtrl = next.(modes.YAMLEditController)
			return m, cmd
		case ModeDescribe:
			next, cmd := m.describeCtrl.Update(msg)
			m.describeCtrl = next.(modes.DescribeViewController)
			return m, cmd
		case ModeTable:
			if m.focus == FocusNav && m.nav.FilterActive() {
				var cmd tea.Cmd
				m.nav, cmd = m.nav.Update(msg)
				return m, cmd
			}
			if m.tableCtrl.FilterActive() {
				next, cmd := m.tableCtrl.Update(msg)
				m.tableCtrl = next.(modes.TableController)
				return m, cmd
			}
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		// Surface raw mouse details when KLENS_DEBUG_MOUSE is set so terminal
		// click delivery (or lack thereof) can be diagnosed in the status bar.
		if os.Getenv("KLENS_DEBUG_MOUSE") != "" {
			if click, ok := msg.(tea.MouseClickMsg); ok {
				mp := click.Mouse()
				m.statusBar = m.statusBar.SetMessage(fmt.Sprintf(
					"mouse click: button=%d (%s) X=%d Y=%d", mp.Button, mp.Button, mp.X, mp.Y))
			}
		}
		// Click handling routes through the hit-region map: resolve the screen
		// point to a zone once, then hand the panel coordinates relative to its
		// own outer rect. Nav clicks work in every mode (clicking a nav item
		// while viewing logs / yaml / etc. escapes back to the table). Content
		// clicks are only interpreted as row selection in ModeTable. In
		// fullscreen the map contains a single ZoneContent region covering the
		// whole terminal, so nav misrouting cannot happen by construction.
		if click, ok := msg.(tea.MouseClickMsg); ok {
			mouse := click.Mouse()
			zone, lx, ly := m.hitMap().At(mouse.X, mouse.Y)
			switch zone {
			case hit.ZoneNav:
				// Nav click. If an item was clicked, select it; if we were
				// in a non-table mode, return to the table view.
				// ly-1 skips the nav panel's top border.
				prevKind := m.nav.ActiveKind()
				var newKind string
				m.nav, newKind = m.nav.HandleClickAt(ly - 1)
				switchedMode := m.mode != ModeTable
				if switchedMode {
					if m.mode == ModeLogs && m.logStreamer != nil {
						m.logStreamer.Stop()
						m.logStreamer = nil
					}
					m.mode = ModeTable
					m.fullScreen = false
				}
				if newKind == "" {
					if m.focus != FocusNav {
						m.focus = FocusNav
						m.nav = m.nav.SetFocused(true)
						m.tableCtrl = m.tableCtrl.SetFocused(false).ClearSelection()
					}
					if switchedMode {
						return m, m.buildTableCmd()
					}
					return m, nil
				}
				m.focus = FocusContent
				m.nav = m.nav.SetFocused(false)
				m.tableCtrl = m.tableCtrl.SetFocused(true)
				if newKind != prevKind {
					m.tableCtrl = m.setKindAndSync(newKind)
					m.setStatusBarKind(newKind)
					return m, m.buildTableCmd()
				}
				if switchedMode {
					return m, m.buildTableCmd()
				}
				return m, nil

			case hit.ZoneContent:
				// Content area click. Only the table interprets these as row
				// selection; in other modes fall through to the panel's own
				// mouse handling (scroll etc.).
				if m.mode == ModeTable {
					if m.focus != FocusContent {
						m.focus = FocusContent
						m.nav = m.nav.SetFocused(false)
						m.tableCtrl = m.tableCtrl.SetFocused(true)
					}
					// ly-1 skips the table panel's top border (table hit
					// methods take border-inner Y, outer-relative X).
					switch click.Button {
					case tea.MouseLeft:
						// A click on the column-header row sorts by that column
						// (toggling asc/desc on repeat clicks). Try this before
						// drag-select so the header click never begins a drag.
						var sorted bool
						m.tableCtrl, sorted = m.tableCtrl.HandleHeaderClickAt(lx, ly-1)
						if sorted {
							return m, nil
						}
						var started bool
						m.tableCtrl, started = m.tableCtrl.HandleMouseDown(lx, ly-1)
						if started {
							return m, panels.TableAutoScrollTickCmd()
						}
						return m, nil
					case tea.MouseRight:
						var hitRow bool
						m.tableCtrl, hitRow = m.tableCtrl.HandleClickAt(ly-1, false)
						if hitRow {
							m = m.openContextMenu()
						}
						return m, nil
					}
				}
				// YAML viewer click — start drag-select if it lands on a YAML line.
				if m.mode == ModeYAML && click.Button == tea.MouseLeft {
					var started bool
					m.yamlViewCtrl, started = m.yamlViewCtrl.HandleMouseDown(lx, ly)
					if started {
						return m, panels.YAMLAutoScrollTickCmd()
					}
				}
				// Logs panel click routing. Try drag-select first: HandleMouseDown
				// only succeeds when the click lands on viewport content (auto-
				// switches focus across stripes in split mode), so chrome clicks
				// (tab bar, stripe borders/titles) fall through to HandleClickAt.
				if m.mode == ModeLogs && click.Button == tea.MouseLeft {
					var started bool
					m.logsCtrl, started = m.logsCtrl.HandleMouseDown(lx, ly)
					if started {
						if m.focus != FocusContent {
							m.focus = FocusContent
							m.nav = m.nav.SetFocused(false)
						}
						return m, panels.LogAutoScrollTickCmd()
					}
					var hitChrome bool
					m.logsCtrl, hitChrome = m.logsCtrl.HandleClickAt(lx, ly)
					if hitChrome {
						if m.focus != FocusContent {
							m.focus = FocusContent
							m.nav = m.nav.SetFocused(false)
						}
						return m, nil
					}
				}
			}
		}
		// Drag/release routing for in-app drag-to-copy. Coordinates are always
		// relative to the content rect — deliberately not zone-gated, because a
		// drag in flight must keep receiving motion outside the panel bounds to
		// drive edge auto-scroll. HandleMouseDrag/Up are no-ops when no
		// selection is in flight, so stray motion/release events are harmless.
		{
			cr := m.contentRect()
			switch m.mode {
			case ModeLogs:
				if motion, ok := msg.(tea.MouseMotionMsg); ok {
					mp := motion.Mouse()
					lx, ly := cr.Local(mp.X, mp.Y)
					m.logsCtrl = m.logsCtrl.HandleMouseDrag(lx, ly)
					return m, nil
				}
				if release, ok := msg.(tea.MouseReleaseMsg); ok && release.Button == tea.MouseLeft {
					mp := release.Mouse()
					lx, ly := cr.Local(mp.X, mp.Y)
					var status string
					m.logsCtrl, status = m.logsCtrl.HandleMouseUp(lx, ly)
					if status != "" {
						m.statusBar = m.statusBar.SetMessage(status)
					}
					return m, nil
				}
			case ModeYAML:
				if motion, ok := msg.(tea.MouseMotionMsg); ok {
					mp := motion.Mouse()
					lx, ly := cr.Local(mp.X, mp.Y)
					m.yamlViewCtrl = m.yamlViewCtrl.HandleMouseDrag(lx, ly)
					return m, nil
				}
				if release, ok := msg.(tea.MouseReleaseMsg); ok && release.Button == tea.MouseLeft {
					mp := release.Mouse()
					lx, ly := cr.Local(mp.X, mp.Y)
					var status string
					m.yamlViewCtrl, status = m.yamlViewCtrl.HandleMouseUp(lx, ly)
					if status != "" {
						m.statusBar = m.statusBar.SetMessage(status)
					}
					return m, nil
				}
			case ModeTable:
				if motion, ok := msg.(tea.MouseMotionMsg); ok {
					mp := motion.Mouse()
					lx, ly := cr.Local(mp.X, mp.Y)
					m.tableCtrl = m.tableCtrl.HandleMouseDrag(lx, ly-1)
					return m, nil
				}
				if release, ok := msg.(tea.MouseReleaseMsg); ok && release.Button == tea.MouseLeft {
					mp := release.Mouse()
					lx, ly := cr.Local(mp.X, mp.Y)
					var status string
					m.tableCtrl, status = m.tableCtrl.HandleMouseUp(lx, ly-1)
					if status != "" {
						m.statusBar = m.statusBar.SetMessage(status)
					}
					return m, nil
				}
			}
		}
		switch m.mode {
		case ModeTable:
			var cmd tea.Cmd
			if m.focus == FocusContent {
				m.tableCtrl, cmd = m.tableCtrl.Step(msg)
			} else {
				m.nav, cmd = m.nav.Update(msg)
			}
			return m, cmd
		case ModeLogs:
			next, cmd := m.logsCtrl.Update(msg)
			m.logsCtrl = next.(modes.LogsController)
			return m, cmd
		case ModeYAML:
			next, cmd := m.yamlViewCtrl.Update(msg)
			m.yamlViewCtrl = next.(modes.YAMLViewController)
			return m, cmd
		case ModeEditor:
			next, cmd := m.yamlEditCtrl.Update(msg)
			m.yamlEditCtrl = next.(modes.YAMLEditController)
			return m, cmd
		case ModeXRay:
			next, cmd := m.xrayCtrl.Update(msg)
			m.xrayCtrl = next.(modes.XRayController)
			return m, cmd
		case ModeMetrics:
			next, cmd := m.metricsCtrl.Update(msg)
			m.metricsCtrl = next.(modes.MetricsController)
			return m, cmd
		case ModeDescribe:
			next, cmd := m.describeCtrl.Update(msg)
			m.describeCtrl = next.(modes.DescribeViewController)
			return m, cmd
		}
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	// When nav filter input is open, route all keys to nav handler.
	if m.focus == FocusNav && m.nav.FilterActive() {
		prev := m.nav.ActiveKind()
		var cmd tea.Cmd
		m.nav, cmd = m.nav.Update(msg)
		if m.nav.ActiveKind() != prev {
			m.tableCtrl = m.setKindAndSync(m.nav.ActiveKind())
			m.setStatusBarKind(m.nav.ActiveKind())
			return m, tea.Batch(cmd, m.buildTableCmd())
		}
		return m, cmd
	}

	// Global keys work in any mode
	switch msg.String() {
	case "F":
		switch m.mode {
		case ModeYAML, ModeEditor, ModeLogs, ModeXRay, ModeMetrics, ModeDescribe:
			// Let the editor controller consume F as literal text when in
			// Insert mode. consumed=false falls through to fullscreen toggle.
			if m.mode == ModeEditor {
				next, cmd, consumed := m.yamlEditCtrl.HandleKey(msg)
				m.yamlEditCtrl = next.(modes.YAMLEditController)
				if consumed {
					return m, cmd
				}
			}
			m = m.toggleFullScreen()
			m = m.resizePanels()
			return m, nil
		}
	case "q", "ctrl+c":
		if m.mode == ModeEditor {
			next, cmd, consumed := m.yamlEditCtrl.HandleKey(msg)
			m.yamlEditCtrl = next.(modes.YAMLEditController)
			if consumed {
				return m, cmd
			}
		}
		if m.mode == ModeLogs {
			next, cmd, consumed := m.logsCtrl.HandleKey(msg)
			m.logsCtrl = next.(modes.LogsController)
			if consumed {
				return m, cmd
			}
		}
		if m.tableCtrl.FilterActive() {
			var cmd tea.Cmd
			m.tableCtrl, cmd = m.tableCtrl.Step(msg)
			return m, cmd
		}
		m.stopAll()
		return m, tea.Quit
	case "esc":
		// In the YAML editor's Insert mode, ESC switches to Normal — don't exit to table.
		// (Handled before fullScreen so insert→normal transition fires before fullScreen exits.)
		if m.mode == ModeEditor {
			next, cmd, consumed := m.yamlEditCtrl.HandleKey(msg)
			m.yamlEditCtrl = next.(modes.YAMLEditController)
			if consumed {
				return m, cmd
			}
		}
		// If table has filter or h-scroll state, let the table handle ESC to peel it.
		if m.mode == ModeTable && (m.tableCtrl.HasFilter() || m.tableCtrl.HasHScroll()) {
			var cmd tea.Cmd
			m.tableCtrl, cmd = m.tableCtrl.Step(msg)
			return m, cmd
		}
		// Peel log viewer state (filter input, search, podFilter, split layout)
		// BEFORE peeling fullscreen — otherwise an open filter input would be
		// destroyed by an exit-fullscreen we didn't want. The controller
		// returns consumed=true when there was a layer to peel; false means
		// nothing left to peel and we should fall through to the fullscreen
		// / mode-exit cascade below.
		if m.mode == ModeLogs {
			next, cmd, consumed := m.logsCtrl.HandleKey(msg)
			m.logsCtrl = next.(modes.LogsController)
			if consumed {
				return m, cmd
			}
		}
		// Same peel pattern for the describe viewer: open filter input →
		// applied filter → exit mode. The viewer's HandleEsc returns true
		// while it still has state to peel.
		if m.mode == ModeDescribe && m.describeCtrl.HasActiveState() {
			next, consumed := m.describeCtrl.HandleEsc()
			m.describeCtrl = next
			if consumed {
				return m, nil
			}
		}
		// Peel fullscreen if active — return to normal layout, stay in mode.
		if m.fullScreen {
			m.fullScreen = false
			m = m.resizePanels()
			return m, nil
		}
		if m.mode != ModeTable {
			m.mode = ModeTable
			m.fullScreen = false
			if m.logStreamer != nil {
				m.logStreamer.Stop()
				m.logStreamer = nil
			}
			return m, nil
		}
		if m.focus == FocusContent {
			m.focus = FocusNav
			m.nav = m.nav.SetFocused(true)
			m.tableCtrl = m.tableCtrl.SetFocused(false).ClearSelection()
			return m, nil
		}
	case "ctrl+n":
		if m.clusterMgr != nil {
			clusterNs := m.clusterNamespaces
			if len(clusterNs) == 0 && m.watcher != nil {
				for _, ns := range k8sops.ListAs[*corev1.Namespace](m.watcher, "Namespace", "") {
					clusterNs = append(clusterNs, ns.Name)
				}
			}
			savedNs := []string(nil)
			if m.appConfig != nil {
				savedNs = m.appConfig.SavedNamespaces(m.clusterMgr.ActiveContext())
			}
			m.namespacePicker = m.namespacePicker.Show(clusterNs, savedNs)
		}
		return m, nil
	case "ctrl+o":
		if m.clusterMgr != nil {
			m.clusterPicker = m.clusterPicker.Show(m.clusterMgr.Contexts(), m.clusterMgr.ActiveContext())
		}
		return m, nil
	case "ctrl+f":
		// PF list modal is global — works in any mode, regardless of focus.
		// The log viewer also binds ctrl+f, but only while in ModeLogs, so we
		// intentionally don't intercept there.
		if m.mode != ModeLogs {
			m.pfList = m.pfList.Show(m.pfManager.List())
			return m, nil
		}
	case "z":
		// Sanitize is Pod-only and a destructive op.
		if m.mode == ModeTable && m.nav.ActiveKind() == "Pod" {
			return m.actionSanitize()
		}
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
		m.fullScreen = false
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
			case "y", "l", "x", "m", "d", "a", "s", "f", "F", "ctrl+d", "ctrl+k":
				return m.handleTableKeys(msg)
			case "ctrl+z", "w", "o", "e":
				if m.isEventsTable() {
					return m.handleTableKeys(msg)
				}
			}
		}
	}

	if m.mode == ModeYAML {
		next, cmd, consumed := m.yamlViewCtrl.HandleKey(msg)
		m.yamlViewCtrl = next.(modes.YAMLViewController)
		if consumed {
			return m, cmd
		}
		// Refused keys: "e" (open editor) and "ctrl+z" (rollback) — both need
		// root-owned state (readOnly + rollback stash).
		return m.handleYAMLViewKeys(msg)
	}

	if m.mode == ModeEditor {
		next, cmd, _ := m.yamlEditCtrl.HandleKey(msg)
		m.yamlEditCtrl = next.(modes.YAMLEditController)
		return m, cmd
	}

	if m.mode == ModeLogs {
		next, cmd, _ := m.logsCtrl.HandleKey(msg)
		m.logsCtrl = next.(modes.LogsController)
		var s string
		m.logsCtrl, s = m.logsCtrl.ConsumeStatusMsg()
		if s != "" {
			m.statusBar = m.statusBar.SetMessage(s)
		}
		return m, cmd
	}

	if m.mode == ModeXRay {
		next, cmd, _ := m.xrayCtrl.HandleKey(msg)
		m.xrayCtrl = next.(modes.XRayController)
		return m, cmd
	}

	if m.mode == ModeMetrics {
		next, cmd, _ := m.metricsCtrl.HandleKey(msg)
		m.metricsCtrl = next.(modes.MetricsController)
		return m, cmd
	}

	if m.mode == ModeDescribe {
		next, cmd, _ := m.describeCtrl.HandleKey(msg)
		m.describeCtrl = next.(modes.DescribeViewController)
		var s string
		m.describeCtrl, s = m.describeCtrl.ConsumeStatusMsg()
		if s != "" {
			m.statusBar = m.statusBar.SetMessage(s)
		}
		return m, cmd
	}

	// Table + nav routing
	switch msg.String() {
	case "tab":
		if m.focus == FocusNav {
			m.focus = FocusContent
		} else {
			m.focus = FocusNav
			m.tableCtrl = m.tableCtrl.ClearSelection()
		}
		m.nav = m.nav.SetFocused(m.focus == FocusNav)
		m.tableCtrl = m.tableCtrl.SetFocused(m.focus == FocusContent)
		return m, nil
	case "enter", "right":
		if m.focus == FocusNav {
			m.focus = FocusContent
			m.nav = m.nav.SetFocused(false)
			m.tableCtrl = m.tableCtrl.SetFocused(true)
			return m, nil
		}
	case "left":
		if m.focus == FocusContent {
			m.focus = FocusNav
			m.nav = m.nav.SetFocused(true)
			m.tableCtrl = m.tableCtrl.SetFocused(false).ClearSelection()
			return m, nil
		}
	}

	var cmd tea.Cmd
	if m.focus == FocusNav {
		prev := m.nav.ActiveKind()
		m.nav, cmd = m.nav.Update(msg)
		if m.nav.ActiveKind() != prev {
			m.tableCtrl = m.setKindAndSync(m.nav.ActiveKind())
			m.setStatusBarKind(m.nav.ActiveKind())
			return m, tea.Batch(cmd, m.buildTableCmd())
		}
	} else {
		m.tableCtrl, cmd = m.tableCtrl.Step(msg)
	}
	return m, cmd
}

func (m Model) handleTableKeys(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	// When filter input is open, all keys go to the table's filter handler.
	if m.tableCtrl.FilterActive() {
		var cmd tea.Cmd
		m.tableCtrl, cmd = m.tableCtrl.Step(msg)
		return m, cmd
	}
	switch msg.String() {
	case "enter":
		// First press in nav: shift focus to content. Second press while
		// already in content (with a selected row) opens the context menu.
		if m.focus == FocusContent {
			m2 := m.openContextMenu()
			if m2.contextMenu.IsVisible() {
				return m2, nil
			}
		}
		m.focus = FocusContent
		m.nav = m.nav.SetFocused(false)
		m.tableCtrl = m.tableCtrl.SetFocused(true)
		return m, nil
	case "left":
		m.focus = FocusNav
		m.nav = m.nav.SetFocused(true)
		m.tableCtrl = m.tableCtrl.SetFocused(false).ClearSelection()
		return m, nil
	case "y":
		return m.actionViewYAML()
	case "l":
		return m.actionLogs()
	case "x":
		return m.actionXRay()
	case "m":
		return m.actionMetrics()
	case "d":
		return m.actionDescribe()
	case "ctrl+d":
		if m.isEventsTable() {
			m.statusBar = m.statusBar.SetMessage("not supported on Event")
			return m, nil
		}
		return m.actionDelete()
	case "ctrl+k":
		return m.actionKill()
	case "ctrl+z":
		if m.isEventsTable() {
			return m.actionEventToggleFaults()
		}
	case "w":
		if m.isEventsTable() {
			return m.actionEventToggleWrap()
		}
	case "o":
		if m.isEventsTable() {
			return m.actionEventJumpToInvolvedObject()
		}
	case "e":
		if m.isEventsTable() {
			m.statusBar = m.statusBar.SetMessage("not supported on Event")
			return m, nil
		}
	case "s":
		if m.nav.ActiveKind() == "HelmRelease" {
			return m.actionSuspendHelm()
		}
		return m.actionScale()
	case "r":
		return m.actionResumeHelm()
	case "a":
		if m.isEventsTable() {
			m.statusBar = m.statusBar.SetMessage("not supported on Event")
			return m, nil
		}
		return m.actionAttach()
	case "f", "F":
		return m.actionPortForward()
	}
	// Pass remaining keys to table
	var cmd tea.Cmd
	m.tableCtrl, cmd = m.tableCtrl.Step(msg)
	return m, cmd
}

// isEventsTable reports whether the Events kind is currently displayed in
// the resource table. Gates the events-only keybindings (ctrl+z, w, o) and
// the suppression messages for ctrl+d / e / a.
func (m Model) isEventsTable() bool {
	return m.mode == ModeTable && m.nav.ActiveKind() == "Event"
}

// actionEventToggleFaults flips the faults-only filter and refreshes the
// table so the post-filter pass runs immediately. Sets a title-row badge
// ("· faults") so the user has a persistent cue that filtering is active
// — a status-bar message would permanently hide the help footer.
func (m Model) actionEventToggleFaults() (Model, tea.Cmd) {
	m.eventFaultsOnly = !m.eventFaultsOnly
	if m.eventFaultsOnly {
		m.tableCtrl = m.tableCtrl.SetTitleBadge("faults")
	} else {
		m.tableCtrl = m.tableCtrl.SetTitleBadge("")
	}
	return m, m.buildTableCmd()
}

// actionEventToggleWrap flips MESSAGE-column wrap. SetWrapColumn picks the
// first Scrollable column on the active kind (today only MESSAGE on Event),
// matching the existing horizontal-scroll opt-in flag. The visual change
// (cells wrapping vs truncating) is feedback enough — a persistent
// status-bar message would permanently hide the help footer.
func (m Model) actionEventToggleWrap() (Model, tea.Cmd) {
	m.eventWrapMessage = !m.eventWrapMessage
	if !m.eventWrapMessage {
		m.tableCtrl = m.tableCtrl.ClearWrapColumn()
		return m, nil
	}
	k, ok := kinds.Lookup("Event")
	if !ok {
		m.eventWrapMessage = false
		return m, nil
	}
	idx := -1
	for i, c := range k.Columns() {
		if c.Scrollable {
			idx = i
			break
		}
	}
	if idx < 0 {
		m.eventWrapMessage = false
		return m, nil
	}
	m.tableCtrl = m.tableCtrl.SetWrapColumn(idx)
	return m, nil
}

// actionEventJumpToInvolvedObject resolves the selected event row's
// InvolvedObject and switches the nav panel to that kind. If the involved
// object's kind isn't registered, surfaces the kind name via the status bar
// and stays put.
func (m Model) actionEventJumpToInvolvedObject() (Model, tea.Cmd) {
	row := m.tableCtrl.SelectedRow()
	if row == nil {
		return m, nil
	}
	k, ok := kinds.Lookup("Event")
	if !ok {
		return m, nil
	}
	resolver, ok := any(k).(kinds.InvolvedObjectResolver)
	if !ok {
		return m, nil
	}
	obj, ok := row.Raw.(runtime.Object)
	if !ok {
		return m, nil
	}
	gvk, ns, name, ok := resolver.InvolvedObject(obj)
	if !ok {
		m.statusBar = m.statusBar.SetMessage("event has no involved object")
		return m, nil
	}
	target, ok := kinds.LookupByGVK(gvk)
	if !ok {
		m.statusBar = m.statusBar.SetMessage("unknown kind: " + gvk.Kind)
		return m, nil
	}
	prevKind := m.nav.ActiveKind()
	targetKind := target.Meta().Kind
	if targetKind != prevKind {
		m.nav = m.nav.SetActiveKind(targetKind)
		m.tableCtrl = m.setKindAndSync(targetKind)
		m.setStatusBarKind(targetKind)
	}
	// Schedule the cursor to land on the target row once the new kind's
	// informer cache has emitted into the table. The next refreshMsg picks
	// it up; while the informer is still syncing the retry counter is held
	// at zero so the jump survives the warm-up.
	m.pendingJumpName = name
	m.pendingJumpKind = targetKind
	m.pendingJumpTries = 0

	// Namespace switch when the involved object lives elsewhere. Without
	// this the user would land on the target kind's table but in the wrong
	// namespace — pendingJumpName would never find a match. switchNamespace
	// resets focus to nav as part of its "reset to table" pass; restore
	// content focus afterwards so the user lands ready to scroll.
	var cmd tea.Cmd
	if ns != "" && ns != m.namespace {
		m, cmd = m.switchNamespace(ns)
	} else {
		cmd = m.buildTableCmd()
	}
	m.focus = FocusContent
	m.nav = m.nav.SetFocused(false)
	m.tableCtrl = m.tableCtrl.SetFocused(true)
	return m, cmd
}

// maybeFaultsFilter returns rows pared down to fault rows when the toggle is
// on and the active kind is Event with a FaultRowMarker. Allocates a fresh
// backing slice so we never mutate the informer's owned memory.
func (m Model) maybeFaultsFilter(rows []k8sops.ResourceRow) []k8sops.ResourceRow {
	if !m.eventFaultsOnly || m.nav.ActiveKind() != "Event" || len(rows) == 0 {
		return rows
	}
	k, ok := kinds.Lookup("Event")
	if !ok {
		return rows
	}
	fm, ok := any(k).(kinds.FaultRowMarker)
	if !ok {
		return rows
	}
	out := make([]k8sops.ResourceRow, 0, len(rows))
	for _, r := range rows {
		obj, ok := r.Raw.(runtime.Object)
		if !ok {
			continue
		}
		if fm.IsFaultRow(obj) {
			out = append(out, r)
		}
	}
	return out
}

// maybeApplyPendingJump moves the cursor onto pendingJumpName after a fresh
// refresh. While the watcher is still syncing the retry counter is held at
// zero — the row may simply not have been populated yet. After two
// post-sync misses we give up: the user is in the right view, just without
// an exact cursor position.
func (m Model) maybeApplyPendingJump() Model {
	if m.pendingJumpName == "" || m.pendingJumpKind != m.nav.ActiveKind() {
		return m
	}
	if next, ok := m.tableCtrl.CursorToName(m.pendingJumpName); ok {
		m.tableCtrl = next
		m.pendingJumpName = ""
		m.pendingJumpKind = ""
		m.pendingJumpTries = 0
		return m
	}
	if m.syncing {
		return m
	}
	m.pendingJumpTries++
	if m.pendingJumpTries >= 2 {
		m.pendingJumpName = ""
		m.pendingJumpKind = ""
		m.pendingJumpTries = 0
	}
	return m
}

// dispatchTableAction routes a context-menu pick to the same helpers used by handleTableKeys.
func (m Model) dispatchTableAction(action string) (Model, tea.Cmd) {
	switch action {
	case "y":
		return m.actionViewYAML()
	case "l":
		return m.actionLogs()
	case "x":
		return m.actionXRay()
	case "m":
		return m.actionMetrics()
	case "a":
		return m.actionAttach()
	case "f":
		return m.actionPortForward()
	case "scale":
		return m.actionScale()
	case "suspend":
		return m.actionSuspendHelm()
	case "resume":
		return m.actionResumeHelm()
	case "d":
		return m.actionDescribe()
	case "ctrl+d":
		return m.actionDelete()
	case "ctrl+k":
		return m.actionKill()
	case "clear-selection":
		return m.actionClearSelection()
	}
	return m, nil
}

// openContextMenu shows the context menu for whatever the user has currently
// selected. With a multi-select active, it builds a multi-resource menu (logs,
// bulk delete, clear). Otherwise it builds the single-resource menu.
func (m Model) openContextMenu() Model {
	row := m.tableCtrl.SelectedRow()
	if row == nil {
		return m
	}
	kind := m.nav.ActiveKind()
	var items []widgets.MenuItem
	var title string
	if n := m.tableCtrl.SelectionCount(); n > 1 {
		items = buildMultiContextMenuItems(kind, n, m.readOnly)
		title = fmt.Sprintf("%d %s selected", n, pluralizeKind(kind))
	} else {
		items = buildContextMenuItems(kind, m.readOnly)
		title = fmt.Sprintf("%s/%s", kind, row.Name)
	}
	if len(items) == 0 {
		return m
	}
	termW, termH := m.layout.TermSize()
	m.contextMenu = m.contextMenu.SetSize(termW, termH).Show(title, items)
	return m
}

// pluralizeKind returns a simple plural form of a Kubernetes resource kind.
// "Pod" → "Pods", "PersistentVolumeClaim" → "PersistentVolumeClaims".
func pluralizeKind(kind string) string {
	if kind == "" {
		return "items"
	}
	if strings.HasSuffix(kind, "s") {
		return kind + "es"
	}
	return kind + "s"
}

// buildMultiContextMenuItems returns the menu entries that make sense when more
// than one row is selected. Single-resource actions (View YAML, XRay, Attach,
// Scale, etc.) are intentionally excluded.
func buildMultiContextMenuItems(kind string, count int, readOnly bool) []widgets.MenuItem {
	k, ok := kinds.Lookup(kind)
	if !ok {
		return nil
	}
	var items []widgets.MenuItem
	if _, ok := any(k).(kinds.Logger); ok {
		items = append(items, widgets.MenuItem{
			Label:  fmt.Sprintf("View Logs (%d, combined)", count),
			Action: "l",
			Hint:   "l",
		})
	}
	if !readOnly {
		if _, ok := any(k).(kinds.Deleter); ok {
			items = append(items, widgets.MenuItem{
				Label:  fmt.Sprintf("Delete %d…", count),
				Action: "ctrl+d",
				Hint:   "ctrl+d",
			})
		}
		if _, ok := any(k).(kinds.Killer); ok {
			items = append(items, widgets.MenuItem{
				Label:  fmt.Sprintf("Kill %d… (force)", count),
				Action: "ctrl+k",
				Hint:   "ctrl+k",
			})
		}
	}
	items = append(items, widgets.MenuItem{
		Label:  "Clear selection",
		Action: "clear-selection",
		Hint:   "c",
	})
	return items
}

// buildContextMenuItems returns the list of menu entries valid for the given kind
// and the current read-only state. Mirrors the gating logic in setStatusBarKind.
func buildContextMenuItems(kind string, readOnly bool) []widgets.MenuItem {
	k, ok := kinds.Lookup(kind)
	if !ok {
		return nil
	}
	var items []widgets.MenuItem
	// Every Kind implements Fetch, so YAML view is always available.
	items = append(items, widgets.MenuItem{Label: "View YAML", Action: "y", Hint: "y"})
	items = append(items, widgets.MenuItem{Label: "Describe", Action: "d", Hint: "d"})
	if _, ok := any(k).(kinds.Logger); ok {
		items = append(items, widgets.MenuItem{Label: "View Logs", Action: "l", Hint: "l"})
	}
	if _, ok := any(k).(kinds.XRayer); ok {
		items = append(items, widgets.MenuItem{Label: "View XRay", Action: "x", Hint: "x"})
	}
	if _, ok := any(k).(kinds.MetricsSupporter); ok {
		items = append(items, widgets.MenuItem{Label: "View Metrics", Action: "m", Hint: "m"})
	}
	if _, ok := any(k).(kinds.PortForwarder); ok {
		items = append(items, widgets.MenuItem{Label: "Port Forward…", Action: "f", Hint: "f"})
	}
	if !readOnly {
		if _, ok := any(k).(kinds.Attacher); ok {
			items = append(items, widgets.MenuItem{Label: "Attach (exec)", Action: "a", Hint: "a"})
		}
		if _, ok := any(k).(kinds.Scaler); ok {
			items = append(items, widgets.MenuItem{Label: "Scale", Action: "scale", Hint: "s"})
		}
		if _, ok := any(k).(kinds.Suspender); ok {
			items = append(items,
				widgets.MenuItem{Label: "Suspend", Action: "suspend", Hint: "s"},
				widgets.MenuItem{Label: "Resume", Action: "resume", Hint: "r"},
			)
		}
		if _, ok := any(k).(kinds.Deleter); ok {
			items = append(items, widgets.MenuItem{Label: "Delete…", Action: "ctrl+d", Hint: "ctrl+d"})
		}
		if _, ok := any(k).(kinds.Killer); ok {
			items = append(items, widgets.MenuItem{Label: "Kill… (force)", Action: "ctrl+k", Hint: "ctrl+k"})
		}
	}
	return items
}

// --- Per-action helpers (extracted from handleTableKeys; behavior-preserving) ---

func (m Model) actionClearSelection() (Model, tea.Cmd) {
	m.tableCtrl = m.tableCtrl.ClearSelection()
	return m, nil
}

// actionDescribe fetches kubectl-style describe output for the selected row
// and opens ModeDescribe with it. Describe is non-mutating, so it works in
// readonly mode (no readOnly gate). The fetch runs in a goroutine and emits
// panels.DescribeFetchedMsg on completion.
func (m Model) actionDescribe() (Model, tea.Cmd) {
	row := m.tableCtrl.SelectedRow()
	if row == nil {
		return m, nil
	}
	if m.clusterMgr == nil {
		return m, nil
	}
	cfg, err := m.clusterMgr.ActiveRestConfig()
	if err != nil || cfg == nil {
		m.statusBar = m.statusBar.SetMessage("describe: no client")
		return m, nil
	}
	kind := m.nav.ActiveKind()
	k, ok := kinds.Lookup(kind)
	if !ok {
		return m, nil
	}
	meta := k.Meta()
	gvk := schema.GroupVersionKind{
		Group:   meta.GVR.Group,
		Version: meta.GVR.Version,
		Kind:    meta.Kind,
	}
	m.statusBar = m.statusBar.SetMessage("Loading describe…")
	return m, panels.FetchDescribeCmd(cfg, gvk, kind, row.Name, row.Namespace)
}

func (m Model) actionViewYAML() (Model, tea.Cmd) {
	row := m.tableCtrl.SelectedRow()
	if row == nil {
		return m, nil
	}
	if m.nav.ActiveKind() == "HelmRelease" {
		if u, ok := row.Raw.(*unstructuredpkg.Unstructured); ok {
			return m, panels.FetchHelmReleaseYAMLCmd(u)
		}
		return m, nil
	}
	if m.clusterMgr != nil {
		cs, _ := m.clusterMgr.ActiveClientset()
		if cs != nil {
			return m, panels.FetchYAMLCmd(cs, m.nav.ActiveKind(), row.Name, row.Namespace)
		}
	}
	return m, nil
}

func (m Model) actionLogs() (Model, tea.Cmd) {
	kind := m.nav.ActiveKind()
	selectedNames := m.tableCtrl.SelectedPods()
	var groups []k8sops.LogGroup
	if m.watcher != nil {
		for _, name := range selectedNames {
			pods := k8sops.ResolvePodNames(kind, name, m.namespace, m.watcher)
			if len(pods) > 0 {
				groups = append(groups, k8sops.LogGroup{Name: name, Pods: pods})
			}
		}
	} else {
		for _, name := range selectedNames {
			groups = append(groups, k8sops.LogGroup{Name: name, Pods: []string{name}})
		}
	}
	if len(groups) > 0 && m.clusterMgr != nil {
		cs, _ := m.clusterMgr.ActiveClientset()
		if cs != nil {
			if m.logStreamer != nil {
				m.logStreamer.Stop()
			}
			streamer := k8sops.NewLogStreamer(cs, m.namespace)
			streamer.StartGrouped(groups)
			m.logStreamer = streamer
			m.logGroups = groups
			m.logsCtrl = m.logsCtrl.SetPodGroups(groups)
			m.mode = ModeLogs
			m.focus = FocusContent
			return m, streamer.ReadCmd()
		}
	}
	return m, nil
}

func (m Model) actionXRay() (Model, tea.Cmd) {
	row := m.tableCtrl.SelectedRow()
	if row != nil && m.watcher != nil {
		tree := m.buildXRay(m.nav.ActiveKind(), row.Name)
		m.xrayCtrl = m.xrayCtrl.SetTree(m.nav.ActiveKind(), row.Name, tree)
		m.mode = ModeXRay
		m.focus = FocusContent
	}
	return m, nil
}

func (m Model) actionMetrics() (Model, tea.Cmd) {
	row := m.tableCtrl.SelectedRow()
	if row == nil {
		return m, nil
	}
	key := row.Namespace + "/" + row.Name
	rm := m.metricsData.Pods[key]
	m.metricsCtrl = m.metricsCtrl.SetResource(row.Name, row.Namespace, rm)
	if m.watcher != nil {
		for _, pod := range k8sops.ListAs[*corev1.Pod](m.watcher, "Pod", row.Namespace) {
			if pod.Name == row.Name {
				cpuReqM, cpuLimM, memReqB, memLimB := panels.PodResourceTotals(pod)
				m.metricsCtrl = m.metricsCtrl.SetLimits(cpuReqM, cpuLimM, memReqB, memLimB)
				break
			}
		}
	}
	m.mode = ModeMetrics
	m.focus = FocusContent
	return m, nil
}

func (m Model) actionDelete() (Model, tea.Cmd) {
	if m.readOnly {
		m.statusBar = m.statusBar.SetMessage("read-only mode")
		return m, nil
	}
	kind := m.nav.ActiveKind()
	rows := m.tableCtrl.SelectedRows()
	if len(rows) == 0 {
		return m, nil
	}
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
	return m, nil
}

// actionKill is the force-delete (grace=0) counterpart to actionDelete. Opens
// a danger-styled confirm; on confirm, executeConfirmedOp dispatches KillCmd
// per target. For non-Pod kinds KillCmd falls back to DeleteCmd, so the
// keybinding works everywhere ctrl+d does — only Pods see different on-wire
// semantics.
func (m Model) actionKill() (Model, tea.Cmd) {
	if m.readOnly {
		m.statusBar = m.statusBar.SetMessage("read-only mode")
		return m, nil
	}
	kind := m.nav.ActiveKind()
	rows := m.tableCtrl.SelectedRows()
	if len(rows) == 0 {
		return m, nil
	}
	var targets []deleteTarget
	for _, row := range rows {
		targets = append(targets, deleteTarget{name: row.Name, namespace: row.Namespace})
	}
	var action, resource string
	if len(targets) == 1 {
		action, resource = "Kill", targets[0].name
	} else {
		action = fmt.Sprintf("Kill %d", len(targets))
		resource = kind + "s"
	}
	m.confirm = m.confirm.ShowDanger(action, resource)
	m.pendingOp = pendingOpData{op: "kill", kind: kind, targets: targets}
	return m, nil
}

// actionSanitize opens the sanitize confirmation dialog for the current
// namespace's Pod view. The actual deletion runs only after the user types
// the exact confirmation phrase and dispatches a SanitizeRequest.
func (m Model) actionSanitize() (Model, tea.Cmd) {
	if m.readOnly {
		m.statusBar = m.statusBar.SetMessage("read-only mode")
		return m, nil
	}
	if m.nav.ActiveKind() != "Pod" {
		return m, nil
	}
	m.sanitizeDialog = m.sanitizeDialog.Show(m.namespace)
	return m, nil
}

// executeSanitize lists pods in the current namespace that are in a
// completed/error state and issues a Delete for each. Filtering matches
// k9s: phase == Succeeded || phase == Failed.
func (m Model) executeSanitize(namespace string) (Model, tea.Cmd) {
	if m.readOnly || m.watcher == nil || m.clusterMgr == nil {
		return m, nil
	}
	cs, err := m.clusterMgr.ActiveClientset()
	if err != nil || cs == nil {
		m.statusBar = m.statusBar.SetMessage("sanitize: no client")
		return m, nil
	}
	k, ok := kinds.Lookup("Pod")
	if !ok {
		return m, nil
	}
	deps := m.depsFor(cs)
	var cmds []tea.Cmd
	for _, p := range k8sops.ListAs[*corev1.Pod](m.watcher, "Pod", namespace) {
		if p.DeletionTimestamp != nil {
			continue
		}
		phase := p.Status.Phase
		if phase != corev1.PodSucceeded && phase != corev1.PodFailed {
			continue
		}
		if c := kinds.DeleteCmd(k, deps, p.Namespace, p.Name); c != nil {
			cmds = append(cmds, c)
		}
	}
	if len(cmds) == 0 {
		m.statusBar = m.statusBar.SetMessage("sanitize: no completed/error pods")
		return m, nil
	}
	m.statusBar = m.statusBar.SetMessage(fmt.Sprintf("sanitize: deleting %d pod(s)", len(cmds)))
	return m, tea.Batch(cmds...)
}

func (m Model) actionScale() (Model, tea.Cmd) {
	if m.readOnly {
		m.statusBar = m.statusBar.SetMessage("read-only mode")
		return m, nil
	}
	k, ok := kinds.Lookup(m.nav.ActiveKind())
	if !ok {
		return m, nil
	}
	if _, ok := any(k).(kinds.Scaler); !ok {
		return m, nil
	}
	row := m.tableCtrl.SelectedRow()
	if row == nil {
		return m, nil
	}
	current := currentReplicas(m.nav.ActiveKind(), row)
	m.scaleDialog = m.scaleDialog.Show(m.nav.ActiveKind(), row.Name, row.Namespace, current)
	return m, nil
}

func (m Model) actionSuspendHelm() (Model, tea.Cmd) {
	if m.readOnly {
		m.statusBar = m.statusBar.SetMessage("read-only mode")
		return m, nil
	}
	if m.nav.ActiveKind() != "HelmRelease" {
		return m, nil
	}
	row := m.tableCtrl.SelectedRow()
	if row == nil {
		return m, nil
	}
	m.confirm = m.confirm.Show("Suspend", row.Name)
	m.pendingOp = pendingOpData{op: "suspend", kind: "HelmRelease", name: row.Name, namespace: row.Namespace}
	return m, nil
}

func (m Model) actionResumeHelm() (Model, tea.Cmd) {
	if m.readOnly {
		m.statusBar = m.statusBar.SetMessage("read-only mode")
		return m, nil
	}
	if m.nav.ActiveKind() != "HelmRelease" {
		return m, nil
	}
	row := m.tableCtrl.SelectedRow()
	if row == nil {
		return m, nil
	}
	m.confirm = m.confirm.Show("Resume", row.Name)
	m.pendingOp = pendingOpData{op: "resume", kind: "HelmRelease", name: row.Name, namespace: row.Namespace}
	return m, nil
}

// actionPortForward toggles port-forwarding for the selected pod: if any
// forward is currently active against it, all of its forwards are stopped;
// otherwise the dialog opens to start a new one. PF is non-mutating (it opens
// a local socket; no cluster state changes), so it is intentionally NOT gated
// on m.readOnly — k9s behaves the same.
func (m Model) actionPortForward() (Model, tea.Cmd) {
	row := m.tableCtrl.SelectedRow()
	if row == nil {
		m.statusBar = m.statusBar.SetMessage("no pod selected")
		return m, nil
	}
	k, ok := kinds.Lookup(m.nav.ActiveKind())
	if !ok {
		return m, nil
	}
	if _, ok := any(k).(kinds.PortForwarder); !ok {
		m.statusBar = m.statusBar.SetMessage("port-forward not supported for " + m.nav.ActiveKind())
		return m, nil
	}
	pod, ok := row.Raw.(*corev1.Pod)
	if !ok {
		return m, nil
	}

	// Toggle: if this pod already has at least one active forward, shift+f
	// stops all of them rather than opening a second forward.
	stopped := m.stopPodPortForwards(pod.Namespace, pod.Name)
	if stopped > 0 {
		m.statusBar = m.statusBar.SetMessage(fmt.Sprintf(
			"stopped %d port-forward(s) for %s/%s", stopped, pod.Namespace, pod.Name))
		if m.pfList.IsVisible() {
			m.pfList = m.pfList.SetSessions(m.pfManager.List())
		}
		return m, m.buildTableCmd()
	}

	options := portOptionsForPod(pod)
	m.pfDialog = m.pfDialog.Show("Pod", pod.Name, pod.Namespace, options)
	return m, nil
}

// stopPodPortForwards stops every active forward targeting the named pod and
// returns the number stopped. Returns 0 if none were active.
func (m *Model) stopPodPortForwards(namespace, name string) int {
	if m.pfManager == nil {
		return 0
	}
	var ids []string
	for _, s := range m.pfManager.List() {
		if s.Namespace == namespace && s.PodName == name {
			ids = append(ids, s.ID)
		}
	}
	for _, id := range ids {
		m.pfManager.Stop(id)
	}
	return len(ids)
}

// portOptionsForPod returns one PortOption per declared container port. Many
// container specs omit `ports` — in that case the dialog falls back to the
// custom-input path (see PortForwardDialog.Show with empty options).
func portOptionsForPod(pod *corev1.Pod) []widgets.PortOption {
	var options []widgets.PortOption
	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			options = append(options, widgets.PortOption{
				Container: c.Name,
				PortName:  p.Name,
				Port:      int(p.ContainerPort),
			})
		}
	}
	return options
}

// startPortForward kicks off a port-forward against the pod the dialog
// described. For Pod kind the dialog's Name *is* the pod name; future Service
// support would resolve a backing pod here first.
func (m Model) startPortForward(req widgets.PortForwardRequest) (Model, tea.Cmd) {
	if m.clusterMgr == nil {
		return m, nil
	}
	cs, err := m.clusterMgr.ActiveClientset()
	if err != nil || cs == nil {
		m.statusBar = m.statusBar.SetMessage("no cluster connection")
		return m, nil
	}
	cfg, err := m.clusterMgr.ActiveRestConfig()
	if err != nil {
		m.statusBar = m.statusBar.SetMessage("rest config: " + err.Error())
		return m, nil
	}
	return m, k8sops.StartPortForwardCmd(
		cs, cfg, m.msgCh,
		req.Kind, req.Name, req.Namespace, req.Name,
		req.LocalPort, req.RemotePort,
	)
}

func (m Model) actionAttach() (Model, tea.Cmd) {
	if m.readOnly {
		m.statusBar = m.statusBar.SetMessage("read-only mode")
		return m, nil
	}
	row := m.tableCtrl.SelectedRow()
	if row == nil {
		m.statusBar = m.statusBar.SetMessage("no pod selected")
		return m, nil
	}
	if m.nav.ActiveKind() != "Pod" {
		m.statusBar = m.statusBar.SetMessage("attach only available for Pods")
		return m, nil
	}
	pod, _ := row.Raw.(*corev1.Pod)
	entries := PodContainerEntries(pod)
	// No containers known (raw missing) — fall back to letting the API server
	// pick the default by passing an empty container name.
	if len(entries) == 0 {
		return m.attachToContainer(row.Namespace, row.Name, "")
	}
	// Single-container pod: attach immediately, no prompt.
	if len(entries) == 1 {
		return m.attachToContainer(row.Namespace, row.Name, entries[0].Name)
	}
	// Multi-container: let the user pick.
	m.containerPicker = m.containerPicker.Show(row.Name, row.Namespace, entries)
	return m, nil
}

// attachToContainer dispatches the attach command for a specific container.
// container may be empty, in which case the API server's default selection
// is used (k8s.detectContainer falls back to spec.Containers[0]).
func (m Model) attachToContainer(namespace, pod, container string) (Model, tea.Cmd) {
	if os.Getenv("TMUX") != "" {
		kubeCtx := ""
		if m.clusterMgr != nil {
			kubeCtx = m.clusterMgr.ActiveContext()
		}
		return m, k8sops.TmuxAttachWindowCmd(kubeCtx, namespace, pod, container)
	}
	// Non-tmux fallback: suspend TUI and exec directly.
	if m.clusterMgr == nil {
		return m, nil
	}
	cs, err := m.clusterMgr.ActiveClientset()
	if err != nil || cs == nil {
		m.statusBar = m.statusBar.SetMessage("no cluster connection")
		return m, nil
	}
	cfg, err := m.clusterMgr.ActiveRestConfig()
	if err != nil {
		m.statusBar = m.statusBar.SetMessage("rest config: " + err.Error())
		return m, nil
	}
	return m, k8sops.AttachCmd(cs, cfg, namespace, pod, container)
}

// PodContainerEntries flattens a pod's containers, init containers, and
// ephemeral containers into a single picker list, preserving spec order.
// Init/ephemeral entries are tagged so the picker can label them.
func PodContainerEntries(pod *corev1.Pod) []widgets.ContainerEntry {
	if pod == nil {
		return nil
	}
	out := make([]widgets.ContainerEntry, 0,
		len(pod.Spec.Containers)+len(pod.Spec.InitContainers)+len(pod.Spec.EphemeralContainers))
	for _, c := range pod.Spec.Containers {
		out = append(out, widgets.ContainerEntry{Name: c.Name, Kind: widgets.ContainerRegular})
	}
	for _, c := range pod.Spec.InitContainers {
		out = append(out, widgets.ContainerEntry{Name: c.Name, Kind: widgets.ContainerInit})
	}
	for _, c := range pod.Spec.EphemeralContainers {
		out = append(out, widgets.ContainerEntry{Name: c.Name, Kind: widgets.ContainerEphemeral})
	}
	return out
}

// handleYAMLViewKeys is reached only for keys the YAMLViewController refused
// ("e" and "ctrl+z"). These actions read state that lives on the root
// (m.readOnly + the rollback stash), so the controller defers to the model.
func (m Model) handleYAMLViewKeys(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "e":
		if m.readOnly {
			m.statusBar = m.statusBar.SetMessage("read-only mode")
			return m, nil
		}
		kind, name, ns := m.yamlViewCtrl.ResourceInfo()
		m.yamlEditCtrl = m.yamlEditCtrl.LoadYAML(kind, name, ns, m.yamlViewCtrl.RawYAML())
		m.mode = ModeEditor
		return m, nil
	case "ctrl+z":
		if m.readOnly || m.rollbackYAML == "" {
			return m, nil
		}
		kind, name, ns := m.yamlViewCtrl.ResourceInfo()
		if m.rollbackKind != kind || m.rollbackName != name || m.rollbackNS != ns {
			return m, nil
		}
		m.yamlEditCtrl = m.yamlEditCtrl.LoadYAML(kind, name, ns, m.rollbackYAML)
		m.rollbackYAML = ""
		m.mode = ModeEditor
		return m, nil
	}
	return m, nil
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
	name      string // single-resource ops (scale, suspend, etc.)
	namespace string
	targets   []deleteTarget // multi-delete
}

func (m Model) executeConfirmedOp(result widgets.ConfirmResult) (Model, tea.Cmd) {
	cs, err := m.clusterMgr.ActiveClientset()
	if err != nil {
		m.statusBar = m.statusBar.SetMessage("no client: " + err.Error())
		return m, nil
	}
	k, ok := kinds.Lookup(m.pendingOp.kind)
	if !ok {
		return m, nil
	}
	deps := m.depsFor(cs)
	build := func(ns, name string) tea.Cmd {
		switch m.pendingOp.op {
		case "delete":
			return kinds.DeleteCmd(k, deps, ns, name)
		case "kill":
			return kinds.KillCmd(k, deps, ns, name)
		case "suspend":
			return kinds.SuspendCmd(k, deps, ns, name, true)
		case "resume":
			return kinds.SuspendCmd(k, deps, ns, name, false)
		}
		return nil
	}
	// Multi-target ops (currently only delete) loop the action with each target.
	if len(m.pendingOp.targets) > 0 {
		m.tableCtrl = m.tableCtrl.ClearSelection()
		var cmds []tea.Cmd
		for _, t := range m.pendingOp.targets {
			if c := build(t.namespace, t.name); c != nil {
				cmds = append(cmds, c)
			}
		}
		return m, tea.Batch(cmds...)
	}
	cmd := build(m.pendingOp.namespace, m.pendingOp.name)
	if cmd == nil {
		return m, nil
	}
	return m, cmd
}

// depsFor bundles the cluster handles every kinds dispatch helper needs.
func (m Model) depsFor(cs kubernetes.Interface) kinds.Deps {
	deps := kinds.Deps{Clientset: cs}
	if m.watcher != nil {
		deps.Dynamic = m.watcher.DynamicClient()
		deps.HelmGVR = m.watcher.HelmReleaseGVR()
	}
	return deps
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
	m = m.resetToTable()
	m.tableCtrl = m.tableCtrl.ClearSelection()

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
	m.syncing = true
	m.tableCtrl = m.tableCtrl.SetSyncing(true)
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
	if m.pfManager != nil {
		// Port-forwards target the previous cluster's pods; the local sockets
		// would still accept connections after the switch but proxy to nothing.
		m.pfManager.StopAll()
	}
	m = m.resetToTable()
	m.clearEventsViewState()

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
	m.syncing = true
	m.tableCtrl = m.tableCtrl.SetSyncing(true)
	return m, tea.Batch(
		k8sops.WatchCmd(m.msgCh),
		m.buildTableCmd(),
		k8sops.MetricsTickCmd(),
	)
}

func (m Model) buildXRay(kind, name string) *k8sops.TreeNode {
	if m.watcher == nil {
		return nil
	}
	rd, ok := k8sops.Resolve(kind)
	if !ok || rd.BuildXRay == nil {
		return nil
	}
	return rd.BuildXRay(m.watcher, m.namespace, name)
}

// kindSyncing reports whether the watcher's informer for `kind` has not yet
// finished its initial LIST. Used to render the "Syncing <kind>…" placeholder
// in the resource table when the user navigates to a not-yet-synced kind.
func (m Model) kindSyncing(kind string) bool {
	if m.watcher == nil {
		return true
	}
	return !m.watcher.KindSynced(kind)
}

// setKindAndSync swaps the resource table's active kind and updates its
// syncing badge to match whether that informer has data ready yet. Events-
// view state lives on the model rather than the panel, so this is the seam
// where leaving the Event kind clears the faults / wrap flags. Re-entering
// Event starts clean, matching the lifetime of every other events-only
// piece of state.
func (m *Model) clearEventsViewState() {
	m.eventFaultsOnly = false
	m.eventWrapMessage = false
	m.tableCtrl = m.tableCtrl.ClearWrapColumn()
	m.tableCtrl = m.tableCtrl.SetTitleBadge("")
	m.pendingJumpName = ""
	m.pendingJumpKind = ""
	m.pendingJumpTries = 0
}

func (m *Model) setKindAndSync(kind string) modes.TableController {
	if kind != "Event" {
		m.clearEventsViewState()
	}
	return m.tableCtrl.SetKind(kind).SetSyncing(m.kindSyncing(kind))
}

func (m *Model) setStatusBarKind(kind string) {
	m.statusBar = m.statusBar.SetActiveKind(kind)
	if kind == "Event" {
		m.statusBar = m.statusBar.SetHelp(eventsHelp())
		return
	}
	help := []panels.HelpItem{
		{Key: "↑↓/jk", Desc: "navigate"},
		{Key: "enter", Desc: "focus"},
		{Key: "/", Desc: "filter"},
	}
	k, ok := kinds.Lookup(kind)
	if !ok {
		m.statusBar = m.statusBar.SetHelp(help)
		return
	}
	// Every Kind implements Fetch — YAML view + Describe are always available.
	help = append(help, panels.HelpItem{Key: "y", Desc: "yaml"})
	help = append(help, panels.HelpItem{Key: "d", Desc: "describe"})
	if _, ok := any(k).(kinds.Logger); ok {
		help = append(help, panels.HelpItem{Key: "l", Desc: "logs"})
	}
	if _, ok := any(k).(kinds.XRayer); ok {
		help = append(help, panels.HelpItem{Key: "x", Desc: "xray"})
	}
	if _, ok := any(k).(kinds.MetricsSupporter); ok {
		help = append(help, panels.HelpItem{Key: "m", Desc: "metrics"})
	}
	if _, ok := any(k).(kinds.PortForwarder); ok {
		help = append(help, panels.HelpItem{Key: "shift+f", Desc: "port-forward"})
	}
	if !m.readOnly {
		if _, ok := any(k).(kinds.Attacher); ok {
			help = append(help, panels.HelpItem{Key: "a", Desc: "attach"})
		}
		if _, ok := any(k).(kinds.Scaler); ok {
			help = append(help, panels.HelpItem{Key: "s", Desc: "scale"})
		}
		if _, ok := any(k).(kinds.Suspender); ok {
			help = append(help,
				panels.HelpItem{Key: "s", Desc: "suspend"},
				panels.HelpItem{Key: "r", Desc: "resume"},
			)
		}
		if _, ok := any(k).(kinds.Deleter); ok {
			help = append(help, panels.HelpItem{Key: "ctrl+d", Desc: "delete"})
		}
		if _, ok := any(k).(kinds.Killer); ok {
			help = append(help, panels.HelpItem{Key: "ctrl+k", Desc: "kill"})
		}
		if kind == "Pod" {
			help = append(help, panels.HelpItem{Key: "z", Desc: "sanitize"})
		}
	}
	for _, c := range k.Columns() {
		if c.Scrollable {
			help = append(help, panels.HelpItem{Key: "←/→", Desc: "scroll col"})
			break
		}
	}
	help = append(help,
		panels.HelpItem{Key: "ctrl+r", Desc: "refresh"},
		panels.HelpItem{Key: "ctrl+c/q", Desc: "quit"},
	)
	m.statusBar = m.statusBar.SetHelp(help)
}

// eventsHelp returns the events-view help footer. The hidden keys are
// ctrl+d / e / a (event has no Deleter / Applier / Attacher); their
// suppression is mirrored by status-bar feedback in handleTableKeys.
func eventsHelp() []panels.HelpItem {
	return []panels.HelpItem{
		{Key: "↑↓/jk", Desc: "navigate"},
		{Key: "/", Desc: "filter"},
		{Key: "ctrl+z", Desc: "faults"},
		{Key: "w", Desc: "wrap"},
		{Key: "o", Desc: "object"},
		{Key: "y", Desc: "yaml"},
		{Key: "d", Desc: "describe"},
		{Key: "esc", Desc: "back"},
	}
}

// currentReplicas extracts the replica count from a ResourceRow's raw object
// via the Scaler.CurrentReplicas method on the matching kind. Defaults to 1
// when no kind (or no Scaler) is found.
func currentReplicas(kind string, row *k8sops.ResourceRow) int32 {
	k, ok := kinds.Lookup(kind)
	if !ok {
		return 1
	}
	s, ok := any(k).(kinds.Scaler)
	if !ok {
		return 1
	}
	return s.CurrentReplicas(row.Raw)
}

// View renders the full TUI.
func (m Model) View() tea.View {
	v := tea.NewView(m.renderContent())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// WheelAtBoundary reports whether a wheel event in `button`'s direction would
// be a pure no-op against whichever panel currently owns scroll. The Bubbletea
// program is configured with tea.WithFilter to drop these events before Update
// runs, which also skips the full TUI View() that would otherwise fire per
// event. Without this, trackpad momentum scroll at a boundary makes the app
// feel unresponsive while queued wheel events drain through render.
//
// Returns false during loading, when any modal is up (modals have their own
// scroll), in ModeEditor (textarea cursor — different notion of boundary),
// and when nav has focus (nav cycles, no boundary).
func (m Model) WheelAtBoundary(button tea.MouseButton) bool {
	if m.loading {
		return false
	}
	if m.namespacePicker.IsVisible() ||
		m.clusterPicker.IsVisible() ||
		m.containerPicker.IsVisible() ||
		m.confirm.IsVisible() ||
		m.scaleDialog.IsVisible() ||
		m.sanitizeDialog.IsVisible() ||
		m.contextMenu.IsVisible() ||
		m.pfDialog.IsVisible() ||
		m.pfList.IsVisible() {
		return false
	}
	if m.focus == FocusNav {
		return false
	}
	switch m.mode {
	case ModeTable:
		return m.tableCtrl.WheelAtBoundary(button)
	case ModeYAML:
		return m.yamlViewCtrl.WheelAtBoundary(button)
	case ModeLogs:
		return m.logsCtrl.WheelAtBoundary(button)
	case ModeXRay:
		return m.xrayCtrl.WheelAtBoundary(button)
	case ModeMetrics:
		return m.metricsCtrl.WheelAtBoundary(button)
	case ModeDescribe:
		return m.describeCtrl.WheelAtBoundary(button)
	}
	return false
}

// renderContent builds the textual content of the View. When a modal is open,
// it is centered on the full terminal so it remains visible (the base view fills
// the entire terminal height after the lipgloss v2 width/height fix, so simply
// appending the modal below would push it off-screen).
// hitMap builds the screen hit-region registry for the current layout state.
// Rebuilt per mouse event — a handful of rect appends — so regions can never
// go stale relative to the layout (herdr's recompute-per-frame pattern,
// adapted to Bubbletea's event loop).
func (m Model) hitMap() hit.Map {
	var zones hit.Map
	if m.fullScreen {
		zones.Add(hit.ZoneContent, m.layout.Fullscreen())
		return zones
	}
	zones.Add(hit.ZoneHeader, m.layout.Header())
	zones.Add(hit.ZoneNav, m.layout.Nav())
	zones.Add(hit.ZoneContent, m.layout.Content())
	zones.Add(hit.ZoneStatus, m.layout.Status())
	return zones
}

// contentRect is the content panel's outer screen rect honoring fullscreen.
// Drag handlers use it directly (not via hitMap) because in-flight drags must
// keep translating coordinates even when the pointer leaves the panel.
func (m Model) contentRect() layout.Rect {
	if m.fullScreen {
		return m.layout.Fullscreen()
	}
	return m.layout.Content()
}

func (m Model) renderContent() string {
	if m.layout.TooSmall() {
		return renderTooSmall(m.layout)
	}

	if m.loading {
		return renderLoading(m.layout, m.reconnecting)
	}

	if m.namespacePicker.IsVisible() {
		return m.modalOverlay(m.namespacePicker.View())
	}

	if m.clusterPicker.IsVisible() {
		return m.modalOverlay(m.clusterPicker.View())
	}

	if m.containerPicker.IsVisible() {
		return m.modalOverlay(m.containerPicker.View())
	}

	if m.confirm.IsVisible() {
		return m.modalOverlay(m.confirm.View())
	}

	if m.scaleDialog.IsVisible() {
		return m.modalOverlay(m.scaleDialog.View())
	}

	if m.sanitizeDialog.IsVisible() {
		return m.modalOverlay(m.sanitizeDialog.View())
	}

	if m.contextMenu.IsVisible() {
		return m.modalOverlay(m.contextMenu.View())
	}

	if m.pfDialog.IsVisible() {
		return m.modalOverlay(m.pfDialog.View())
	}

	if m.pfList.IsVisible() {
		return m.modalOverlay(m.pfList.View())
	}

	return m.baseView()
}

// modalOverlay centers a modal's pre-rendered output on the full terminal,
// using ColorAbyss as the surrounding backdrop.
func (m Model) modalOverlay(modal string) string {
	termW, termH := m.layout.TermSize()
	return lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(styles.ColorAbyss)))
}

func (m Model) baseView() string {
	if m.fullScreen {
		return m.contentView()
	}
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
		return m.yamlViewCtrl.View()
	case ModeEditor:
		return m.yamlEditCtrl.View()
	case ModeLogs:
		return m.logsCtrl.View()
	case ModeXRay:
		return m.xrayCtrl.View()
	case ModeMetrics:
		return m.metricsCtrl.View()
	case ModeDescribe:
		return m.describeCtrl.View()
	default:
		return m.tableCtrl.View()
	}
}

func renderTooSmall(l layout.Layout) string {
	w, h := l.TermSize()
	msg := fmt.Sprintf("  Terminal too small: %d×%d  (need %d×%d)",
		w, h, layout.MinTermWidth, layout.MinTermHeight)
	return styles.Warning.Bold(true).Render(msg) + strings.Repeat("\n", max(0, h-1))
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
	termW, termH := m.layout.TermSize()
	m.header = m.header.SetWidth(m.layout.Header().Width)
	m.statusBar = m.statusBar.SetWidth(m.layout.Status().Width)
	m.nav = m.nav.SetSize(navDim.Width, navDim.Height)
	m.tableCtrl = m.tableCtrl.SetSize(contentDim.Width, contentDim.Height).(modes.TableController)

	// In fullscreen the active content panel uses the entire terminal; the
	// non-fullscreen panels are kept at normal size so their state stays
	// coherent for the toggle-off render. Table never goes fullscreen.
	cw, ch := contentDim.Width, contentDim.Height
	if m.fullScreen {
		cw, ch = termW, termH
	}
	m.yamlViewCtrl = m.yamlViewCtrl.SetSize(cw, ch).(modes.YAMLViewController)
	m.yamlEditCtrl = m.yamlEditCtrl.SetSize(cw, ch).(modes.YAMLEditController)
	m.logsCtrl = m.logsCtrl.SetSize(cw, ch).(modes.LogsController)
	m.xrayCtrl = m.xrayCtrl.SetSize(cw, ch).(modes.XRayController)
	m.metricsCtrl = m.metricsCtrl.SetSize(cw, ch).(modes.MetricsController)
	m.describeCtrl = m.describeCtrl.SetSize(cw, ch).(modes.DescribeViewController)

	m.contextMenu = m.contextMenu.SetSize(termW, termH)
	m.confirm = m.confirm.SetSize(termW, termH)
	return m
}

func (m Model) connectCmd() tea.Cmd {
	ch := m.msgCh
	// Preserve the in-app active context across refresh (ctrl+r). On first
	// connect m.clusterMgr is nil, so we fall through to kubeconfig's
	// current-context. After the user has switched via ctrl+k, that selection
	// would otherwise be lost when we reload the kubeconfig below.
	preferredCtx := ""
	if m.clusterMgr != nil {
		preferredCtx = m.clusterMgr.ActiveContext()
	}
	return func() tea.Msg {
		mgr, err := cluster.New()
		if err != nil {
			return errMsg{err: fmt.Errorf("kubeconfig: %w", err)}
		}
		if preferredCtx != "" && preferredCtx != mgr.ActiveContext() {
			if err := mgr.SwitchContext(preferredCtx); err != nil {
				return errMsg{err: fmt.Errorf("restore context %q: %w", preferredCtx, err), ctx: mgr.ActiveContext()}
			}
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
	rd, ok := k8sops.Resolve(kind)
	if !ok || rd.ListRows == nil {
		return nil
	}
	return rd.ListRows(m.watcher, m.namespace, k8sops.RowContext{
		Metrics:           m.metricsData,
		PortForwardActive: m.podHasActivePortForward,
	})
}

// podHasActivePortForward reports whether any tracked port-forward session is
// running against the given pod. Wired into RowContext so BuildPodRows can
// render the PF column without depending on the app package directly.
func (m Model) podHasActivePortForward(namespace, name string) bool {
	if m.pfManager == nil {
		return false
	}
	for _, s := range m.pfManager.List() {
		if s.Namespace == namespace && s.PodName == name {
			return true
		}
	}
	return false
}

func (m *Model) stopAll() {
	if m.watcher != nil {
		m.watcher.Stop()
	}
	if m.logStreamer != nil {
		m.logStreamer.Stop()
	}
	if m.pfManager != nil {
		m.pfManager.StopAll()
	}
}
