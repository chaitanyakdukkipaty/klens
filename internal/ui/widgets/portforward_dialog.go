package widgets

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

// PortOption is one entry the user can pick in the port-forward dialog —
// typically one ContainerPort from the pod's spec. Custom remote ports go
// through the editable RemoteInput.
type PortOption struct {
	Container string // optional — empty for service-port style entries
	PortName  string // optional — Ports[].Name from the spec
	Port      int    // remote port (declared container port)
}

// PortForwardRequest is emitted on enter. The caller resolves Pod / Service
// metadata back from its own state — the dialog only knows what the user typed.
type PortForwardRequest struct {
	Confirmed  bool
	Kind       string
	Name       string
	Namespace  string
	LocalPort  int
	RemotePort int
}

const pfDialogMax = 65535

// PortForwardDialog is a modal that collects (remote port, local port) for a
// new port-forward. When the underlying resource declares container ports they
// are presented as cyclable options; the user can still override either field
// with free-typed digits.
type PortForwardDialog struct {
	visible   bool
	kind      string
	name      string
	namespace string

	options    []PortOption
	optionIdx  int  // -1 when in custom (free-typed remote) mode
	editingTop bool // true = editing remote; false = editing local

	remoteInput string
	localInput  string
	validation  string
}

func NewPortForwardDialog() PortForwardDialog { return PortForwardDialog{} }

// Show initializes the dialog for the given resource. options may be empty,
// in which case the dialog starts in custom mode (both ports user-typed).
func (d PortForwardDialog) Show(kind, name, namespace string, options []PortOption) PortForwardDialog {
	d.visible = true
	d.kind = kind
	d.name = name
	d.namespace = namespace
	d.options = options
	d.validation = ""
	d.editingTop = false
	if len(options) > 0 {
		d.optionIdx = 0
		d.remoteInput = strconv.Itoa(options[0].Port)
		d.localInput = strconv.Itoa(options[0].Port)
	} else {
		d.optionIdx = -1
		d.remoteInput = ""
		d.localInput = ""
		d.editingTop = true
	}
	return d
}

func (d PortForwardDialog) IsVisible() bool { return d.visible }

func (d PortForwardDialog) Hide() PortForwardDialog {
	d.visible = false
	return d
}

func (d PortForwardDialog) Update(msg tea.Msg) (PortForwardDialog, tea.Cmd) {
	if !d.visible {
		return d, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return d, nil
	}
	switch key.String() {
	case "esc":
		d.visible = false
		return d, func() tea.Msg { return PortForwardRequest{Confirmed: false} }

	case "enter":
		remote, rerr := parsePort(d.remoteInput)
		if rerr != nil {
			d.validation = "remote: " + rerr.Error()
			return d, nil
		}
		local, lerr := parseLocalPort(d.localInput)
		if lerr != nil {
			d.validation = "local: " + lerr.Error()
			return d, nil
		}
		d.visible = false
		req := PortForwardRequest{
			Confirmed:  true,
			Kind:       d.kind,
			Name:       d.name,
			Namespace:  d.namespace,
			LocalPort:  local,
			RemotePort: remote,
		}
		return d, func() tea.Msg { return req }

	case "tab", "shift+tab":
		d.editingTop = !d.editingTop
		return d, nil

	case "left":
		// Cycle to previous declared port option, if any.
		if len(d.options) == 0 {
			return d, nil
		}
		if d.optionIdx <= 0 {
			d.optionIdx = len(d.options) - 1
		} else {
			d.optionIdx--
		}
		d.applyOption()
		return d, nil

	case "right":
		if len(d.options) == 0 {
			return d, nil
		}
		if d.optionIdx < 0 || d.optionIdx >= len(d.options)-1 {
			d.optionIdx = 0
		} else {
			d.optionIdx++
		}
		d.applyOption()
		return d, nil

	case "backspace":
		if d.editingTop {
			if n := len(d.remoteInput); n > 0 {
				d.remoteInput = d.remoteInput[:n-1]
				d.optionIdx = -1 // user is now editing freely
			}
		} else {
			if n := len(d.localInput); n > 0 {
				d.localInput = d.localInput[:n-1]
			}
		}
		d.validation = ""
		return d, nil
	}

	// Digit input.
	k := key.String()
	if len(k) == 1 && k >= "0" && k <= "9" {
		if d.editingTop {
			if len(d.remoteInput) < 5 {
				d.remoteInput += k
				d.optionIdx = -1
			}
		} else {
			if len(d.localInput) < 5 {
				d.localInput += k
			}
		}
		d.validation = ""
	}
	return d, nil
}

