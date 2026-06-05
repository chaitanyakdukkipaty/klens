package styles

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Palette is the semantic color set for a theme. Every rendered color in the
// app maps to one of these roles; the exported style/color variables below
// are rebuilt from the active palette by Apply, so panels keep using
// pre-built styles (no per-render allocations) while themes stay swappable
// at runtime.
type Palette struct {
	Name string

	Primary   color.Color // accent: focus borders, titles, keys
	Selection color.Color // selected row / nav cursor background
	Success   color.Color // success text, namespace
	Warning   color.Color // warnings, dialog borders, search highlight
	Error     color.Color // errors
	Muted     color.Color // secondary text, help descriptions
	Hover     color.Color // mouse-hover row tint (background)

	Bg       color.Color // app background tone (search highlight fg)
	Border   color.Color // unfocused panel borders
	HeaderBg color.Color // header bar background
	StatusBg color.Color // status bar / modal backdrop
	ReadOnly color.Color // [RO] indicator
	BodyText color.Color // primary body text
	BrightFg color.Color // selection foreground / brightest text

	StatusRunning    color.Color
	StatusPending    color.Color
	StatusFailed     color.Color
	StatusTerminated color.Color
	StatusUnknown    color.Color
	StatusSucceeded  color.Color

	LogPrefix []color.Color // multi-pod log prefix cycle
}

// KlensDark is the default theme: the original klens Kubernetes-blue look.
var KlensDark = Palette{
	Name:      "klens-dark",
	Primary:   lipgloss.Color("#00ADD8"),
	Selection: lipgloss.Color("#5C6BC0"),
	Success:   lipgloss.Color("#4CAF50"),
	Warning:   lipgloss.Color("#FFC107"),
	Error:     lipgloss.Color("#F44336"),
	Muted:     lipgloss.Color("#888888"),
	Hover:     lipgloss.Color("#2A2A4A"),
	Bg:        lipgloss.Color("#1A1A2E"),
	Border:    lipgloss.Color("#333355"),
	HeaderBg:  lipgloss.Color("#0D1117"),
	StatusBg:  lipgloss.Color("#0D0D1A"),
	ReadOnly:  lipgloss.Color("#FF6B6B"),
	BodyText:  lipgloss.Color("#AAAAAA"),
	BrightFg:  lipgloss.Color("#FFFFFF"),

	StatusRunning:    lipgloss.Color("#4CAF50"),
	StatusPending:    lipgloss.Color("#FFC107"),
	StatusFailed:     lipgloss.Color("#F44336"),
	StatusTerminated: lipgloss.Color("#9E9E9E"),
	StatusUnknown:    lipgloss.Color("#9E9E9E"),
	StatusSucceeded:  lipgloss.Color("#64B5F6"),

	LogPrefix: []color.Color{
		lipgloss.Color("#00ADD8"),
		lipgloss.Color("#4CAF50"),
		lipgloss.Color("#FFC107"),
		lipgloss.Color("#BA68C8"),
		lipgloss.Color("#FF5722"),
		lipgloss.Color("#03A9F4"),
		lipgloss.Color("#8BC34A"),
		lipgloss.Color("#FF9800"),
	},
}

// CatppuccinMocha maps the catppuccin-mocha palette onto klens roles.
var CatppuccinMocha = Palette{
	Name:      "catppuccin-mocha",
	Primary:   lipgloss.Color("#89B4FA"), // blue
	Selection: lipgloss.Color("#45475A"), // surface1
	Success:   lipgloss.Color("#A6E3A1"), // green
	Warning:   lipgloss.Color("#F9E2AF"), // yellow
	Error:     lipgloss.Color("#F38BA8"), // red
	Muted:     lipgloss.Color("#6C7086"), // overlay0
	Hover:     lipgloss.Color("#313244"), // surface0
	Bg:        lipgloss.Color("#1E1E2E"), // base
	Border:    lipgloss.Color("#313244"), // surface0
	HeaderBg:  lipgloss.Color("#181825"), // mantle
	StatusBg:  lipgloss.Color("#11111B"), // crust
	ReadOnly:  lipgloss.Color("#EBA0AC"), // maroon
	BodyText:  lipgloss.Color("#A6ADC8"), // subtext0
	BrightFg:  lipgloss.Color("#CDD6F4"), // text

	StatusRunning:    lipgloss.Color("#A6E3A1"),
	StatusPending:    lipgloss.Color("#F9E2AF"),
	StatusFailed:     lipgloss.Color("#F38BA8"),
	StatusTerminated: lipgloss.Color("#6C7086"),
	StatusUnknown:    lipgloss.Color("#6C7086"),
	StatusSucceeded:  lipgloss.Color("#89DCEB"), // sky

	LogPrefix: []color.Color{
		lipgloss.Color("#89B4FA"), // blue
		lipgloss.Color("#A6E3A1"), // green
		lipgloss.Color("#F9E2AF"), // yellow
		lipgloss.Color("#CBA6F7"), // mauve
		lipgloss.Color("#FAB387"), // peach
		lipgloss.Color("#94E2D5"), // teal
		lipgloss.Color("#89DCEB"), // sky
		lipgloss.Color("#F5C2E7"), // pink
	},
}

