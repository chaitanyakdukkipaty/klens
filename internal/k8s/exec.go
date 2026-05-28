package k8s

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
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
	cs        kubernetes.Interface
	cfg       *rest.Config
	namespace string
	pod       string
	container string
	stdin     io.Reader
	stdout    io.Writer
	stderr    io.Writer
}

func (c *PodExecCommand) SetStdin(r io.Reader)  { c.stdin = r }
func (c *PodExecCommand) SetStdout(w io.Writer) { c.stdout = w }
func (c *PodExecCommand) SetStderr(w io.Writer) { c.stderr = w }

func (c *PodExecCommand) Run() error {
	container := c.container
	if container == "" {
		container = detectContainer(c.cs, c.namespace, c.pod)
	}
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
// container may be empty, in which case detectContainer picks the first one.
func AttachCmd(cs kubernetes.Interface, cfg *rest.Config, namespace, pod, container string) tea.Cmd {
	c := &PodExecCommand{cs: cs, cfg: cfg, namespace: namespace, pod: pod, container: container}
	return tea.Exec(c, func(err error) tea.Msg {
		return AttachFinishedMsg{Pod: pod, Err: err}
	})
}

// TmuxAttachWindowCmd opens a new tmux window running kubectl exec into the pod.
// It captures the new window's index so the TUI can switch to it later.
// kubeContext pins the exec to the cluster klens is currently viewing — without
// it, kubectl would use the on-disk current-context, which drifts after ctrl+o.
func TmuxAttachWindowCmd(kubeContext, namespace, pod, container string) tea.Cmd {
	return func() tea.Msg {
		ctxFlag := ""
		if kubeContext != "" {
			ctxFlag = fmt.Sprintf(" --context=%s", kubeContext)
		}
		var kubectlCmd string
		if container != "" {
			kubectlCmd = fmt.Sprintf(
				"kubectl%s exec -i -t -n %s %s -c %s -- sh -c 'clear; (bash || ash || sh)'",
				ctxFlag, namespace, pod, container)
		} else {
			kubectlCmd = fmt.Sprintf(
				"kubectl%s exec -i -t -n %s %s -- sh -c 'clear; (bash || ash || sh)'",
				ctxFlag, namespace, pod)
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
func detectContainer(cs kubernetes.Interface, namespace, podName string) string {
	pod, err := cs.CoreV1().Pods(namespace).Get(
		context.Background(), podName, metav1.GetOptions{})
	if err != nil || len(pod.Spec.Containers) == 0 {
		return ""
	}
	return pod.Spec.Containers[0].Name
}
