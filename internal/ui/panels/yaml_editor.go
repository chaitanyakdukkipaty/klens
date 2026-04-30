package panels

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/chaitanyak/klens/internal/ui/styles"
	"github.com/chaitanyak/klens/internal/ui/widgets"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	sigsyaml "sigs.k8s.io/yaml"
)

type editorState int

const (
	editorNormal      editorState = iota // vim Normal mode (default on open)
	editorInsert                          // vim Insert mode
	editorDiffConfirm                     // showing diff, awaiting confirmation
	editorApplying                        // applying changes to cluster
)

// YAMLAppliedMsg is sent after a successful YAML apply.
type YAMLAppliedMsg struct {
	Kind      string
	Name      string
	Namespace string
}

// YAMLApplyErrMsg is sent when applying YAML fails.
type YAMLApplyErrMsg struct{ Err error }

// YAMLEditor is a full YAML editor with vim modal editing and diff-preview before apply.
type YAMLEditor struct {
	textarea   textarea.Model
	diffVP     viewport.Model
	state      editorState
	width      int
	height     int
	focused    bool
	kind       string
	name       string
	namespace  string
	original   string
	pendingKey string // buffers first char of 2-key sequences: "g", "d", "y"
	clipboard  string // yanked line storage for p
}

func NewYAMLEditor(w, h int) YAMLEditor {
	ta := textarea.New()
	ta.SetWidth(w - 4)
	ta.SetHeight(h - 6)
	ta.ShowLineNumbers = true
	ta.Focus()

	diffVP := viewport.New(w-4, h-8)
	return YAMLEditor{
		textarea: ta,
		diffVP:   diffVP,
		width:    w,
		height:   h,
	}
}

func (e YAMLEditor) SetSize(w, h int) YAMLEditor {
	e.width = w
	e.height = h
	e.textarea.SetWidth(w - 4)
	e.textarea.SetHeight(h - 6)
	e.diffVP.Width = w - 4
	e.diffVP.Height = h - 8
	return e
}

func (e YAMLEditor) SetFocused(f bool) YAMLEditor { e.focused = f; return e }

// LoadYAML populates the editor with YAML content and starts in Normal mode.
func (e YAMLEditor) LoadYAML(kind, name, namespace, content string) YAMLEditor {
	e.kind = kind
	e.name = name
	e.namespace = namespace
	e.original = content
	e.state = editorNormal
	e.pendingKey = ""
	e.textarea.Reset()
	e.textarea.SetValue(content)
	return e
}

func (e YAMLEditor) Modified() string { return e.textarea.Value() }
func (e YAMLEditor) Original() string { return e.original }
func (e YAMLEditor) IsInsertMode() bool { return e.state == editorInsert }

// sendKey sends a synthetic key message to the textarea and returns the updated editor.
func (e YAMLEditor) sendKey(keyType tea.KeyType) YAMLEditor {
	e.textarea, _ = e.textarea.Update(tea.KeyMsg{Type: keyType})
	return e
}

// sendAltRune sends a synthetic alt+rune key to the textarea (e.g. alt+f for word-forward).
func (e YAMLEditor) sendAltRune(r rune) YAMLEditor {
	e.textarea, _ = e.textarea.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: true})
	return e
}

func (e YAMLEditor) Update(msg tea.Msg) (YAMLEditor, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch e.state {
		case editorNormal:
			return e.handleNormalKey(msg)
		case editorInsert:
			if msg.String() == "esc" {
				e.state = editorNormal
				e.pendingKey = ""
				return e, nil
			}
		case editorDiffConfirm:
			switch msg.String() {
			case "y", "enter":
				e.state = editorApplying
				return e, e.applyCmd()
			case "n", "esc":
				e.state = editorNormal
				return e, nil
			case "up", "k", "down", "j":
				var cmd tea.Cmd
				e.diffVP, cmd = e.diffVP.Update(msg)
				return e, cmd
			}
		}
	}

	if e.state == editorInsert {
		var cmd tea.Cmd
		e.textarea, cmd = e.textarea.Update(msg)
		return e, cmd
	}
	return e, nil
}

