package tui

import (
	"strings"
	"unicode"
)

// Mode is the editor's vim mode.
type Mode int

// Editor modes.
const (
	ModeInsert Mode = iota
	ModeNormal
	ModeReplace
)

// Label is the footer name of the mode.
func (m Mode) Label() string {
	switch m {
	case ModeNormal:
		return "NORMAL"
	case ModeReplace:
		return "REPLACE"
	default:
		return "INSERT"
	}
}

// mask is the modeMask bit for this mode, used to look up key bindings.
func (m Mode) mask() modeMask {
	switch m {
	case ModeNormal:
		return inNormal
	case ModeReplace:
		return inReplace
	default:
		return inInsert
	}
}

// Editor is a modal, multi-line text buffer with the vim subset this app uses.
// Most normal-mode letters are claimed by the app for scrolling, so only the
// editing motions live here.
type Editor struct {
	buffer  []rune
	cursor  int
	mode    Mode
	pending rune
}

// NewEditor builds an empty editor in insert mode.
func NewEditor() *Editor { return &Editor{mode: ModeInsert} }

// Mode returns the current mode.
func (e *Editor) Mode() Mode { return e.mode }

// Text returns the buffer contents.
func (e *Editor) Text() string { return string(e.buffer) }

// Empty reports whether the buffer is empty.
func (e *Editor) Empty() bool { return len(e.buffer) == 0 }

// CursorAtEnd reports whether the cursor is at the end of the buffer.
func (e *Editor) CursorAtEnd() bool { return e.cursor == len(e.buffer) }

// SetText replaces the buffer and parks the cursor at the end.
func (e *Editor) SetText(text string) {
	e.buffer = []rune(text)
	e.cursor = len(e.buffer)
}

// Clear empties the buffer.
func (e *Editor) Clear() {
	e.buffer = nil
	e.cursor = 0
}

// SetMode switches modes and forgets any pending normal-mode operator.
func (e *Editor) SetMode(mode Mode) {
	e.mode = mode
	e.pending = 0
}

func (e *Editor) lineStart(i int) int {
	for i > 0 && e.buffer[i-1] != '\n' {
		i--
	}
	return i
}

func (e *Editor) lineEnd(i int) int {
	for i < len(e.buffer) && e.buffer[i] != '\n' {
		i++
	}
	return i
}

// MoveLeft moves the cursor one rune left.
func (e *Editor) MoveLeft() {
	if e.cursor > 0 {
		e.cursor--
	}
}

// MoveRight moves the cursor one rune right.
func (e *Editor) MoveRight() {
	if e.cursor < len(e.buffer) {
		e.cursor++
	}
}

// MoveHome moves to the start of the current line.
func (e *Editor) MoveHome() { e.cursor = e.lineStart(e.cursor) }

// MoveEnd moves to the end of the current line.
func (e *Editor) MoveEnd() { e.cursor = e.lineEnd(e.cursor) }

// MoveUp moves to the previous line, keeping the column when possible.
func (e *Editor) MoveUp() {
	start := e.lineStart(e.cursor)
	if start == 0 {
		return
	}
	column := e.cursor - start
	previousEnd := start - 1
	previousStart := e.lineStart(previousEnd)
	e.cursor = min(previousStart+column, previousEnd)
}

// MoveDown moves to the next line, keeping the column when possible.
func (e *Editor) MoveDown() {
	end := e.lineEnd(e.cursor)
	if end >= len(e.buffer) {
		return
	}
	start := e.lineStart(e.cursor)
	column := e.cursor - start
	nextStart := end + 1
	nextEnd := e.lineEnd(nextStart)
	e.cursor = min(nextStart+column, nextEnd)
}

// InsertRune inserts (or replaces, in replace mode) one rune.
func (e *Editor) InsertRune(r rune) {
	if e.mode == ModeReplace && e.cursor < len(e.buffer) && e.buffer[e.cursor] != '\n' {
		e.buffer[e.cursor] = r
		e.cursor++
		return
	}
	e.buffer = append(e.buffer, 0)
	copy(e.buffer[e.cursor+1:], e.buffer[e.cursor:])
	e.buffer[e.cursor] = r
	e.cursor++
}

// InsertText inserts every rune of s.
func (e *Editor) InsertText(s string) {
	for _, r := range s {
		e.InsertRune(r)
	}
}

// Newline inserts a line break at the cursor.
func (e *Editor) Newline() { e.InsertRune('\n') }

// Backspace deletes the rune before the cursor.
func (e *Editor) Backspace() {
	if e.cursor == 0 {
		return
	}
	e.buffer = append(e.buffer[:e.cursor-1], e.buffer[e.cursor:]...)
	e.cursor--
}

// DeleteForward deletes the rune under the cursor.
func (e *Editor) DeleteForward() {
	if e.cursor >= len(e.buffer) {
		return
	}
	e.buffer = append(e.buffer[:e.cursor], e.buffer[e.cursor+1:]...)
}

// DeleteToLineStart deletes from the start of the line to the cursor.
func (e *Editor) DeleteToLineStart() {
	start := e.lineStart(e.cursor)
	if start == e.cursor {
		return
	}
	e.buffer = append(e.buffer[:start], e.buffer[e.cursor:]...)
	e.cursor = start
}

// DeleteToLineEnd deletes from the cursor to the end of the line.
func (e *Editor) DeleteToLineEnd() {
	end := e.lineEnd(e.cursor)
	e.buffer = append(e.buffer[:e.cursor], e.buffer[end:]...)
}

