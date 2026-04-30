package k8s

import (
	"context"
	"fmt"
	"net"
	"net/http"

	tea "github.com/charmbracelet/bubbletea"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PortForwardSession represents an active port-forward.
type PortForwardSession struct {
	ID         string
	Resource   string
	Namespace  string
	LocalPort  int
	RemotePort int
	stopChan   chan struct{}
	readyChan  chan struct{}
}

// PortForwardReadyMsg is sent when a port-forward is established.
type PortForwardReadyMsg struct {
	ID        string
	LocalPort int
	Err       error
}

// PortForwardStoppedMsg is sent when a port-forward ends.
type PortForwardStoppedMsg struct {
	ID string
}

// StartPortForwardCmd begins a port-forward for the given pod.
func StartPortForwardCmd(cs *kubernetes.Clientset, cfg *rest.Config, namespace, podName string, localPort, remotePort int) tea.Cmd {
	return func() tea.Msg {
		if localPort == 0 {
			// Pick an ephemeral port
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

		errChan := make(chan error, 1)
		go func() {
			errChan <- fw.ForwardPorts()
		}()

		// Wait for ready or error
		select {
		case <-readyChan:
		case err := <-errChan:
			return PortForwardReadyMsg{Err: err}
		}

		id := fmt.Sprintf("%s/%s:%d->%d", namespace, podName, localPort, remotePort)
		return PortForwardReadyMsg{
			ID:        id,
			LocalPort: localPort,
		}
	}
}

// StopPortForward closes the stop channel for the given session.
func StopPortForward(session *PortForwardSession) {
	if session != nil && session.stopChan != nil {
		close(session.stopChan)
	}
}

// suppress unused import
var _ = context.Background