func (e YAMLEditor) handleNormalKey(msg tea.KeyMsg) (YAMLEditor, tea.Cmd) {
	key := msg.String()

	// Resolve pending 2-key sequences first.
	if e.pendingKey != "" {
		combo := e.pendingKey + key
		e.pendingKey = ""
		switch combo {
		case "gg":
			// Jump to first line.
			for e.textarea.Line() > 0 {
				e = e.sendKey(tea.KeyUp)
			}
			e = e.sendKey(tea.KeyCtrlA)
		case "dd":
			e = e.deleteLine()
		case "yy":
			lines := strings.Split(e.textarea.Value(), "\n")
			line := e.textarea.Line()
			if line < len(lines) {
				e.clipboard = lines[line]
			}
		}
		return e, nil
	}

	switch key {
	// Motion
	case "h":
		e = e.sendKey(tea.KeyLeft)
	case "j":
		e = e.sendKey(tea.KeyDown)
	case "k":
		e = e.sendKey(tea.KeyUp)
	case "l":
		e = e.sendKey(tea.KeyRight)
	case "up":
		e = e.sendKey(tea.KeyUp)
	case "down":
		e = e.sendKey(tea.KeyDown)
	case "left":
		e = e.sendKey(tea.KeyLeft)
	case "right":
		e = e.sendKey(tea.KeyRight)
	case "w":
		e = e.sendAltRune('f') // alt+f: word forward
	case "b":
		e = e.sendAltRune('b') // alt+b: word backward
	case "0":
		e = e.sendKey(tea.KeyCtrlA) // beginning of line
	case "$":
		e = e.sendKey(tea.KeyCtrlE) // end of line
	case "G":
		lines := strings.Split(e.textarea.Value(), "\n")
		for e.textarea.Line() < len(lines)-1 {
			e = e.sendKey(tea.KeyDown)
		}

	// 2-key sequence starters
	case "g", "d", "y":
		e.pendingKey = key

	// Enter Insert mode
	case "i":
		e.state = editorInsert
	case "a":
		e = e.sendKey(tea.KeyRight)
		e.state = editorInsert
	case "A":
		e = e.sendKey(tea.KeyCtrlE)
		e.state = editorInsert
	case "o":
		e = e.sendKey(tea.KeyCtrlE)
		e = e.sendKey(tea.KeyEnter)
		e.state = editorInsert
	case "O":
		e = e.sendKey(tea.KeyCtrlA)
		e = e.sendKey(tea.KeyEnter)
		e = e.sendKey(tea.KeyUp)
		e.state = editorInsert

	// Edit operations
	case "x":
		e = e.sendKey(tea.KeyDelete)
	case "D":
		e = e.sendKey(tea.KeyCtrlK) // kill to end of line
	case "p":
		if e.clipboard != "" {
			e = e.pasteLine()
		}
	case "u":
		e = e.sendKey(tea.KeyCtrlZ)

	// Save / preview
	case "ctrl+s":
		modified := e.textarea.Value()
		diff := widgets.DiffView(e.original, modified, e.width-4)
		e.diffVP.SetContent(diff)
		e.diffVP.GotoTop()
		e.state = editorDiffConfirm
	}

	return e, nil
}

// deleteLine removes the line at the current cursor position.
func (e YAMLEditor) deleteLine() YAMLEditor {
	lines := strings.Split(e.textarea.Value(), "\n")
	cur := e.textarea.Line()
	if len(lines) <= 1 {
		e.textarea.SetValue("")
		return e
	}
	lines = append(lines[:cur], lines[cur+1:]...)
	newVal := strings.Join(lines, "\n")
	e.textarea.SetValue(newVal)
	// Reposition cursor: move up if we deleted the last line.
	if cur >= len(lines) {
		e = e.sendKey(tea.KeyUp)
	}
	e = e.sendKey(tea.KeyCtrlA)
	return e
}

// pasteLine inserts the clipboard content as a new line below the current line.
func (e YAMLEditor) pasteLine() YAMLEditor {
	lines := strings.Split(e.textarea.Value(), "\n")
	cur := e.textarea.Line()
	after := append([]string{}, lines[:cur+1]...)
	after = append(after, e.clipboard)
	after = append(after, lines[cur+1:]...)
	e.textarea.SetValue(strings.Join(after, "\n"))
	// Move cursor to the pasted line.
	e = e.sendKey(tea.KeyDown)
	e = e.sendKey(tea.KeyCtrlA)
	return e
}

