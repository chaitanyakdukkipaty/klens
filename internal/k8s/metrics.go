package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"k8s.io/client-go/kubernetes"
)

const MetricsInterval = 15 * time.Second
const metricsSamples = 20

// ResourceMetrics holds CPU and memory time-series for one resource.
type ResourceMetrics struct {
	Name      string
	Namespace string
	CPUSamples []float64 // millicores
	MEMSamples []float64 // bytes
	CPULatest  float64
	MEMLatest  float64
}

// MetricsUpdatedMsg carries fresh metrics for all pods/nodes.
type MetricsUpdatedMsg struct {
	Pods  map[string]*ResourceMetrics // keyed by "namespace/name"
	Nodes map[string]*ResourceMetrics // keyed by node name
}

// MetricsTick triggers a periodic metrics poll.
type MetricsTick struct{}

// MetricsTickCmd returns a tea.Cmd that fires after MetricsInterval.
func MetricsTickCmd() tea.Cmd {
	return tea.Tick(MetricsInterval, func(t time.Time) tea.Msg {
		return MetricsTick{}
	})
}

type podMetricsResponse struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Containers []struct {
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

type nodeMetricsResponse struct {
	Items []struct {
		Metadata struct {
			Name string `json:"name"`
		} `json:"metadata"`
		Usage struct {
			CPU    string `json:"cpu"`
			Memory string `json:"memory"`
		} `json:"usage"`
	} `json:"items"`
}

// FetchMetricsCmd queries metrics-server and returns a MetricsUpdatedMsg.
func FetchMetricsCmd(cs *kubernetes.Clientset, namespace string, prev MetricsUpdatedMsg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pods := make(map[string]*ResourceMetrics)
		nodes := make(map[string]*ResourceMetrics)

		// Fetch pod metrics
		path := fmt.Sprintf("/apis/metrics.k8s.io/v1beta1/namespaces/%s/pods", namespace)
		if namespace == "" || namespace == "all" {
			path = "/apis/metrics.k8s.io/v1beta1/pods"
		}
		raw, err := cs.RESTClient().Get().AbsPath(path).DoRaw(ctx)
		if err == nil {
			var resp podMetricsResponse
			if json.Unmarshal(raw, &resp) == nil {
				for _, item := range resp.Items {
					key := item.Metadata.Namespace + "/" + item.Metadata.Name
					cpu := float64(0)
					mem := float64(0)
					for _, c := range item.Containers {
						cpu += parseCPUMillicores(c.Usage.CPU)
						mem += parseMemoryBytes(c.Usage.Memory)
					}
					rm := &ResourceMetrics{
						Name:      item.Metadata.Name,
						Namespace: item.Metadata.Namespace,
						CPULatest: cpu,
						MEMLatest: mem,
					}
					// Carry forward previous samples
					if prev.Pods != nil {
						if old, ok := prev.Pods[key]; ok {
							rm.CPUSamples = appendSample(old.CPUSamples, cpu)
							rm.MEMSamples = appendSample(old.MEMSamples, mem)
						}
					}
					if len(rm.CPUSamples) == 0 {
						rm.CPUSamples = []float64{cpu}
						rm.MEMSamples = []float64{mem}
					}
					pods[key] = rm
				}
			}
		}

		// Fetch node metrics
		rawN, errN := cs.RESTClient().Get().AbsPath("/apis/metrics.k8s.io/v1beta1/nodes").DoRaw(ctx)
		if errN == nil {
			var resp nodeMetricsResponse
			if json.Unmarshal(rawN, &resp) == nil {
				for _, item := range resp.Items {
					cpu := parseCPUMillicores(item.Usage.CPU)
					mem := parseMemoryBytes(item.Usage.Memory)
					rm := &ResourceMetrics{
						Name:      item.Metadata.Name,
						CPULatest: cpu,
						MEMLatest: mem,
					}
					if prev.Nodes != nil {
						if old, ok := prev.Nodes[item.Metadata.Name]; ok {
							rm.CPUSamples = appendSample(old.CPUSamples, cpu)
							rm.MEMSamples = appendSample(old.MEMSamples, mem)
						}
					}
					if len(rm.CPUSamples) == 0 {
						rm.CPUSamples = []float64{cpu}
						rm.MEMSamples = []float64{mem}
					}
					nodes[item.Metadata.Name] = rm
				}
			}
		}

		return MetricsUpdatedMsg{Pods: pods, Nodes: nodes}
	}
}

func appendSample(samples []float64, val float64) []float64 {
	samples = append(samples, val)
	if len(samples) > metricsSamples {
		samples = samples[len(samples)-metricsSamples:]
	}
	return samples
}

// parseCPUMillicores parses a CPU quantity string like "250m" or "1" into millicores.
func parseCPUMillicores(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	if s[len(s)-1] == 'm' {
		var v float64
		fmt.Sscanf(s[:len(s)-1], "%f", &v)
		return v
	}
	// nanocores
	if s[len(s)-1] == 'n' {
		var v float64
		fmt.Sscanf(s[:len(s)-1], "%f", &v)
		return v / 1e6
	}
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v * 1000
}

// parseMemoryBytes parses a memory quantity string into bytes.
func parseMemoryBytes(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	suffixes := map[string]float64{
		"Ki": 1024, "Mi": 1024 * 1024, "Gi": 1024 * 1024 * 1024,
		"K": 1000, "M": 1000 * 1000, "G": 1000 * 1000 * 1000,
	}
	for suffix, mult := range suffixes {
		if len(s) > len(suffix) && s[len(s)-len(suffix):] == suffix {
			var v float64
			fmt.Sscanf(s[:len(s)-len(suffix)], "%f", &v)
			return v * mult
		}
	}
	var v float64
	fmt.Sscanf(s, "%f", &v)
	return v
}
