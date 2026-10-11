package tui

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

// paintedCursor locates the drawn cursor in a rendered frame and returns its
// 1-based row and column.
func paintedCursor(t *testing.T, view string) (int, int) {
	t.Helper()
	for index, line := range strings.Split(view, "\n") {
		if offset := strings.Index(line, beamCursor); offset >= 0 {
			return index + 1, utf8.RuneCountInString(line[:offset]) + 1
		}
	}
	t.Fatalf("no cursor was painted into:\n%s", view)
	return 0, 0
}

func TestCursorAnchorTracksThePaintedCursor(t *testing.T) {
	model := New(newFakeChat("m"))
	model.width, model.height = 40, 12

	inputs := []string{
		"h",
		"hello",
		"hello\nworld",
		"a much longer single line of input",
	}
	for _, input := range inputs {
		model.editor.SetText(input)
		row, column := paintedCursor(t, model.View())
		anchorRow, anchorColumn, ok := model.cursorAnchor.position()
		if !ok {
			t.Fatalf("input %q: the anchor was never published", input)
		}
		if anchorRow != row || anchorColumn != column {
			t.Fatalf("input %q: anchor = (%d,%d), painted cursor at (%d,%d)",
				input, anchorRow, anchorColumn, row, column)
		}
	}
}

func TestCursorAnchorFollowsTheCaretWithoutChangingTheFrame(t *testing.T) {
	model := New(newFakeChat("m"))
	model.width, model.height = 40, 12
	model.editor.SetText("hello")
	model.View()

	model.editor.MoveHome()
	model.View()
	if _, column, _ := model.cursorAnchor.position(); column != 11 {
		t.Fatalf("home -> column %d, want 11 (prompt is 10 cells)", column)
	}
	if row, _, _ := model.cursorAnchor.position(); row == 0 {
		t.Fatal("the anchor should be inside the frame")
	}

	// The caret sits on the editor block, above the footer and below the
	// transcript separator.
	view := strings.Split(model.View(), "\n")
	row, _, _ := model.cursorAnchor.position()
	if !strings.Contains(view[row-1], beamCursor) {
		t.Fatalf("anchor row %d is not the editor line: %q", row, view[row-1])
	}
}

func TestCursorAnchorMovesWithAMultiLineEditor(t *testing.T) {
	model := New(newFakeChat("m"))
	model.width, model.height = 40, 12
	model.editor.SetText("one\ntwo")
	model.View()
	first, _, _ := model.cursorAnchor.position()

	model.editor.MoveUp()
	model.View()
	second, _, _ := model.cursorAnchor.position()
	if second != first-1 {
		t.Fatalf("moving up a line -> row %d, want %d", second, first-1)
	}
}

func TestCursorWriterAppendsACursorMoveToFramesOnly(t *testing.T) {
	anchor := NewCursorAnchor()
	anchor.Set(7, 13)

	var out bytes.Buffer
	writer := NewCursorWriter(&out, anchor)

	frame := []byte("\x1b[Hsome content\x1b[12;H")
	written, err := writer.Write(frame)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written != len(frame) {
		t.Fatalf("Write reported %d bytes, want %d", written, len(frame))
	}
	if got, want := out.String(), string(frame)+"\x1b[?25l\x1b[7;13H"; got != want {
		t.Fatalf("frame -> %q, want %q", got, want)
	}

	// Everything the program writes besides a frame must pass through
	// untouched, so the cursor is not hidden or moved after leaving the
	// alternate screen.
	for _, sequence := range []string{"\x1b[?1049l", "\x1b[?25l", "\x1b[?25h", "\x1b[H", "\x1b[2J", "", "plain text"} {
		out.Reset()
		if _, err := writer.Write([]byte(sequence)); err != nil {
			t.Fatalf("Write(%q): %v", sequence, err)
		}
		if out.String() != sequence {
			t.Fatalf("Write(%q) -> %q, want it unchanged", sequence, out.String())
		}
	}
}

func TestCursorWriterDoesNothingWithoutAPosition(t *testing.T) {
	var out bytes.Buffer
	writer := NewCursorWriter(&out, NewCursorAnchor())
	if _, err := writer.Write([]byte("\x1b[12;H")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if out.String() != "\x1b[12;H" {
		t.Fatalf("an unanchored writer changed the frame: %q", out.String())
	}

	anchor := NewCursorAnchor()
	anchor.Set(0, 5)
	if _, _, ok := anchor.position(); ok {
		t.Fatal("a zero row should clear the anchor")
	}
	anchor.Set(3, 0)
	if _, _, ok := anchor.position(); ok {
		t.Fatal("a zero column should clear the anchor")
	}
}

func TestCursorAnchorIsNilSafe(t *testing.T) {
	var anchor *CursorAnchor
	anchor.Set(1, 1)
	if _, _, ok := anchor.position(); ok {
		t.Fatal("a nil anchor must not report a position")
	}
}

func TestIsFrameTail(t *testing.T) {
	frames := []string{"\x1b[1;H", "\x1b[24;H", "content\n\x1b[30;H"}
	for _, frame := range frames {
		if !isFrameTail([]byte(frame)) {
			t.Fatalf("%q should be recognised as a frame", frame)
		}
	}
	others := []string{"", "\x1b[H", "\x1b[?1049l", "\x1b[12;5H", "\x1b[;H", "\x1b[12;Hx", "H", "\x1b[12;", "\x1b12;H"}
	for _, other := range others {
		if isFrameTail([]byte(other)) {
			t.Fatalf("%q should not be recognised as a frame", other)
		}
	}
}

// terminalFile is the interface Bubble Tea asserts its output against to find
// the window size (x/term's File). CursorWriter must satisfy it, or the UI
// falls back to a fixed 80x24 window.
type terminalFile interface {
	io.ReadWriteCloser
	Fd() uintptr
}

type fakeTerminal struct {
	bytes.Buffer
	fd     uintptr
	closed bool
}

func (f *fakeTerminal) Fd() uintptr { return f.fd }
func (f *fakeTerminal) Close() error {
	f.closed = true
	return nil
}

func TestCursorWriterStillLooksLikeATerminal(t *testing.T) {
	file := &fakeTerminal{fd: 42}
	writer := NewCursorWriter(file, NewCursorAnchor())

	if _, ok := any(writer).(terminalFile); !ok {
		t.Fatal("CursorWriter must satisfy io.ReadWriteCloser + Fd, or Bubble Tea cannot discover the window size")
	}
	if got := writer.Fd(); got != 42 {
		t.Fatalf("Fd = %d, want the wrapped descriptor 42", got)
	}
	if err := writer.Close(); err != nil || !file.closed {
		t.Fatal("Close should be forwarded to the terminal")
	}

	if writer := NewCursorWriter(io.Discard, NewCursorAnchor()); writer.Fd() != ^uintptr(0) {
		t.Fatal("a non-terminal writer should report an invalid descriptor")
	}
	if _, err := io.Copy(io.Discard, NewCursorWriter(io.Discard, NewCursorAnchor())); err != nil {
		t.Fatalf("reading from a non-terminal writer should just report EOF: %v", err)
	}
}