func (d *PortForwardDialog) applyOption() {
	if d.optionIdx < 0 || d.optionIdx >= len(d.options) {
		return
	}
	p := d.options[d.optionIdx]
	d.remoteInput = strconv.Itoa(p.Port)
	d.localInput = strconv.Itoa(p.Port)
}

func parsePort(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("required")
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid")
	}
	if n < 1 || n > pfDialogMax {
		return 0, fmt.Errorf("out of range")
	}
	return n, nil
}

// parseLocalPort accepts "0" / blank as "pick a free port".
func parseLocalPort(s string) (int, error) {
	if s == "" || s == "0" {
		return 0, nil
	}
	return parsePort(s)
}

func (d PortForwardDialog) View() string {
	if !d.visible {
		return ""
	}

	title := appstyles.Warning.Bold(true).Render(
		fmt.Sprintf("  Port Forward — %s/%s", d.kind, d.name))

	var optionLine string
	if len(d.options) > 0 {
		optionLine = appstyles.Muted.Render("  " + d.renderOptionStrip())
	} else {
		optionLine = appstyles.Muted.Render("  (no declared container ports — enter manually)")
	}

	remoteLabel := "  Remote port: "
	localLabel := "  Local port:  "
	if d.editingTop {
		remoteLabel = appstyles.Primary.Render(remoteLabel)
		localLabel = appstyles.Muted.Render(localLabel)
	} else {
		remoteLabel = appstyles.Muted.Render(remoteLabel)
		localLabel = appstyles.Primary.Render(localLabel)
	}

	remoteVal := d.renderField(d.remoteInput, d.editingTop)
	localVal := d.renderField(d.localPlaceholder(), !d.editingTop)

	hint := appstyles.Muted.Render(
		"  [←→] cycle ports  [tab] switch field  [enter] start  [esc] cancel")

	body := strings.Join([]string{
		title,
		optionLine,
		"",
		remoteLabel + remoteVal,
		localLabel + localVal,
		"",
	}, "\n")
	if d.validation != "" {
		body += appstyles.Error.Render("  "+d.validation) + "\n"
	}
	body += hint

	box := appstyles.DialogBox.Render(body)
	return lipgloss.Place(70, 14, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceStyle(lipgloss.NewStyle().Background(appstyles.ColorAbyss)))
}

func (d PortForwardDialog) renderField(value string, focused bool) string {
	if focused {
		return appstyles.Primary.Render(value + "█")
	}
	if value == "" {
		return appstyles.Muted.Render("—")
	}
	return appstyles.Muted.Render(value)
}

// localPlaceholder substitutes a friendly hint when the local input is empty.
func (d PortForwardDialog) localPlaceholder() string {
	if d.localInput == "" {
		return "(auto)"
	}
	return d.localInput
}

func (d PortForwardDialog) renderOptionStrip() string {
	if len(d.options) == 0 {
		return ""
	}
	parts := make([]string, 0, len(d.options))
	for i, o := range d.options {
		label := strconv.Itoa(o.Port)
		if o.PortName != "" {
			label = o.PortName + ":" + label
		}
		if o.Container != "" {
			label = o.Container + "/" + label
		}
		if i == d.optionIdx {
			label = appstyles.Primary.Bold(true).Render("[" + label + "]")
		} else {
			label = appstyles.Muted.Render(label)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "  ")
}