// Presets lists the selectable themes in settings/config order.
var Presets = []Palette{KlensDark, CatppuccinMocha}

// PresetByName returns the preset with the given name (case-insensitive),
// falling back to KlensDark for unknown or empty names.
func PresetByName(name string) Palette {
	for _, p := range Presets {
		if strings.EqualFold(p.Name, name) {
			return p
		}
	}
	return KlensDark
}

// Current is the active palette. Read-only outside this package; switch
// themes with Apply.
var Current Palette

// Exported colors and pre-built styles. All are (re)assigned by Apply from
// the active palette; never write to them from other packages.
var (
	ColorPrimary     color.Color
	ColorMuted       color.Color
	ColorSelection   color.Color
	ColorHover       color.Color
	ColorHeaderBg    color.Color
	ColorAbyss       color.Color // status bar / modal backdrop
	ColorReadOnly    color.Color
	ColorBodyText    color.Color
	ColorWhite       color.Color // selection foreground / brightest text
	ColorDiffContext color.Color // diff viewer context lines (unchanged lines)

	// Status colors
	ColorRunning    color.Color
	ColorPending    color.Color
	ColorFailed     color.Color
	ColorTerminated color.Color
	ColorUnknown    color.Color
	ColorSucceeded  color.Color

	// Panel borders
	NormalBorder  lipgloss.Style
	FocusedBorder lipgloss.Style

	// Header
	Header lipgloss.Style

	// Status bar
	StatusBar lipgloss.Style
	StatusKey lipgloss.Style
	StatusVal lipgloss.Style

	// Table
	TableHeader      lipgloss.Style
	TableRow         lipgloss.Style
	TableRowSelected lipgloss.Style
	TableRowHover    lipgloss.Style

	// Titles
	Title    lipgloss.Style
	Subtitle lipgloss.Style

	// Text variants
	Bold            lipgloss.Style
	Muted           lipgloss.Style
	Error           lipgloss.Style
	Warning         lipgloss.Style
	Success         lipgloss.Style
	Primary         lipgloss.Style
	SearchHighlight lipgloss.Style

	// Dialog
	DialogBox lipgloss.Style

	// Help bar (bottom)
	HelpKey     lipgloss.Style
	HelpDesc    lipgloss.Style
	HelpBracket lipgloss.Style

	// Log viewer
	LogPrefixColors []color.Color
	// LogPrefixStyles are pre-built bold styles indexed parallel to
	// LogPrefixColors. Using these avoids allocating a new lipgloss.Style on
	// every rendered log line.
	LogPrefixStyles []lipgloss.Style
)

// onApply holds rebuild hooks registered by packages that keep their own
// pre-built styles derived from this palette (nav, table, diff, tree…).
// Apply invokes them after rebuilding its own exports, so a runtime theme
// switch propagates everywhere.
var onApply []func()

// RegisterOnApply registers a derived-style rebuild hook and invokes it once
// immediately (the active palette is already applied by the time package
// init() functions run).
func RegisterOnApply(f func()) {
	onApply = append(onApply, f)
	f()
}

// statusStyleMap holds pre-built styles keyed by Kubernetes status string.
var statusStyleMap map[string]lipgloss.Style

var statusStyleDefault lipgloss.Style

