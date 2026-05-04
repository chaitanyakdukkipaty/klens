package styles

import "github.com/charmbracelet/lipgloss"

var (
	// Base colors (private — consumed by styles in this file)
	colorPrimary   = lipgloss.Color("#00ADD8")
	colorSecondary = lipgloss.Color("#5C6BC0")
	colorAccent    = lipgloss.Color("#4CAF50")
	colorWarning   = lipgloss.Color("#FFC107")
	colorError     = lipgloss.Color("#F44336")
	colorMuted     = lipgloss.Color("#888888")
	colorBg        = lipgloss.Color("#1A1A2E")
	colorBorder    = lipgloss.Color("#333355")

	// Exported base colors (for direct use by panels/widgets)
	ColorPrimary    = colorPrimary
	ColorMuted      = colorMuted
	ColorSelection  = colorSecondary            // #5C6BC0 — selected row/nav background
	ColorHeaderBg   = lipgloss.Color("#0D1117") // Space Black — header bar
	ColorAbyss      = lipgloss.Color("#0D0D1A") // Abyss — status bar, dialog backdrop
	ColorReadOnly   = lipgloss.Color("#FF6B6B") // Readonly Coral — [RO] indicator
	ColorBodyText   = lipgloss.Color("#AAAAAA") // secondary body text
	ColorWhite      = lipgloss.Color("#FFFFFF") // selection foreground
	ColorDiffContext = lipgloss.Color("#888888") // diff viewer context lines (unchanged lines)

	// Status colors
	ColorRunning    = lipgloss.Color("#4CAF50")
	ColorPending    = lipgloss.Color("#FFC107")
	ColorFailed     = lipgloss.Color("#F44336")
	ColorTerminated = lipgloss.Color("#9E9E9E")
	ColorUnknown    = lipgloss.Color("#9E9E9E")
	ColorSucceeded  = lipgloss.Color("#64B5F6")

	// Panel borders
	NormalBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder)

	FocusedBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary)

	// Header
	Header = lipgloss.NewStyle().
		Background(ColorHeaderBg).
		Foreground(colorPrimary).
		Bold(true).
		Padding(0, 1)

	// Status bar
	StatusBar = lipgloss.NewStyle().
			Background(ColorAbyss).
			Foreground(colorMuted).
			Padding(0, 1)

	StatusKey = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	StatusVal = lipgloss.NewStyle().
			Foreground(ColorBodyText)

	// Table
	TableHeader = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Padding(0, 1)

	TableRow = lipgloss.NewStyle().
			Padding(0, 1)

	TableRowSelected = lipgloss.NewStyle().
				Background(ColorSelection).
				Foreground(ColorWhite).
				Padding(0, 1)

	// Titles
	Title = lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true).
		PaddingLeft(1)

	Subtitle = lipgloss.NewStyle().
			Foreground(colorMuted).
			PaddingLeft(1)

	// Text variants
	Bold            = lipgloss.NewStyle().Bold(true)
	Muted           = lipgloss.NewStyle().Foreground(colorMuted)
	Error           = lipgloss.NewStyle().Foreground(colorError)
	Warning         = lipgloss.NewStyle().Foreground(colorWarning)
	Success         = lipgloss.NewStyle().Foreground(colorAccent)
	Primary         = lipgloss.NewStyle().Foreground(colorPrimary)
	SearchHighlight = lipgloss.NewStyle().Background(colorWarning).Foreground(colorBg)

	// Dialog
	DialogBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorWarning).
			Padding(1, 2)

	// Help bar (bottom)
	HelpKey  = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	HelpDesc = lipgloss.NewStyle().Foreground(colorMuted)

	// Log viewer
	LogPrefixColors = []lipgloss.Color{
		lipgloss.Color("#00ADD8"),
		lipgloss.Color("#4CAF50"),
		lipgloss.Color("#FFC107"),
		lipgloss.Color("#BA68C8"),
		lipgloss.Color("#FF5722"),
		lipgloss.Color("#03A9F4"),
		lipgloss.Color("#8BC34A"),
		lipgloss.Color("#FF9800"),
	}
)

// LogPrefixStyles are pre-built bold styles indexed parallel to LogPrefixColors.
// Using these avoids allocating a new lipgloss.Style on every rendered log line.
var LogPrefixStyles = func() []lipgloss.Style {
	s := make([]lipgloss.Style, len(LogPrefixColors))
	for i, c := range LogPrefixColors {
		s[i] = lipgloss.NewStyle().Foreground(c).Bold(true)
	}
	return s
}()

// statusStyleMap holds pre-built styles keyed by Kubernetes status string.
var statusStyleMap = map[string]lipgloss.Style{
	"Running":           lipgloss.NewStyle().Foreground(ColorRunning),
	"Active":            lipgloss.NewStyle().Foreground(ColorRunning),
	"Bound":             lipgloss.NewStyle().Foreground(ColorRunning),
	"True":              lipgloss.NewStyle().Foreground(ColorRunning),
	"Pending":           lipgloss.NewStyle().Foreground(ColorPending),
	"ContainerCreating": lipgloss.NewStyle().Foreground(ColorPending),
	"PodInitializing":   lipgloss.NewStyle().Foreground(ColorPending),
	"Failed":            lipgloss.NewStyle().Foreground(ColorFailed),
	"Error":             lipgloss.NewStyle().Foreground(ColorFailed),
	"CrashLoopBackOff":  lipgloss.NewStyle().Foreground(ColorFailed),
	"OOMKilled":         lipgloss.NewStyle().Foreground(ColorFailed),
	"False":             lipgloss.NewStyle().Foreground(ColorFailed),
	"Terminating":       lipgloss.NewStyle().Foreground(ColorTerminated),
	"Succeeded":         lipgloss.NewStyle().Foreground(ColorSucceeded),
	"Completed":         lipgloss.NewStyle().Foreground(ColorSucceeded),
}

var statusStyleDefault = lipgloss.NewStyle().Foreground(ColorUnknown)

// StatusStyle returns a pre-built lipgloss style for the given Kubernetes resource status.
func StatusStyle(status string) lipgloss.Style {
	if s, ok := statusStyleMap[status]; ok {
		return s
	}
	return statusStyleDefault
}
