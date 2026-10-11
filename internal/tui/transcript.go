package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

// Entry is one retained transcript print. Rendered lines are cached per width
// and revision so a redraw only touches entries whose content changed.
type Entry struct {
	markdown bool
	text     string
	style    string // ANSI SGR parameters, empty for plain text
	live     bool
	revision int

	cacheWidth int
	cacheRev   int
	cache      []string
}

func newMarkdownEntry(text string) *Entry { return &Entry{markdown: true, text: text} }

func newTextEntry(text string) *Entry { return &Entry{text: text} }

func newStyledEntry(text, style string) *Entry {
	return &Entry{text: text, style: style}
}

func (e *Entry) invalidate() {
	e.revision++
	e.cache = nil
}

func (e *Entry) setText(text string) {
	e.text = text
	e.invalidate()
}

// lines returns the cached styled lines for this entry at width.
func (e *Entry) lines(width int) []string {
	if e.cache != nil && e.cacheWidth == width && e.cacheRev == e.revision {
		return e.cache
	}
	var lines []string
	if e.markdown {
		lines = markdownLines(e.text, width)
	} else {
		for _, line := range wrapText(e.text, width) {
			lines = append(lines, sgr(e.style, line))
		}
	}
	if len(lines) == 0 {
		lines = []string{sgr(e.style, "")}
	}
	e.cache = lines
	e.cacheWidth = width
	e.cacheRev = e.revision
	return lines
}

var (
	rendererMu sync.Mutex
	renderers  = map[int]*glamour.TermRenderer{}
)

func markdownRenderer(width int) *glamour.TermRenderer {
	if width < 1 {
		width = 1
	}
	rendererMu.Lock()
	defer rendererMu.Unlock()
	if renderer, ok := renderers[width]; ok {
		return renderer
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithWordWrap(width),
		glamour.WithStandardStyle("dark"),
	)
	if err != nil {
		return nil
	}
	renderers[width] = renderer
	return renderer
}

func markdownLines(text string, width int) []string {
	renderer := markdownRenderer(width)
	if renderer == nil {
		return wrapText(text, width)
	}
	out, err := renderer.Render(text)
	if err != nil {
		return wrapText(text, width)
	}
	out = strings.TrimRight(out, "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// wrapText wraps hard at width runes while preserving explicit newlines.
func wrapText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, logical := range strings.Split(text, "\n") {
		runes := []rune(logical)
		if len(runes) == 0 {
			out = append(out, "")
			continue
		}
		for len(runes) > width {
			out = append(out, string(runes[:width]))
			runes = runes[width:]
		}
		out = append(out, string(runes))
	}
	return out
}
