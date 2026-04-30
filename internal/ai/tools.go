package ai

import "encoding/json"

// toolDef holds the definition for a single klens tool.
type toolDef struct {
	Name  string                 `json:"name"`
	Desc  string                 `json:"description"`
	Props map[string]interface{} `json:"input_schema"`
}

var toolDefs = []toolDef{
	{
		Name: "list_resources",
		Desc: "List Kubernetes resources matching a pattern in the current cluster",
		Props: map[string]interface{}{
			"kind":           map[string]string{"type": "string", "description": "Resource kind (Pod, Deployment, Service, etc.)"},
			"namespace":      map[string]string{"type": "string", "description": "Kubernetes namespace, or 'all' for all namespaces"},
			"name_pattern":   map[string]string{"type": "string", "description": "Substring to match against resource names"},
			"label_selector": map[string]string{"type": "string", "description": "Label selector e.g. 'app=frontend'"},
		},
	},
	{
		Name: "stream_logs",
		Desc: "Stream logs from one or more pods, optionally filtering by regex",
		Props: map[string]interface{}{
			"pods":         map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Pod names"},
			"namespace":    map[string]string{"type": "string", "description": "Kubernetes namespace"},
			"filter_regex": map[string]string{"type": "string", "description": "Optional regex filter"},
			"tail_lines":   map[string]interface{}{"type": "integer", "description": "Recent lines (default 200)"},
		},
	},
	{
		Name: "scale_resource",
		Desc: "Scale a Deployment, StatefulSet, or ReplicaSet to a specific replica count",
		Props: map[string]interface{}{
			"kind":      map[string]string{"type": "string", "description": "Resource kind"},
			"name":      map[string]string{"type": "string", "description": "Resource name"},
			"namespace": map[string]string{"type": "string", "description": "Kubernetes namespace"},
			"replicas":  map[string]interface{}{"type": "integer", "description": "Target replica count"},
		},
	},
	{
		Name: "delete_resource",
		Desc: "Delete a Kubernetes resource",
		Props: map[string]interface{}{
			"kind":      map[string]string{"type": "string", "description": "Resource kind"},
			"name":      map[string]string{"type": "string", "description": "Resource name"},
			"namespace": map[string]string{"type": "string", "description": "Kubernetes namespace"},
		},
	},
	{
		Name: "exec_command",
		Desc: "Execute an interactive shell in a pod container",
		Props: map[string]interface{}{
			"pod":       map[string]string{"type": "string", "description": "Pod name"},
			"namespace": map[string]string{"type": "string", "description": "Kubernetes namespace"},
			"command":   map[string]string{"type": "string", "description": "Command (default /bin/sh)"},
		},
	},
	{
		Name: "port_forward",
		Desc: "Set up port forwarding from a local port to a pod port",
		Props: map[string]interface{}{
			"pod":         map[string]string{"type": "string", "description": "Pod name"},
			"namespace":   map[string]string{"type": "string", "description": "Kubernetes namespace"},
			"local_port":  map[string]interface{}{"type": "integer", "description": "Local port (0 = auto)"},
			"remote_port": map[string]interface{}{"type": "integer", "description": "Remote pod port"},
		},
	},
	{
		Name: "describe_resource",
		Desc: "View the full YAML of a Kubernetes resource",
		Props: map[string]interface{}{
			"kind":      map[string]string{"type": "string", "description": "Resource kind"},
			"name":      map[string]string{"type": "string", "description": "Resource name"},
			"namespace": map[string]string{"type": "string", "description": "Kubernetes namespace"},
		},
	},
	{
		Name: "show_topology",
		Desc: "Show the resource relationship topology for a Deployment or Service",
		Props: map[string]interface{}{
			"kind":      map[string]string{"type": "string", "description": "Deployment or Service"},
			"name":      map[string]string{"type": "string", "description": "Resource name"},
			"namespace": map[string]string{"type": "string", "description": "Kubernetes namespace"},
		},
	},
	{
		Name: "show_events",
		Desc: "Show Kubernetes events for a resource or globally",
		Props: map[string]interface{}{
			"kind":      map[string]string{"type": "string", "description": "Resource kind (optional)"},
			"name":      map[string]string{"type": "string", "description": "Resource name (optional)"},
			"namespace": map[string]string{"type": "string", "description": "Kubernetes namespace"},
		},
	},
	{
		Name: "rollout_restart",
		Desc: "Trigger a rolling restart of a Deployment, StatefulSet, or DaemonSet",
		Props: map[string]interface{}{
			"kind":      map[string]string{"type": "string", "description": "Resource kind"},
			"name":      map[string]string{"type": "string", "description": "Resource name"},
			"namespace": map[string]string{"type": "string", "description": "Kubernetes namespace"},
		},
	},
}

// ToolSchemasJSON returns the tool definitions as a JSON string for embedding in the subprocess prompt.
func ToolSchemasJSON() string {
	b, _ := json.MarshalIndent(toolDefs, "", "  ")
	return string(b)
}

// IsDestructive returns true if the tool+args combination should require confirmation.
func IsDestructive(toolName string, input map[string]interface{}) bool {
	if toolName == "delete_resource" {
		return true
	}
	if toolName == "scale_resource" {
		if r, ok := input["replicas"]; ok {
			switch v := r.(type) {
			case float64:
				return v == 0
			case int:
				return v == 0
			}
		}
	}
	return false
}
