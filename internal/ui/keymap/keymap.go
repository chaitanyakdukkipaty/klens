// Package keymap is the labels-only keybinding catalog: the single source
// the keybindings overlay (and any future help surface) renders from.
// Dispatch stays in the existing handlers — if a label here drifts from
// behavior it's a cosmetic bug, not a broken key (see design doc, decision
// 11).
package keymap

// Binding is one key → action label pair.
type Binding struct {
	Key  string
	Desc string
}

// Section groups bindings by the surface they apply to.
type Section struct {
	Title    string
	Bindings []Binding
}

// Sections returns the full catalog in display order.
func Sections() []Section {
	return []Section{
		{Title: "Global", Bindings: []Binding{
			{"tab", "cycle focus nav ↔ content"},
			{"enter", "focus table / open context menu"},
			{"↑↓ / jk", "navigate"},
			{"/", "filter"},
			{"esc", "back / peel state"},
			{"ctrl+n", "namespace picker"},
			{"ctrl+o", "cluster context picker"},
			{"ctrl+f", "active port-forwards"},
			{"ctrl+r", "reconnect / refresh"},
			{"?", "this help"},
			{"q / ctrl+c", "quit"},
		}},
		{Title: "Views (tab bar)", Bindings: []Binding{
			{"y", "YAML (e inside to edit)"},
			{"d", "describe"},
			{"l", "logs (multi-pod with selection)"},
			{"x", "x-ray tree"},
			{"m", "metrics"},
			{"F", "fullscreen (detail views)"},
			{"esc", "back to table"},
		}},
		{Title: "Table", Bindings: []Binding{
			{"space", "mark row (Pods; composes with logs)"},
			{"s", "scale / suspend (HelmRelease)"},
			{"r", "resume (HelmRelease)"},
			{"shift+f", "port-forward (Pods)"},
			{"a", "attach / exec into pod"},
			{"ctrl+d", "delete (graceful)"},
			{"ctrl+k", "kill (force, grace=0)"},
			{"z", "sanitize completed/errored pods"},
			{"shift+→/←", "cycle sort column"},
			{"shift+↑/↓", "sort asc / desc"},
			{"←/→", "scroll wide column"},
		}},
		{Title: "Events table", Bindings: []Binding{
			{"ctrl+z", "faults-only filter"},
			{"w", "wrap MESSAGE column"},
			{"o", "open involved object"},
		}},
		{Title: "Sidebar", Bindings: []Binding{
			{"h / ←", "fold group"},
			{"l / →", "unfold group"},
			{"enter / space", "toggle group (on header)"},
			{"g / G", "top / bottom"},
		}},
		{Title: "Describe viewer", Bindings: []Binding{
			{"/", "regex filter"},
			{"n / N", "next / prev match"},
			{"c", "copy all"},
			{"ctrl+s", "save to file"},
		}},
		{Title: "Log viewer", Bindings: []Binding{
			{"/", "filter lines"},
			{"ctrl+f", "inline search"},
			{"n / N", "next / prev match"},
			{"s", "pause / resume stream"},
			{"a", "toggle autoscroll"},
			{"w", "toggle wrap"},
			{"p", "previous-container logs"},
			{"J", "JSON pretty-print"},
			{"1–9 / 0", "solo pod / all pods"},
			{"v", "cycle tabs / split layout"},
			{"c", "copy visible lines"},
			{"ctrl+s", "save to file"},
		}},
		{Title: "YAML editor", Bindings: []Binding{
			{"i a A o O", "insert modes"},
			{"hjkl", "move (normal mode)"},
			{"ctrl+s", "diff preview / apply"},
			{"ctrl+z", "rollback last apply (in YAML view)"},
		}},
		{Title: "Mouse", Bindings: []Binding{
			{"click", "select / activate everywhere"},
			{"right-click", "row context menu"},
			{"drag", "select text to copy"},
			{"drag thumb", "scroll (table scrollbar)"},
			{"wheel", "scroll / cycle"},
			{"header click", "sort by column"},
		}},
	}
}