// Apply activates a palette: rebuilds every exported color and pre-built
// style from it. Safe to call at any point on the Bubbletea loop goroutine
// (styles are only read during View on the same goroutine).
func Apply(p Palette) {
	Current = p

	ColorPrimary = p.Primary
	ColorMuted = p.Muted
	ColorSelection = p.Selection
	ColorHover = p.Hover
	ColorHeaderBg = p.HeaderBg
	ColorAbyss = p.StatusBg
	ColorReadOnly = p.ReadOnly
	ColorBodyText = p.BodyText
	ColorWhite = p.BrightFg
	ColorDiffContext = p.Muted

	ColorRunning = p.StatusRunning
	ColorPending = p.StatusPending
	ColorFailed = p.StatusFailed
	ColorTerminated = p.StatusTerminated
	ColorUnknown = p.StatusUnknown
	ColorSucceeded = p.StatusSucceeded

	NormalBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Border)

	FocusedBorder = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Primary)

	Header = lipgloss.NewStyle().
		Background(p.HeaderBg).
		Foreground(p.Primary).
		Bold(true).
		Padding(0, 1)

	StatusBar = lipgloss.NewStyle().
		Background(p.StatusBg).
		Foreground(p.Muted).
		Padding(0, 1)

	StatusKey = lipgloss.NewStyle().
		Foreground(p.Primary).
		Bold(true)

	StatusVal = lipgloss.NewStyle().
		Foreground(p.BodyText)

	TableHeader = lipgloss.NewStyle().
		Foreground(p.Primary).
		Bold(true).
		Padding(0, 1)

	TableRow = lipgloss.NewStyle().
		Padding(0, 1)

	TableRowSelected = lipgloss.NewStyle().
		Background(p.Selection).
		Foreground(p.BrightFg).
		Padding(0, 1)

	TableRowHover = lipgloss.NewStyle().
		Background(p.Hover).
		Padding(0, 1)

	Title = lipgloss.NewStyle().
		Foreground(p.Primary).
		Bold(true).
		PaddingLeft(1)

	Subtitle = lipgloss.NewStyle().
		Foreground(p.Muted).
		PaddingLeft(1)

	Bold = lipgloss.NewStyle().Bold(true)
	Muted = lipgloss.NewStyle().Foreground(p.Muted)
	Error = lipgloss.NewStyle().Foreground(p.Error)
	Warning = lipgloss.NewStyle().Foreground(p.Warning)
	Success = lipgloss.NewStyle().Foreground(p.Success)
	Primary = lipgloss.NewStyle().Foreground(p.Primary)
	SearchHighlight = lipgloss.NewStyle().Background(p.Warning).Foreground(p.Bg)

	DialogBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Warning).
		Padding(1, 2)

	HelpKey = lipgloss.NewStyle().Foreground(p.Primary).Bold(true)
	HelpDesc = lipgloss.NewStyle().Foreground(p.Muted)
	HelpBracket = lipgloss.NewStyle().Foreground(p.Muted)

	LogPrefixColors = p.LogPrefix
	LogPrefixStyles = make([]lipgloss.Style, len(p.LogPrefix))
	for i, c := range p.LogPrefix {
		LogPrefixStyles[i] = lipgloss.NewStyle().Foreground(c).Bold(true)
	}

	running := lipgloss.NewStyle().Foreground(p.StatusRunning)
	pending := lipgloss.NewStyle().Foreground(p.StatusPending)
	failed := lipgloss.NewStyle().Foreground(p.StatusFailed)
	terminated := lipgloss.NewStyle().Foreground(p.StatusTerminated)
	succeeded := lipgloss.NewStyle().Foreground(p.StatusSucceeded)
	statusStyleMap = map[string]lipgloss.Style{
		"Running":           running,
		"Active":            running,
		"Bound":             running,
		"Ready":             running,
		"True":              running,
		"Pending":           pending,
		"ContainerCreating": pending,
		"PodInitializing":   pending,
		"Failed":            failed,
		"Error":             failed,
		"CrashLoopBackOff":  failed,
		"OOMKilled":         failed,
		"False":             failed,
		"Terminating":       terminated,
		"Succeeded":         succeeded,
		"Completed":         succeeded,
		// Event TYPE coloring. Warning shares the pending-yellow tone so it
		// pops; Normal reuses the muted terminated-gray so it reads as
		// informational rather than implying a positive health signal.
		"Warning": pending,
		"Normal":  terminated,
		// "missing" flags an XRay reference whose target isn't in the informer
		// cache — dim-red so it reads as a real warning without screaming.
		"missing": failed.Faint(true),
	}
	statusStyleDefault = lipgloss.NewStyle().Foreground(p.StatusUnknown)

	for _, f := range onApply {
		f()
	}
}

func init() {
	Apply(KlensDark)
}

// StatusStyle returns a pre-built lipgloss style for the given Kubernetes resource status.
func StatusStyle(status string) lipgloss.Style {
	if s, ok := statusStyleMap[status]; ok {
		return s
	}
	return statusStyleDefault
}
