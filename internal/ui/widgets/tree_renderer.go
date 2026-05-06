package widgets

import (
	"strings"

	"charm.land/lipgloss/v2"
	k8s "github.com/chaitanyak/klens/internal/k8s"
	appstyles "github.com/chaitanyak/klens/internal/ui/styles"
)

var (
	treeKindStyle = lipgloss.NewStyle().Foreground(appstyles.ColorPrimary).Bold(true)
	treeNameStyle = lipgloss.NewStyle().Foreground(appstyles.ColorBodyText)
)

// RenderTree renders a TreeNode hierarchy using unicode box-drawing characters.
func RenderTree(root *k8s.TreeNode) string {
	var lines []string
	renderNode(root, "", true, &lines)
	return strings.Join(lines, "\n")
}

func renderNode(node *k8s.TreeNode, prefix string, isLast bool, lines *[]string) {
	connector := "├─ "
	childPrefix := prefix + "│  "
	if isLast {
		connector = "└─ "
		childPrefix = prefix + "   "
	}

	statusStyle := appstyles.StatusStyle(node.Status)

	label := treeKindStyle.Render(node.Kind) + " " +
		treeNameStyle.Render(node.Name) + " " +
		statusStyle.Render("[" + node.Status + "]")

	if prefix == "" {
		// Root node — no connector
		*lines = append(*lines, label)
		childPrefix = "  "
	} else {
		*lines = append(*lines, appstyles.Muted.Render(prefix+connector)+label)
	}

	for i, child := range node.Children {
		last := i == len(node.Children)-1
		renderNode(child, childPrefix, last, lines)
	}
}