// DeleteLine deletes the whole current line.
func (e *Editor) DeleteLine() {
	start := e.lineStart(e.cursor)
	end := e.lineEnd(e.cursor)
	if end < len(e.buffer) {
		end++
	}
	e.buffer = append(e.buffer[:start], e.buffer[end:]...)
	e.cursor = start
}

// DeleteWord deletes to the start of the next word.
func (e *Editor) DeleteWord() {
	end := e.wordForward(e.cursor)
	e.buffer = append(e.buffer[:e.cursor], e.buffer[end:]...)
}

// InsertMode switches to insert mode in place.
func (e *Editor) InsertMode() { e.SetMode(ModeInsert) }

// Append switches to insert mode after the cursor.
func (e *Editor) Append() {
	if e.cursor < len(e.buffer) {
		e.cursor++
	}
	e.SetMode(ModeInsert)
}

// InsertAtLineStart switches to insert mode at the first non-blank column.
func (e *Editor) InsertAtLineStart() {
	e.cursor = e.lineStart(e.cursor)
	e.SetMode(ModeInsert)
}

// AppendLineEnd switches to insert mode at the end of the line.
func (e *Editor) AppendLineEnd() {
	e.cursor = e.lineEnd(e.cursor)
	e.SetMode(ModeInsert)
}

// OpenBelow inserts a new line below and enters insert mode.
func (e *Editor) OpenBelow() {
	e.cursor = e.lineEnd(e.cursor)
	e.InsertRune('\n')
	e.SetMode(ModeInsert)
}

// OpenAbove inserts a new line above and enters insert mode.
func (e *Editor) OpenAbove() {
	e.cursor = e.lineStart(e.cursor)
	e.InsertRune('\n')
	e.cursor--
	e.SetMode(ModeInsert)
}

// ReplaceMode switches to overtype mode.
func (e *Editor) ReplaceMode() { e.SetMode(ModeReplace) }

// ReplaceChar overtypes the rune under the cursor.
func (e *Editor) ReplaceChar(r rune) {
	if e.cursor < len(e.buffer) && e.buffer[e.cursor] != '\n' {
		e.buffer[e.cursor] = r
	}
}

// Pending reports the pending normal-mode operator rune, if any.
func (e *Editor) Pending() rune { return e.pending }

// SetPending remembers a normal-mode operator.
func (e *Editor) SetPending(r rune) { e.pending = r }

// ClearPending forgets any pending operator.
func (e *Editor) ClearPending() { e.pending = 0 }

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func (e *Editor) wordForward(i int) int {
	n := len(e.buffer)
	for i < n && isWordRune(e.buffer[i]) {
		i++
	}
	for i < n && !isWordRune(e.buffer[i]) {
		i++
	}
	return i
}

func (e *Editor) wordBackward(i int) int {
	for i > 0 && !isWordRune(e.buffer[i-1]) {
		i--
	}
	for i > 0 && isWordRune(e.buffer[i-1]) {
		i--
	}
	return i
}

func (e *Editor) wordEnd(i int) int {
	n := len(e.buffer)
	if i < n {
		i++
	}
	for i < n && !isWordRune(e.buffer[i]) {
		i++
	}
	for i < n && isWordRune(e.buffer[i]) {
		i++
	}
	if i > 0 {
		i--
	}
	return i
}

// Motion applies a normal-mode motion by name.
func (e *Editor) Motion(name string) {
	switch name {
	case "h":
		e.MoveLeft()
	case "l":
		e.MoveRight()
	case "0":
		e.MoveHome()
	case "^":
		start := e.lineStart(e.cursor)
		for start < len(e.buffer) && (e.buffer[start] == ' ' || e.buffer[start] == '\t') {
			start++
		}
		e.cursor = start
	case "$":
		e.MoveEnd()
	case "w":
		e.cursor = e.wordForward(e.cursor)
	case "b":
		e.cursor = e.wordBackward(e.cursor)
	case "e":
		e.cursor = e.wordEnd(e.cursor)
	}
}

// VisualLines wraps the buffer for display. indent spaces prefix every line
// after the first, matching a prompt that shares the first line. It returns the
// lines plus the cursor's (line, column) in that coordinate space.
func (e *Editor) VisualLines(width, indent int) ([]string, int, int) {
	available := max(1, width-indent)
	buffer := e.buffer
	var lines []string
	cursorLine, cursorCol := 0, 0
	logicalStart := 0
	for {
		end := logicalStart
		for end < len(buffer) && buffer[end] != '\n' {
			end++
		}
		lineRunes := buffer[logicalStart:end]
		count := (len(lineRunes) + available - 1) / available
		if count < 1 {
			count = 1
		}
		for chunk := 0; chunk < count; chunk++ {
			from := min(chunk*available, len(lineRunes))
			to := min(from+available, len(lineRunes))
			text := string(lineRunes[from:to])
			if len(lines) > 0 {
				text = strings.Repeat(" ", indent) + text
			}
			lines = append(lines, text)
		}
		if e.cursor >= logicalStart && e.cursor <= end {
			offset := e.cursor - logicalStart
			chunk := offset / available
			if chunk >= count {
				chunk = count - 1
			}
			cursorLine = len(lines) - count + chunk
			cursorCol = offset - chunk*available
			if cursorLine > 0 {
				cursorCol += indent
			}
		}
		if end >= len(buffer) {
			break
		}
		logicalStart = end + 1
	}
	if len(lines) == 0 {
		lines = []string{""}
	}
	return lines, cursorLine, cursorCol
}
