package k8s

import (
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"

	tea "charm.land/bubbletea/v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwardSession represents an active port-forward.
//
// stopChan is closed to terminate the forward. ID is a stable, human-readable
// handle suitable for display in a listing modal.
type PortForwardSession struct {
	ID         string
	Kind       string // "Pod" today; "Service" in a future revision.
	Resource   string // ns/name of the originating resource
	PodName    string // the actual pod the forward runs against
	Namespace  string
	LocalPort  int
	RemotePort int

	stopChan chan struct{}
	stopOnce sync.Once
}

// Stop terminates the port-forward. Safe to call multiple times.
func (s *PortForwardSession) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.stopChan != nil {
			close(s.stopChan)
		}
	})
}

// PortForwardReadyMsg is sent when a port-forward is established or fails to
// establish. On success, Session is non-nil and the local port is bound.
type PortForwardReadyMsg struct {
	Session *PortForwardSession
	Err     error
}

// PortForwardClosedMsg is sent when an established port-forward terminates
// (clean stop or runtime error). Pushed via the model's msgCh because the
// forward goroutine outlives the tea.Cmd that started it.
type PortForwardClosedMsg struct {
	ID  string
	Err error // nil on clean stop
}

// StartPortForwardCmd begins a port-forward against the given pod. The returned
// command blocks the bubbletea worker until the forward is established or
// errors out — the long-running ForwardPorts loop is moved off to a goroutine
// which reports completion to msgCh as a PortForwardClosedMsg.
//
// localPort == 0 selects an ephemeral port. ForwardPorts() races the listener
// close on rare occasions; surface the resulting "address in use" error to the
// caller rather than retrying silently.
func StartPortForwardCmd(
	cs kubernetes.Interface,
	cfg *rest.Config,
	msgCh chan tea.Msg,
	kind, resourceName, namespace, podName string,
	localPort, remotePort int,
) tea.Cmd {
	return func() tea.Msg {
		if localPort == 0 {
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return PortForwardReadyMsg{Err: fmt.Errorf("find free port: %w", err)}
			}
			localPort = l.Addr().(*net.TCPAddr).Port
			l.Close()
		}

		roundTripper, upgrader, err := spdy.RoundTripperFor(cfg)
		if err != nil {
			return PortForwardReadyMsg{Err: fmt.Errorf("spdy roundtripper: %w", err)}
		}

		req := cs.CoreV1().RESTClient().Post().
			Resource("pods").
			Namespace(namespace).
			Name(podName).
			SubResource("portforward")

		dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, req.URL())

		stopChan := make(chan struct{})
		readyChan := make(chan struct{})
		ports := []string{fmt.Sprintf("%d:%d", localPort, remotePort)}

		fw, err := portforward.New(dialer, ports, stopChan, readyChan, nil, nil)
		if err != nil {
			return PortForwardReadyMsg{Err: fmt.Errorf("portforward: %w", err)}
		}

		session := &PortForwardSession{
			ID:         fmt.Sprintf("%s/%s:%d->%d", namespace, podName, localPort, remotePort),
			Kind:       kind,
			Resource:   namespace + "/" + resourceName,
			PodName:    podName,
			Namespace:  namespace,
			LocalPort:  localPort,
			RemotePort: remotePort,
			stopChan:   stopChan,
		}

		errChan := make(chan error, 1)
		go func() {
			errChan <- fw.ForwardPorts()
		}()

		select {
		case <-readyChan:
			// Established. Watch for the eventual close in the background and
			// notify the UI via msgCh so the manager can drop the dead session.
			go func() {
				err := <-errChan
				if msgCh != nil {
					msgCh <- PortForwardClosedMsg{ID: session.ID, Err: err}
				}
			}()
			return PortForwardReadyMsg{Session: session}

		case err := <-errChan:
			return PortForwardReadyMsg{Err: err}
		}
	}
}

// PortForwardManager tracks active port-forward sessions for the running TUI.
// Keys are session IDs. All methods are safe for concurrent use because
// PortForwardClosedMsg arrives on the bubbletea worker goroutine while Update
// may also be touching the map.
type PortForwardManager struct {
	mu       sync.Mutex
	sessions map[string]*PortForwardSession
}

// NewPortForwardManager returns an empty manager.
func NewPortForwardManager() *PortForwardManager {
	return &PortForwardManager{sessions: map[string]*PortForwardSession{}}
}

// Add registers a newly established session.
func (m *PortForwardManager) Add(s *PortForwardSession) {
	if m == nil || s == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
}

// Get returns the session with the given ID, or nil.
func (m *PortForwardManager) Get(id string) *PortForwardSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// Remove drops the session from the map without stopping it (called from the
// PortForwardClosedMsg path where the forward has already ended on its own).
func (m *PortForwardManager) Remove(id string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// Stop terminates and removes the session with the given ID. Returns true if
// a session was found.
func (m *PortForwardManager) Stop(id string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	s.Stop()
	return true
}

// StopAll terminates every active session.
func (m *PortForwardManager) StopAll() {
	if m == nil {
		return
	}
	m.mu.Lock()
	sessions := make([]*PortForwardSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = map[string]*PortForwardSession{}
	m.mu.Unlock()
	for _, s := range sessions {
		s.Stop()
	}
}

// List returns a snapshot of active sessions, sorted by ID for stable
// rendering. Slice elements are pointers to the live sessions — read-only.
func (m *PortForwardManager) List() []*PortForwardSession {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	out := make([]*PortForwardSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	m.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Len returns the number of active sessions.
func (m *PortForwardManager) Len() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}
