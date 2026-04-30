package widgets

import (
	"strings"
)

var blocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Sparkline renders a fixed-width unicode block sparkline from a slice of float64 values.
// If values is empty or has only one element, a minimal bar is rendered.
func Sparkline(values []float64, width int) string {
	if width <= 0 {
		return ""
	}
	if len(values) == 0 {
		return strings.Repeat("▁", width)
	}

	// Use the last `width` samples
	if len(values) > width {
		values = values[len(values)-width:]
	}

	min, max := values[0], values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}

	rng := max - min
	var sb strings.Builder
	for _, v := range values {
		var idx int
		if rng > 0 {
			idx = int((v - min) / rng * float64(len(blocks)-1))
		}
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		sb.WriteRune(blocks[idx])
	}

	// Pad to width with lowest block
	result := sb.String()
	for len([]rune(result)) < width {
		result = string(blocks[0]) + result
	}
	return result
}
