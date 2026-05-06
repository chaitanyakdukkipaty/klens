package panels

import "strings"

import "github.com/chaitanyak/klens/internal/ui/styles"

// renderScrollbar returns a height-line string (one char per line) for a vertical scrollbar.
// Track is │ (muted); thumb is █ (primary when focused, muted otherwise).
func renderScrollbar(height, visible, total, yOffset int, focused bool) string {
	trackChar := styles.Muted.Render("│")
	thumbChar := styles.Muted.Render("█")
	if focused {
		thumbChar = styles.Primary.Render("█")
	}
	lines := make([]string, height)
	if total <= visible || height <= 0 {
		for i := range lines {
			lines[i] = trackChar
		}
		return strings.Join(lines, "\n")
	}
	thumbSize := max(1, height*visible/total)
	thumbPos := int(float64(yOffset) / float64(max(1, total-visible)) * float64(height-thumbSize))
	for i := range lines {
		if i >= thumbPos && i < thumbPos+thumbSize {
			lines[i] = thumbChar
		} else {
			lines[i] = trackChar
		}
	}
	return strings.Join(lines, "\n")
}

// joinScrollbar appends one scrollbar char per line to the right of vpContent.
func joinScrollbar(vpContent, sbStr string) string {
	vpLines := strings.Split(vpContent, "\n")
	sbLines := strings.Split(sbStr, "\n")
	combined := make([]string, len(vpLines))
	for i, line := range vpLines {
		sb := " "
		if i < len(sbLines) {
			sb = sbLines[i]
		}
		combined[i] = line + sb
	}
	return strings.Join(combined, "\n")
}
