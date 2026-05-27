package panels

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/atotto/clipboard"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	k8sres "github.com/chaitanyak/klens/internal/k8s"
	"github.com/chaitanyak/klens/internal/ui/styles"
	"github.com/chaitanyak/klens/internal/ui/widgets"
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
	ta.SetWidth(max(1, w-5))
	ta.SetHeight(max(1, h-6))
	ta.ShowLineNumbers = true
	ta.Focus()

	diffVP := viewport.New(viewport.WithWidth(max(1, w-5)), viewport.WithHeight(max(1, h-8)))
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
	e.textarea.SetWidth(max(1, w-5))
	e.textarea.SetHeight(max(1, h-6))
	e.diffVP.SetWidth(max(1, w-5))
	e.diffVP.SetHeight(max(1, h-8))
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

// sendKey sends a synthetic key press to the textarea (special keys like KeyUp, KeyEnter, etc.)
// and returns the updated editor.
func (e YAMLEditor) sendKey(code rune) YAMLEditor {
	e.textarea, _ = e.textarea.Update(tea.KeyPressMsg{Code: code})
	return e
}

// sendCtrlKey sends a synthetic ctrl+<r> key press to the textarea (e.g. ctrl+a for line start).
func (e YAMLEditor) sendCtrlKey(r rune) YAMLEditor {
	e.textarea, _ = e.textarea.Update(tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl})
	return e
}

// sendAltRune sends a synthetic alt+<r> key press to the textarea (e.g. alt+f for word-forward).
// Text is intentionally omitted — when set, KeyPressMsg.String() returns the text alone
// ("f") instead of the modified key ("alt+f"), which textarea's keymap matches against.
func (e YAMLEditor) sendAltRune(r rune) YAMLEditor {
	e.textarea, _ = e.textarea.Update(tea.KeyPressMsg{Code: r, Mod: tea.ModAlt})
	return e
}

func (e YAMLEditor) Update(msg tea.Msg) (YAMLEditor, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch e.state {
		case editorNormal:
			return e.handleNormalKey(msg)
		case editorInsert:
			if msg.String() == "esc" {
				e.state = editorNormal
				e.pendingKey = ""
				return e, nil
			}
			if msg.String() == "ctrl+v" {
				if text, err := clipboard.ReadAll(); err == nil && text != "" {
					e.textarea, _ = e.textarea.Update(tea.PasteMsg{Content: text})
				}
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
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if e.state == editorDiffConfirm {
				var cmd tea.Cmd
				e.diffVP, cmd = e.diffVP.Update(msg)
				return e, cmd
			}
			e = e.sendKey(tea.KeyUp)
		case tea.MouseWheelDown:
			if e.state == editorDiffConfirm {
				var cmd tea.Cmd
				e.diffVP, cmd = e.diffVP.Update(msg)
				return e, cmd
			}
			e = e.sendKey(tea.KeyDown)
		case tea.MouseWheelLeft:
			e = e.sendKey(tea.KeyLeft)
		case tea.MouseWheelRight:
			e = e.sendKey(tea.KeyRight)
		}
		return e, nil
	}

	if e.state == editorInsert {
		var cmd tea.Cmd
		e.textarea, cmd = e.textarea.Update(msg)
		return e, cmd
	}
	return e, nil
}

func (e YAMLEditor) handleNormalKey(msg tea.KeyPressMsg) (YAMLEditor, tea.Cmd) {
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
			e = e.sendCtrlKey('a')
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
		e = e.sendCtrlKey('a') // beginning of line
	case "$":
		e = e.sendCtrlKey('e') // end of line
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
		e = e.sendCtrlKey('e')
		e.state = editorInsert
	case "o":
		e = e.sendCtrlKey('e')
		e = e.sendKey(tea.KeyEnter)
		e.state = editorInsert
	case "O":
		e = e.sendCtrlKey('a')
		e = e.sendKey(tea.KeyEnter)
		e = e.sendKey(tea.KeyUp)
		e.state = editorInsert

	// Edit operations
	case "x":
		e = e.sendKey(tea.KeyDelete)
	case "D":
		e = e.sendCtrlKey('k') // kill to end of line
	case "p":
		if e.clipboard != "" {
			e = e.pasteLine()
		}
	case "u":
		e = e.sendCtrlKey('z')

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
	e = e.sendCtrlKey('a')
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
	e = e.sendCtrlKey('a')
	return e
}

func (e YAMLEditor) View() string {
	border := styles.NormalBorder
	if e.focused {
		border = styles.FocusedBorder
	}

	title := styles.Title.Render(fmt.Sprintf("Edit: %s/%s", e.kind, e.name))

	taScrollbar := func() string {
		taHeight := max(1, e.height-6)
		totalLines := strings.Count(e.textarea.Value(), "\n") + 1
		viewTop := max(0, min(e.textarea.Line()-taHeight/2, totalLines-taHeight))
		return renderScrollbar(taHeight, taHeight, totalLines, viewTop, e.focused)
	}

	var body string
	switch e.state {
	case editorNormal:
		modeBar := styles.Muted.Bold(true).Render(" NORMAL ")
		hint := "  " + RenderHelpInline([]HelpItem{
			{Key: "hjkl", Desc: "move"},
			{Key: "w/b", Desc: "word"},
			{Key: "0/$", Desc: "line"},
			{Key: "gg/G", Desc: "top/bot"},
			{Key: "i/a/o", Desc: "insert"},
			{Key: "dd/D", Desc: "del"},
			{Key: "yy/p", Desc: "yank"},
			{Key: "F", Desc: "fullscreen"},
			{Key: "ctrl+s", Desc: "preview"},
		})
		body = title + "\n" + modeBar + hint + "\n\n" + joinScrollbar(e.textarea.View(), taScrollbar())
	case editorInsert:
		modeBar := styles.Primary.Bold(true).Render(" INSERT ")
		hint := "  " + RenderHelpInline([]HelpItem{
			{Key: "esc", Desc: "→ normal"},
			{Key: "ctrl+s", Desc: "preview"},
		})
		body = title + "\n" + modeBar + hint + "\n\n" + joinScrollbar(e.textarea.View(), taScrollbar())
	case editorDiffConfirm:
		warning := styles.Warning.Bold(true).Render("  Preview changes — apply? [y/n]")
		sbStr := renderScrollbar(
			e.diffVP.Height(),
			e.diffVP.VisibleLineCount(),
			e.diffVP.TotalLineCount(),
			e.diffVP.YOffset(),
			e.focused,
		)
		body = title + "\n" + warning + "\n\n" + joinScrollbar(e.diffVP.View(), sbStr)
	case editorApplying:
		body = title + "\n" + styles.Primary.Render("  Applying…")
	}

	return border.Width(max(1, e.width)).Height(max(1, e.height)).Render(body)
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

// ApplyYAMLCmd is the real apply command that uses a live clientset. It looks
// up the per-kind "apply" Action on the descriptor — the kind-keyed switch
// that used to live here is now closures registered in panels/kinds.go.
func ApplyYAMLCmd(cs kubernetes.Interface, kind, name, namespace, yamlContent string) tea.Cmd {
	action := k8sres.LookupAction(kind, "apply")
	if action == nil {
		return func() tea.Msg {
			return YAMLApplyErrMsg{fmt.Errorf("patch not supported for kind %s via klens yet", kind)}
		}
	}
	return action(k8sres.ActionDeps{
		Clientset:   cs,
		Name:        name,
		Namespace:   namespace,
		YAMLContent: yamlContent,
	})
}

// helpers
var _ = strings.Join
var _ = json.Marshal
var _ = lipgloss.NewStyle