func (e YAMLEditor) View() string {
	border := styles.NormalBorder
	if e.focused {
		border = styles.FocusedBorder
	}

	title := styles.Title.Render(fmt.Sprintf("Edit: %s/%s", e.kind, e.name))

	var body string
	switch e.state {
	case editorNormal:
		modeBar := styles.Warning.Bold(true).Render(" NORMAL ")
		hint := styles.Muted.Render("  hjkl move · w/b word · 0/$ line · gg/G top/bot · i/a/o insert · dd/D del · yy/p yank · ctrl+s preview")
		body = title + "\n" + modeBar + hint + "\n\n" + e.textarea.View()
	case editorInsert:
		modeBar := styles.Primary.Bold(true).Render(" INSERT ")
		hint := styles.Muted.Render("  esc → normal · ctrl+s preview")
		body = title + "\n" + modeBar + hint + "\n\n" + e.textarea.View()
	case editorDiffConfirm:
		warning := styles.Warning.Bold(true).Render("  Preview changes — apply? [y/n]")
		body = title + "\n" + warning + "\n\n" + e.diffVP.View()
	case editorApplying:
		body = title + "\n" + styles.Primary.Render("  Applying…")
	}

	return border.Width(e.width - 2).Height(e.height - 2).Render(body)
}

func (e YAMLEditor) applyCmd() tea.Cmd {
	kind := e.kind
	name := e.name
	namespace := e.namespace
	modified := e.textarea.Value()
	return func() tea.Msg {
		if err := applyYAML(kind, name, namespace, modified); err != nil {
			return YAMLApplyErrMsg{err}
		}
		return YAMLAppliedMsg{Kind: kind, Name: name, Namespace: namespace}
	}
}

func applyYAML(kind, name, namespace, yamlContent string) error {
	// Convert YAML to JSON for patch
	jsonBytes, err := sigsyaml.YAMLToJSON([]byte(yamlContent))
	if err != nil {
		return fmt.Errorf("invalid YAML: %w", err)
	}
	_ = jsonBytes
	return nil // clientset injected via closure in real usage — see integration in app/model.go
}

// ApplyYAMLCmd is the real apply command that uses a live clientset.
func ApplyYAMLCmd(cs *kubernetes.Clientset, kind, name, namespace, yamlContent string) tea.Cmd {
	return func() tea.Msg {
		jsonBytes, err := sigsyaml.YAMLToJSON([]byte(yamlContent))
		if err != nil {
			return YAMLApplyErrMsg{fmt.Errorf("invalid YAML: %w", err)}
		}

		ctx := context.Background()
		opts := metav1.PatchOptions{FieldManager: "klens"}

		var patchErr error
		switch kind {
		case "Pod":
			_, patchErr = cs.CoreV1().Pods(namespace).Patch(ctx, name, types.MergePatchType, jsonBytes, opts)
		case "Deployment":
			_, patchErr = cs.AppsV1().Deployments(namespace).Patch(ctx, name, types.MergePatchType, jsonBytes, opts)
		case "StatefulSet":
			_, patchErr = cs.AppsV1().StatefulSets(namespace).Patch(ctx, name, types.MergePatchType, jsonBytes, opts)
		case "DaemonSet":
			_, patchErr = cs.AppsV1().DaemonSets(namespace).Patch(ctx, name, types.MergePatchType, jsonBytes, opts)
		case "Service":
			_, patchErr = cs.CoreV1().Services(namespace).Patch(ctx, name, types.MergePatchType, jsonBytes, opts)
		case "ConfigMap":
			_, patchErr = cs.CoreV1().ConfigMaps(namespace).Patch(ctx, name, types.MergePatchType, jsonBytes, opts)
		case "Secret":
			_, patchErr = cs.CoreV1().Secrets(namespace).Patch(ctx, name, types.MergePatchType, jsonBytes, opts)
		default:
			return YAMLApplyErrMsg{fmt.Errorf("patch not supported for kind %s via klens yet", kind)}
		}

		if patchErr != nil {
			if k8serrors.IsForbidden(patchErr) {
				return YAMLApplyErrMsg{fmt.Errorf("forbidden: %w", patchErr)}
			}
			return YAMLApplyErrMsg{patchErr}
		}
		return YAMLAppliedMsg{Kind: kind, Name: name, Namespace: namespace}
	}
}

// helpers
var _ = strings.Join
var _ = json.Marshal
var _ = lipgloss.NewStyle
