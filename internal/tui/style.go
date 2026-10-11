package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ANSI SGR fragments. The transcript caches fully-styled lines, so these are
// applied once per rebuilt entry, not per frame.
const (
	ansiReset    = "\x1b[0m"
	ansiDim      = "2"
	ansiBold     = "1"
	ansiReverse  = "7"
	ansiUnder    = "4"
	styleHeader  = "1;36" // bold cyan
	styleDim     = "2"
	styleYellow  = "33"
	styleRed     = "31"
	styleMenu    = "48;5;236;38;5;255"
	styleMenuSel = "7;1;48;5;24;38;5;255"
)

// beamCursor is the drawn stand-in for a terminal "beam" cursor. Bubble Tea's
// renderer cannot position the real cursor, so the cursor has to be painted.
// A beam is a thin bar hugging the left edge of the cursor cell, unlike the
// reverse-video block used in normal mode.
const beamCursor = "\u258f"

func sgr(params, text string) string {
	if params == "" {
		return text
	}
	return "\x1b[" + params + "m" + text + ansiReset
}

// runeLen counts runes. Rune offsets index into strings; they are not screen
// columns, because a full-width character such as a Chinese one occupies two
// terminal cells.
func runeLen(s string) int { return len([]rune(s)) }

// displayWidth is the number of terminal cells s occupies.
func displayWidth(s string) int { return ansi.StringWidth(s) }

// firstRunes returns the first n runes of s.
func firstRunes(s string, n int) string {
	runes := []rune(s)
	if n <= 0 {
		return ""
	}
	if n > len(runes) {
		n = len(runes)
	}
	return string(runes[:n])
}

func padRight(s string, width int) string {
	if width <= displayWidth(s) {
		return s
	}
	return s + strings.Repeat(" ", width-displayWidth(s))
}

func truncateRunes(s string, width int) string {
	if width < 0 {
		width = 0
	}
	if displayWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "")
}
