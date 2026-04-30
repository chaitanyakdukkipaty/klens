package styles

import "github.com/charmbracelet/lipgloss"

var (
	// Base colors
	colorPrimary   = lipgloss.Color("#00ADD8") // Kubernetes blue
	colorSecondary = lipgloss.Color("#5C6BC0")
	colorAccent    = lipgloss.Color("#4CAF50")
	colorWarning   = lipgloss.Color("#FFC107")
	colorError     = lipgloss.Color("#F44336")
	colorMuted     = lipgloss.Color("#666666")
	colorBg        = lipgloss.Color("#1A1A2E")
	colorBorder    = lipgloss.Color("#333355")
	colorFocused   = lipgloss.Color("#00ADD8")

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
			BorderForeground(colorFocused)

	// Header
	Header = lipgloss.NewStyle().
		Background(colorBg).
		Foreground(colorPrimary).
		Bold(true).
		Padding(0, 1)

	// Status bar
	StatusBar = lipgloss.NewStyle().
			Background(lipgloss.Color("#0D0D1A")).
			Foreground(colorMuted).
			Padding(0, 1)

	StatusKey = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	StatusVal = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA"))

	// Table
	TableHeader = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Padding(0, 1)

	TableRow = lipgloss.NewStyle().
			Padding(0, 1)

	TableRowSelected = lipgloss.NewStyle().
				Background(colorSecondary).
				Foreground(lipgloss.Color("#FFFFFF")).
				Padding(0, 1)

	// Titles
	Title = lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true).
		MarginLeft(1)

	Subtitle = lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginLeft(1)

	// Text variants
	Bold    = lipgloss.NewStyle().Bold(true)
	Muted   = lipgloss.NewStyle().Foreground(colorMuted)
	Error   = lipgloss.NewStyle().Foreground(colorError)
	Warning = lipgloss.NewStyle().Foreground(colorWarning)
	Success = lipgloss.NewStyle().Foreground(colorAccent)
	Primary = lipgloss.NewStyle().Foreground(colorPrimary)

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
		lipgloss.Color("#9C27B0"),
		lipgloss.Color("#FF5722"),
		lipgloss.Color("#03A9F4"),
		lipgloss.Color("#8BC34A"),
		lipgloss.Color("#FF9800"),
	}

)

// StatusStyle returns a lipgloss style based on Kubernetes resource status.
func StatusStyle(status string) lipgloss.Style {
	switch status {
	case "Running", "Active", "Bound", "True":
		return lipgloss.NewStyle().Foreground(ColorRunning)
	case "Pending", "ContainerCreating", "PodInitializing":
		return lipgloss.NewStyle().Foreground(ColorPending)
	case "Failed", "Error", "CrashLoopBackOff", "OOMKilled", "False":
		return lipgloss.NewStyle().Foreground(ColorFailed)
	case "Terminating":
		return lipgloss.NewStyle().Foreground(ColorTerminated)
	case "Succeeded", "Completed":
		return lipgloss.NewStyle().Foreground(ColorSucceeded)
	default:
		return lipgloss.NewStyle().Foreground(ColorUnknown)
	}
}
