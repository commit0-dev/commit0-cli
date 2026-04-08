package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

var mdRenderer *glamour.TermRenderer

func initMarkdownRenderer(width int) {
	if width < 20 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-4),
	)
	if err != nil {
		return
	}
	mdRenderer = r
}

func renderMarkdown(content string) string {
	if mdRenderer == nil || strings.TrimSpace(content) == "" {
		return content
	}
	out, err := mdRenderer.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimRight(out, "\n")
}
