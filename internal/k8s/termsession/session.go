// Package termsession owns embedded kubectl-exec terminal sessions: each
// Session wires a Kubernetes SPDY exec stream into an in-process VT emulator
// (charmbracelet/x/vt) so the shell renders inside the TUI instead of a tmux
// window. The app package holds the session registry and renders the active
// session's screen through the terminal dock panel.
package termsession

import (
	"context"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

// Status is the lifecycle state of a session.
type Status int

const (
	StatusRunning Status = iota
	StatusExited
)

// OutputMsg signals that a session's emulator received output and the dock
// should re-render. Delivered through the app's msgCh (the WatchCmd pump),
// coalesced: at most one OutputMsg is in flight per session until the app
// acknowledges it with AckOutput.
type OutputMsg struct{ Session *Session }

// ExitedMsg is returned by the Start command when the exec stream ends —
// shell exit, connection error, or context cancellation.
type ExitedMsg struct {
	Session *Session
	Err     error
}

// idCounter generates unique session IDs across the process lifetime.
var idCounter atomic.Int64

// Session is one embedded exec session: SPDY stream + VT emulator.
//
// Concurrency model: the emulator is mutated from two goroutines — the SPDY
// reader (output → emu.Write) and the Bubbletea update loop (keys →
// emu.SendKey, plus Resize/Render). All emulator access goes through mu.
// The input path (emu.Read) is deliberately NOT under mu: it drains the
// emulator's internal synchronous pipe and must never hold the lock while
// blocked. A dedicated pump goroutine moves encoded input bytes from the
// emulator into an unbounded buffer that serves as the stream's stdin, so
// SendKey never blocks the UI even before the remote shell is connected.
type Session struct {
	ID        string
	Pod       string
	Namespace string
	Container string

	cs  kubernetes.Interface
	cfg *rest.Config

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	emu     *vt.Emulator
	status  Status
	exitErr error

	// View state (scrollback browsing + text selection), guarded by mu. See
	// view.go. following starts true: pinned to the live bottom, identical to
	// the pre-scrollback behaviour.
	following bool
	scrollTop int
	sel       selection

	stdin  *inputBuffer
	sizeCh chan remotecommand.TerminalSize

	// notifyPending coalesces output notifications: set when an OutputMsg is
	// queued, cleared by AckOutput once the app has processed it.
	notifyPending atomic.Bool
	msgCh         chan<- tea.Msg
}

// New creates a session with a w×h emulator. The stream is not opened until
// Start's command runs. msgCh is the app's message pump channel (the same one
// WatchCmd drains); output notifications are delivered through it.
func New(cs kubernetes.Interface, cfg *rest.Config, namespace, pod, container string, w, h int, msgCh chan<- tea.Msg) *Session {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{
		ID:        fmt.Sprintf("term-%d", idCounter.Add(1)),
		Pod:       pod,
		Namespace: namespace,
		Container: container,
		cs:        cs,
		cfg:       cfg,
		ctx:       ctx,
		cancel:    cancel,
		emu:       vt.NewEmulator(max(w, 1), max(h, 1)),
		stdin:     newInputBuffer(),
		sizeCh:    make(chan remotecommand.TerminalSize, 1),
		msgCh:     msgCh,
		following: true,
	}
	s.sizeCh <- remotecommand.TerminalSize{Width: uint16(max(w, 1)), Height: uint16(max(h, 1))}
	go s.pumpInput()
	return s
}

// Start returns the command that opens the exec stream and blocks until it
// ends. The returned message is always an ExitedMsg.
func (s *Session) Start() tea.Cmd {
	return func() tea.Msg {
		err := s.run()
		s.mu.Lock()
		s.status = StatusExited
		s.exitErr = err
		if err != nil && s.ctx.Err() == nil {
			// Surface the failure on the session's own screen so the exited
			// tab explains itself (closed-by-user sessions skip this).
			_, _ = s.emu.WriteString("\r\n[session error: " + err.Error() + "]\r\n")
		}
		s.mu.Unlock()
		return ExitedMsg{Session: s, Err: err}
	}
}

func (s *Session) run() error {
	container := s.Container
	if container == "" {
		container = detectContainer(s.ctx, s.cs, s.Namespace, s.Pod)
	}
	req := s.cs.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(s.Namespace).
		Name(s.Pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			// Same shell fallback chain the old tmux path used. With TTY on,
			// stderr is merged into stdout by the kubelet, so Stderr stays off.
			Command: []string{"sh", "-c", "clear; (bash || ash || sh)"},
			Stdin:   true,
			Stdout:  true,
			TTY:     true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(s.cfg, "POST", req.URL())
	if err != nil {
		return err
	}
	return executor.StreamWithContext(s.ctx, remotecommand.StreamOptions{
		Stdin:             s.stdin,
		Stdout:            outputWriter{s},
		Tty:               true,
		TerminalSizeQueue: sizeQueue{ch: s.sizeCh, ctx: s.ctx},
	})
}

// pumpInput moves encoded input bytes (SendKey/SendText/Paste output) from
// the emulator's synchronous internal pipe into the unbounded stdin buffer.
// It exits when the emulator is closed.
func (s *Session) pumpInput() {
	buf := make([]byte, 4096)
	for {
		n, err := s.emu.Read(buf)
		if n > 0 {
			_, _ = s.stdin.Write(buf[:n])
		}
		if err != nil {
			s.stdin.Close()
			return
		}
	}
}

// Close cancels the stream and tears down the emulator pipes. Idempotent.
func (s *Session) Close() {
	s.cancel()
	s.mu.Lock()
	s.status = StatusExited
	_ = s.emu.Close()
	s.mu.Unlock()
}

// AckOutput re-arms output notification after the app processed an OutputMsg.
// Call it from the Update handler before the next View render: output that
// lands between the ack and the render simply queues one more (cheap) msg.
func (s *Session) AckOutput() { s.notifyPending.Store(false) }

// Resize resizes the emulator and tells the remote PTY (latest size wins).
func (s *Session) Resize(w, h int) {
	if w < 1 || h < 1 {
		return
	}
	s.mu.Lock()
	cur := s.emu.Bounds()
	if cur.Dx() == w && cur.Dy() == h {
		s.mu.Unlock()
		return
	}
	s.emu.Resize(w, h)
	s.mu.Unlock()
	// Latest-wins push: drop a stale queued size before offering the new one.
	select {
	case <-s.sizeCh:
	default:
	}
	select {
	case s.sizeCh <- remotecommand.TerminalSize{Width: uint16(w), Height: uint16(h)}:
	default:
	}
}

// SendKey encodes a key event for the remote shell. No-op on exited sessions.
func (s *Session) SendKey(k uv.KeyEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != StatusRunning {
		return
	}
	s.emu.SendKey(k)
}

// SendText sends printable text (the emulator's SendKey drops Key.Text, so
// printable input goes through here).
func (s *Session) SendText(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != StatusRunning {
		return
	}
	s.emu.SendText(text)
}

// Paste sends pasted text, bracketed when the remote enabled bracketed paste.
func (s *Session) Paste(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != StatusRunning {
		return
	}
	s.emu.Paste(text)
}

// Render snapshots the emulator screen as an ANSI-styled string.
func (s *Session) Render() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emu.Render()
}

// CursorPos returns the emulator-local cursor cell.
func (s *Session) CursorPos() (x, y int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.emu.CursorPosition()
	return p.X, p.Y
}

// Info returns the lifecycle status and exit error (nil while running).
func (s *Session) Info() (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.exitErr
}

// Title is the tab label: the pod name, plus the container when one was
// explicitly picked (multi-container pods).
func (s *Session) Title() string {
	if s.Container != "" {
		return s.Pod + "/" + s.Container
	}
	return s.Pod
}

// outputWriter feeds remote output into the emulator and queues a coalesced
// OutputMsg so the app re-renders. The channel send blocks when the app's
// pump is saturated — that is deliberate backpressure on the SPDY reader.
type outputWriter struct{ s *Session }

func (w outputWriter) Write(p []byte) (int, error) {
	w.s.mu.Lock()
	n, err := w.s.emu.Write(p)
	w.s.mu.Unlock()
	if n > 0 && !w.s.notifyPending.Swap(true) {
		w.s.msgCh <- OutputMsg{Session: w.s}
	}
	return n, err
}

// sizeQueue adapts the latest-wins size channel to remotecommand's
// TerminalSizeQueue. Returning nil ends the remote resize stream, which
// happens when the session context is cancelled.
type sizeQueue struct {
	ch  chan remotecommand.TerminalSize
	ctx context.Context
}

func (q sizeQueue) Next() *remotecommand.TerminalSize {
	select {
	case sz := <-q.ch:
		return &sz
	case <-q.ctx.Done():
		return nil
	}
}

// inputBuffer is an unbounded, closable byte queue: Write never blocks,
// Read blocks until data arrives or the buffer is closed (then io.EOF).
// It decouples the emulator's synchronous input pipe from the SPDY stdin
// reader, which may not exist yet (stream still connecting) or anymore
// (stream ended).
type inputBuffer struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newInputBuffer() *inputBuffer {
	b := &inputBuffer{}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *inputBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	b.buf = append(b.buf, p...)
	b.cond.Signal()
	return len(p), nil
}

func (b *inputBuffer) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.buf) == 0 && !b.closed {
		b.cond.Wait()
	}
	if len(b.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.buf)
	b.buf = b.buf[n:]
	return n, nil
}

func (b *inputBuffer) Close() {
	b.mu.Lock()
	b.closed = true
	b.cond.Broadcast()
	b.mu.Unlock()
}

// detectContainer returns the first container name in the pod spec, or ""
// if the pod can't be fetched (the API server will then use its default).
func detectContainer(ctx context.Context, cs kubernetes.Interface, namespace, podName string) string {
	pod, err := cs.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil || len(pod.Spec.Containers) == 0 {
		return ""
	}
	return pod.Spec.Containers[0].Name
}
