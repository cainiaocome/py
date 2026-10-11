package tui

import (
	"io"
	"strconv"
	"sync"
)

// CursorAnchor is the terminal cell the hardware cursor should rest on. It is
// written by the model while rendering and read by CursorWriter, which run on
// different goroutines, so access is synchronized.
//
// Terminals place the IME candidate window (the popup used for Chinese,
// Japanese and Korean input) at the hardware cursor, and they read that
// position whether or not the cursor is visible. Bubble Tea v1 always parks the
// cursor at the bottom-left corner, which is why an IME popup would otherwise
// appear there instead of at the caret.
type CursorAnchor struct {
	mu       sync.Mutex
	row      int
	column   int
	anchored bool
}

// NewCursorAnchor returns an anchor with no position yet.
func NewCursorAnchor() *CursorAnchor { return &CursorAnchor{} }

// Set records a 1-based terminal cell. Out-of-range values clear the anchor.
// It is safe to call on a nil *CursorAnchor.
func (a *CursorAnchor) Set(row, column int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.row, a.column = row, column
	a.anchored = row > 0 && column > 0
}

func (a *CursorAnchor) position() (int, int, bool) {
	if a == nil {
		return 0, 0, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.row, a.column, a.anchored
}

// CursorWriter passes terminal output through unchanged, except that it appends
// a cursor move to the end of every rendered frame so the hardware cursor
// follows the editor caret. Appending within the same write keeps the frame and
// the move atomic, so the terminal can never observe a stale cursor position.
type CursorWriter struct {
	out    io.Writer
	anchor *CursorAnchor
}

// NewCursorWriter wraps out so that rendered frames carry a cursor move.
func NewCursorWriter(out io.Writer, anchor *CursorAnchor) *CursorWriter {
	return &CursorWriter{out: out, anchor: anchor}
}

// Write implements io.Writer.
func (w *CursorWriter) Write(p []byte) (int, error) {
	row, column, anchored := w.anchor.position()
	if !anchored || !isFrameTail(p) {
		return w.out.Write(p)
	}
	buf := make([]byte, 0, len(p)+24)
	buf = append(buf, p...)
	// Hide the cursor before moving it so it never flashes at the old spot,
	// then rest it on the caret.
	buf = append(buf, "\x1b[?25l\x1b["...)
	buf = strconv.AppendInt(buf, int64(row), 10)
	buf = append(buf, ';')
	buf = strconv.AppendInt(buf, int64(column), 10)
	buf = append(buf, 'H')
	written, err := w.out.Write(buf)
	if written > len(p) {
		written = len(p)
	}
	return written, err
}

// isFrameTail reports whether p is a rendered frame. Bubble Tea's standard
// renderer finishes every alternate-screen frame with a cursor park of the form
// ESC [ <rows> ; H, which distinguishes frames from the other sequences the
// program writes (alt-screen enter/exit, cursor hide/show, screen clears).
func isFrameTail(p []byte) bool {
	if len(p) < 5 || p[len(p)-1] != 'H' || p[len(p)-2] != ';' {
		return false
	}
	index := len(p) - 3
	digits := 0
	for index >= 0 && p[index] >= '0' && p[index] <= '9' {
		index--
		digits++
	}
	return digits > 0 && index > 0 && p[index] == '[' && p[index-1] == 0x1b
}
