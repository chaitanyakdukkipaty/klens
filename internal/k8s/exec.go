package k8s

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

// ExecSession records an active exec session running in a tmux window.
type ExecSession struct {
	Pod       string
	Namespace string
	WindowID  string // tmux window index
}

// AttachFinishedMsg is delivered after a non-tmux tea.Exec session ends.
type AttachFinishedMsg struct {
	Pod string
	Err error
}

// TmuxWindowOpenedMsg is delivered after attempting to open a tmux exec window.
type TmuxWindowOpenedMsg struct {
	Session ExecSession
	Err     error
}

// PodExecCommand implements tea.ExecCommand for a non-tmux interactive session.
type PodExecCommand struct {
	cs        *kubernetes.Clientset
	cfg       *rest.Config
	namespace string
	pod       string
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
}

func (c *PodExecCommand) SetStdin(r io.Reader)  { c.stdin = r }
func (c *PodExecCommand) SetStdout(w io.Writer) { c.stdout = w }
func (c *PodExecCommand) SetStderr(w io.Writer) { c.stderr = w }

func (c *PodExecCommand) Run() error {
	container := detectContainer(c.cs, c.namespace, c.pod)
	req := c.cs.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(c.namespace).
		Name(c.pod).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   []string{"/bin/sh"},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       true,
		}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(c.cfg, "POST", req.URL())
	if err != nil {
		return err
	}
	return executor.StreamWithContext(context.Background(), remotecommand.StreamOptions{
		Stdin:  c.stdin,
		Stdout: c.stdout,
		Stderr: c.stderr,
		Tty:    true,
	})
}

// AttachCmd suspends the TUI and runs an interactive shell in the pod (non-tmux fallback).
func AttachCmd(cs *kubernetes.Clientset, cfg *rest.Config, namespace, pod string) tea.Cmd {
	c := &PodExecCommand{cs: cs, cfg: cfg, namespace: namespace, pod: pod}
	return tea.Exec(c, func(err error) tea.Msg {
		return AttachFinishedMsg{Pod: pod, Err: err}
	})
}

// TmuxAttachWindowCmd opens a new tmux window running kubectl exec into the pod.
// It captures the new window's index so the TUI can switch to it later.
func TmuxAttachWindowCmd(namespace, pod, container string) tea.Cmd {
	return func() tea.Msg {
		var kubectlCmd string
		if container != "" {
			kubectlCmd = fmt.Sprintf(
				"kubectl exec -i -t -n %s %s -c %s -- sh -c 'clear; (bash || ash || sh)'",
				namespace, pod, container)
		} else {
			kubectlCmd = fmt.Sprintf(
				"kubectl exec -i -t -n %s %s -- sh -c 'clear; (bash || ash || sh)'",
				namespace, pod)
		}
		windowName := pod
		if len(windowName) > 30 {
			windowName = windowName[:30]
		}
		out, err := exec.Command("tmux", "new-window",
			"-P", "-F", "#{window_index}",
			"-n", windowName,
			kubectlCmd,
		).Output()
		if err != nil {
			return TmuxWindowOpenedMsg{
				Session: ExecSession{Pod: pod, Namespace: namespace},
				Err:     err,
			}
		}
		return TmuxWindowOpenedMsg{
			Session: ExecSession{
				Pod:       pod,
				Namespace: namespace,
				WindowID:  strings.TrimSpace(string(out)),
			},
		}
	}
}

// detectContainer returns the first container name in the pod spec, or ""
// if the pod can't be fetched (the API server will then use its default).
func detectContainer(cs *kubernetes.Clientset, namespace, podName string) string {
	pod, err := cs.CoreV1().Pods(namespace).Get(
		context.Background(), podName, metav1.GetOptions{})
	if err != nil || len(pod.Spec.Containers) == 0 {
		return ""
	}
	return pod.Spec.Containers[0].Name
}
