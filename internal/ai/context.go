package ai

import (
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// ClusterContext is a snapshot of the current cluster state injected into the Claude prompt.
type ClusterContext struct {
	ClusterName string   `json:"cluster"`
	Namespace   string   `json:"namespace"`
	PodNames    []string `json:"pods,omitempty"`
}

// BuildSystemPrompt creates the system prompt with cluster context embedded.
func BuildSystemPrompt(ctx ClusterContext) string {
	ctxJSON, _ := json.MarshalIndent(ctx, "", "  ")
	return fmt.Sprintf(`You are an AI assistant embedded in klens, a Kubernetes management TUI.

Current cluster context:
%s

You help users manage their Kubernetes cluster by translating natural language requests into
specific klens tool calls. When the user asks you to do something, call the appropriate
tool(s) in the correct sequence to accomplish the task.

Guidelines:
- Use list_resources first to find resources that match a pattern before operating on them
- For log streaming, always resolve pod names using list_resources first if the user gave a pattern
- Never guess resource names — use the pod/resource list above to resolve them
- Destructive operations (delete, scale to 0) will require user confirmation in the UI
- Be concise and precise — prefer specific tool calls over explanation`, string(ctxJSON))
}

// BuildContext creates a ClusterContext from current cluster state.
func BuildContext(clusterName, namespace string, pods []*corev1.Pod) ClusterContext {
	names := make([]string, 0, len(pods))
	for _, p := range pods {
		if p.Status.Phase == corev1.PodRunning {
			names = append(names, p.Name)
		}
	}
	// Truncate to avoid huge prompts
	if len(names) > 50 {
		names = names[:50]
	}
	return ClusterContext{
		ClusterName: clusterName,
		Namespace:   namespace,
		PodNames:    names,
	}
}

// CopilotStep is one actionable step returned by the AI.
type CopilotStep struct {
	ToolName       string
	Input          map[string]interface{}
	IsDestructive  bool
	Status         StepStatus
	StatusMessage  string
}

// StepStatus tracks execution state of a copilot step.
type StepStatus int

const (
	StepPending StepStatus = iota
	StepConfirming
	StepRunning
	StepDone
	StepFailed
	StepSkipped
)

func (s StepStatus) Icon() string {
	switch s {
	case StepPending:
		return "○"
	case StepConfirming:
		return "?"
	case StepRunning:
		return "↻"
	case StepDone:
		return "✓"
	case StepFailed:
		return "✗"
	case StepSkipped:
		return "⊘"
	default:
		return "○"
	}
}

// Describe returns a human-readable description of the step.
func (s CopilotStep) Describe() string {
	switch s.ToolName {
	case "list_resources":
		return fmt.Sprintf("List %s matching %q", s.inputStr("kind"), s.inputStr("name_pattern"))
	case "stream_logs":
		pods := s.inputStrSlice("pods")
		return fmt.Sprintf("Stream logs: %s", strings.Join(pods, ", "))
	case "scale_resource":
		return fmt.Sprintf("Scale %s/%s to %v replicas", s.inputStr("kind"), s.inputStr("name"), s.input("replicas"))
	case "delete_resource":
		return fmt.Sprintf("Delete %s/%s", s.inputStr("kind"), s.inputStr("name"))
	case "exec_command":
		return fmt.Sprintf("Exec into %s", s.inputStr("pod"))
	case "port_forward":
		return fmt.Sprintf("Port-forward %s %v→%v", s.inputStr("pod"), s.input("local_port"), s.input("remote_port"))
	case "describe_resource":
		return fmt.Sprintf("View YAML: %s/%s", s.inputStr("kind"), s.inputStr("name"))
	case "show_topology":
		return fmt.Sprintf("Topology: %s/%s", s.inputStr("kind"), s.inputStr("name"))
	case "show_events":
		return fmt.Sprintf("Events: %s/%s", s.inputStr("kind"), s.inputStr("name"))
	case "rollout_restart":
		return fmt.Sprintf("Rollout restart %s/%s", s.inputStr("kind"), s.inputStr("name"))
	default:
		return s.ToolName
	}
}

func (s CopilotStep) inputStr(key string) string  { return s.InputStr(key) }
func (s CopilotStep) input(key string) interface{} { return s.Input[key] }

// InputStr returns a string input field by key, or "" if absent/non-string.
func (s CopilotStep) InputStr(key string) string {
	if v, ok := s.Input[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// InputStrSlice returns a []string input field by key.
func (s CopilotStep) InputStrSlice(key string) []string {
	if v, ok := s.Input[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			out := make([]string, 0, len(arr))
			for _, a := range arr {
				out = append(out, fmt.Sprintf("%v", a))
			}
			return out
		}
	}
	return nil
}

func (s CopilotStep) inputStrSlice(key string) []string { return s.InputStrSlice(key) }
