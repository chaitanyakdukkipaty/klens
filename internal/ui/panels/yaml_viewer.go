package panels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// YAMLFetchedMsg carries the YAML string for a fetched resource.
type YAMLFetchedMsg struct {
	Kind      string
	Name      string
	Namespace string
	YAML      string
	Err       error
}

// YAMLViewer displays syntax-highlighted YAML in a scrollable viewport.
type YAMLViewer struct {
	viewport  viewport.Model
	width     int
	height    int
	focused   bool
	kind      string
	name      string
	namespace string
	raw       string
}

func NewYAMLViewer(w, h int) YAMLViewer {
	vp := viewport.New(w-2, h-4)
	vp.Style = lipgloss.NewStyle()
	return YAMLViewer{viewport: vp, width: w, height: h}
}

func (v YAMLViewer) SetSize(w, h int) YAMLViewer {
	v.width = w
	v.height = h
	v.viewport.Width = w - 2
	v.viewport.Height = h - 4
	return v
}
func (v YAMLViewer) SetFocused(f bool) YAMLViewer { v.focused = f; return v }
func (v YAMLViewer) RawYAML() string               { return v.raw }
func (v YAMLViewer) ResourceInfo() (kind, name, ns string) {
	return v.kind, v.name, v.namespace
}

func (v YAMLViewer) Update(msg tea.Msg) (YAMLViewer, tea.Cmd) {
	switch msg := msg.(type) {
	case YAMLFetchedMsg:
		v.kind = msg.Kind
		v.name = msg.Name
		v.namespace = msg.Namespace
		v.raw = msg.YAML
		highlighted := highlightYAML(msg.YAML)
		v.viewport.SetContent(highlighted)
		v.viewport.GotoTop()
	case tea.KeyMsg:
		var cmd tea.Cmd
		v.viewport, cmd = v.viewport.Update(msg)
		return v, cmd
	}
	return v, nil
}

func (v YAMLViewer) View() string {
	border := appstyles.NormalBorder
	if v.focused {
		border = appstyles.FocusedBorder
	}
	title := appstyles.Title.Render(fmt.Sprintf("YAML: %s/%s", v.kind, v.name))
	help := appstyles.Muted.Render("  ↑↓/jk scroll  e edit  esc back")
	return border.Width(v.width - 2).Height(v.height - 2).Render(
		title + "\n" + help + "\n\n" + v.viewport.View(),
	)
}

// FetchYAMLCmd returns a tea.Cmd that fetches the raw YAML for a resource.
func FetchYAMLCmd(cs *kubernetes.Clientset, kind, name, namespace string) tea.Cmd {
	return func() tea.Msg {
		obj, err := fetchObject(cs, kind, name, namespace)
		if err != nil {
			return YAMLFetchedMsg{Kind: kind, Name: name, Namespace: namespace, Err: err}
		}
		// Remove managed fields noise
		if m, ok := obj.(interface{ GetManagedFields() interface{} }); ok {
			_ = m
		}
		b, err := json.Marshal(obj)
		if err != nil {
			return YAMLFetchedMsg{Err: err}
		}
		y, err := yaml.JSONToYAML(b)
		if err != nil {
			return YAMLFetchedMsg{Err: err}
		}
		return YAMLFetchedMsg{
			Kind:      kind,
			Name:      name,
			Namespace: namespace,
			YAML:      string(y),
		}
	}
}

// FetchHelmReleaseYAMLCmd builds YAML from the already-cached unstructured object — no network call needed.
func FetchHelmReleaseYAMLCmd(u *unstructured.Unstructured) tea.Cmd {
	return func() tea.Msg {
		data, err := yaml.Marshal(u.Object)
		if err != nil {
			return YAMLFetchedMsg{Err: err}
		}
		return YAMLFetchedMsg{
			Kind:      u.GetKind(),
			Name:      u.GetName(),
			Namespace: u.GetNamespace(),
			YAML:      string(data),
		}
	}
}

func fetchObject(cs *kubernetes.Clientset, kind, name, namespace string) (interface{}, error) {
	ctx := context.Background()
	opts := metav1.GetOptions{}
	switch kind {
	case "Pod":
		return cs.CoreV1().Pods(namespace).Get(ctx, name, opts)
	case "Deployment":
		return cs.AppsV1().Deployments(namespace).Get(ctx, name, opts)
	case "StatefulSet":
		return cs.AppsV1().StatefulSets(namespace).Get(ctx, name, opts)
	case "DaemonSet":
		return cs.AppsV1().DaemonSets(namespace).Get(ctx, name, opts)
	case "ReplicaSet":
		return cs.AppsV1().ReplicaSets(namespace).Get(ctx, name, opts)
	case "Service":
		return cs.CoreV1().Services(namespace).Get(ctx, name, opts)
	case "Ingress":
		return cs.NetworkingV1().Ingresses(namespace).Get(ctx, name, opts)
	case "ConfigMap":
		return cs.CoreV1().ConfigMaps(namespace).Get(ctx, name, opts)
	case "Secret":
		return cs.CoreV1().Secrets(namespace).Get(ctx, name, opts)
	case "Node":
		return cs.CoreV1().Nodes().Get(ctx, name, opts)
	case "PersistentVolume":
		return cs.CoreV1().PersistentVolumes().Get(ctx, name, opts)
	case "PersistentVolumeClaim":
		return cs.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, opts)
	case "Job":
		return cs.BatchV1().Jobs(namespace).Get(ctx, name, opts)
	case "CronJob":
		return cs.BatchV1().CronJobs(namespace).Get(ctx, name, opts)
	case "ServiceAccount":
		return cs.CoreV1().ServiceAccounts(namespace).Get(ctx, name, opts)
	case "Namespace":
		return cs.CoreV1().Namespaces().Get(ctx, name, opts)
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

func highlightYAML(src string) string {
	lexer := lexers.Get("yaml")
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get("dracula")
	if style == nil {
		style = styles.Fallback
	}

	formatter := formatters.Get("terminal256")
	if formatter == nil {
		formatter = formatters.Fallback
	}

	iterator, err := lexer.Tokenise(nil, src)
	if err != nil {
		return src
	}

	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, iterator); err != nil {
		return src
	}
	return buf.String()
}
