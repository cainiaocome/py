package tui

import "strings"

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

func runeLen(s string) int { return len([]rune(s)) }

func padRight(s string, width int) string {
	if width <= runeLen(s) {
		return s
	}
	return s + strings.Repeat(" ", width-runeLen(s))
}

func truncateRunes(s string, width int) string {
	runes := []rune(s)
	if len(runes) <= width {
		return s
	}
	if width < 0 {
		width = 0
	}
	return string(runes[:width])
}
