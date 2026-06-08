package termsession

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// TestLiveExec is an opt-in end-to-end check of the SPDY → emulator →
// input-pump loop against a real cluster pod:
//
//	KLENS_TERMTEST_NS=<ns> KLENS_TERMTEST_POD=<pod> go test ./internal/k8s/termsession/ -run TestLiveExec -v
//
// It opens a shell, runs a marker echo, asserts the marker lands on the
// emulator screen, then exits the shell and asserts clean stream teardown.
func TestLiveExec(t *testing.T) {
	pod := os.Getenv("KLENS_TERMTEST_POD")
	ns := os.Getenv("KLENS_TERMTEST_NS")
	if pod == "" || ns == "" {
		t.Skip("set KLENS_TERMTEST_POD and KLENS_TERMTEST_NS to run the live exec test")
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = filepath.Join(home, ".kube", "config")
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("kubeconfig: %v", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("clientset: %v", err)
	}

	ch := make(chan tea.Msg, 16)
	s := New(cs, cfg, ns, pod, "", 80, 24, ch)
	defer s.Close()

	// Stand-in for the app's WatchCmd pump: ack every output notification.
	go func() {
		for msg := range ch {
			if om, ok := msg.(OutputMsg); ok {
				om.Session.AckOutput()
			}
		}
	}()

	exited := make(chan tea.Msg, 1)
	go func() { exited <- s.Start()() }()

	waitFor := func(want string, d time.Duration) {
		t.Helper()
		deadline := time.Now().Add(d)
		for time.Now().Before(deadline) {
			if strings.Contains(s.Render(), want) {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("screen never showed %q; screen:\n%s", want, s.Render())
	}

	// Prompt up (any shell draws something); then run the marker.
	s.SendText("echo klens-term-$((6*7))\n")
	waitFor("klens-term-42", 15*time.Second)

	if st, _ := s.Info(); st != StatusRunning {
		t.Fatalf("status = %v, want StatusRunning", st)
	}

	s.SendText("exit\n")
	select {
	case msg := <-exited:
		em, ok := msg.(ExitedMsg)
		if !ok {
			t.Fatalf("Start returned %T, want ExitedMsg", msg)
		}
		if em.Err != nil {
			t.Logf("exit err (often non-nil for exit-code style closes): %v", em.Err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("stream did not end after exit")
	}
	if st, _ := s.Info(); st != StatusExited {
		t.Fatalf("status = %v, want StatusExited", st)
	}
}
