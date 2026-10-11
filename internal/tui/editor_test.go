package tui

import (
	"strings"
	"testing"
)

func TestEditorInsertAndClearLine(t *testing.T) {
	editor := NewEditor()
	editor.InsertText("abcdef")
	editor.MoveHome()
	editor.MoveRight()
	editor.MoveRight()
	editor.MoveRight()
	editor.DeleteToLineStart()

	if got := editor.Text(); got != "def" {
		t.Fatalf("text = %q", got)
	}
	if editor.cursor != 0 {
		t.Fatalf("cursor = %d, want 0", editor.cursor)
	}
}

func TestEditorMotions(t *testing.T) {
	editor := NewEditor()
	editor.SetText("foo bar baz")
	editor.MoveHome()

	editor.Motion("w")
	if editor.cursor != 4 {
		t.Fatalf("w -> %d, want 4", editor.cursor)
	}
	editor.Motion("e")
	if editor.cursor != 6 {
		t.Fatalf("e -> %d, want 6", editor.cursor)
	}
	editor.Motion("b")
	if editor.cursor != 4 {
		t.Fatalf("b -> %d, want 4", editor.cursor)
	}
	editor.Motion("$")
	if editor.cursor != 11 {
		t.Fatalf("$ -> %d, want 11", editor.cursor)
	}
	editor.Motion("0")
	if editor.cursor != 0 {
		t.Fatalf("0 -> %d, want 0", editor.cursor)
	}
}

func TestEditorDeleteWordAndLine(t *testing.T) {
	editor := NewEditor()
	editor.SetText("foo bar")
	editor.MoveHome()
	editor.DeleteWord()
	if got := editor.Text(); got != "bar" {
		t.Fatalf("DeleteWord = %q", got)
	}

	editor.SetText("keep\nremove\nkeep2")
	editor.MoveHome()
	editor.MoveUp()
	editor.DeleteLine()
	if got := editor.Text(); got != "keep\nkeep2" {
		t.Fatalf("DeleteLine = %q", got)
	}
}

func TestEditorReplaceMode(t *testing.T) {
	editor := NewEditor()
	editor.SetText("abc")
	editor.MoveHome()
	editor.SetMode(ModeReplace)
	editor.InsertRune('X')
	if got := editor.Text(); got != "Xbc" {
		t.Fatalf("replace = %q", got)
	}
	if editor.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", editor.cursor)
	}
}

func TestEditorVisualLinesWrapAndCursor(t *testing.T) {
	editor := NewEditor()
	editor.SetText("hello world")

	lines, cursorLine, cursorCol := editor.VisualLines(8, 2)
	if len(lines) != 2 {
		t.Fatalf("lines = %#v", lines)
	}
	if lines[0] != "hello " || lines[1] != "  world" {
		t.Fatalf("lines = %#v", lines)
	}
	if cursorLine != 1 || cursorCol != 7 {
		t.Fatalf("cursor = (%d,%d), want (1,7)", cursorLine, cursorCol)
	}
}

func TestEditorVisualLinesMultiline(t *testing.T) {
	editor := NewEditor()
	editor.SetText("ab\ncd")
	lines, _, _ := editor.VisualLines(10, 2)
	if strings.Join(lines, "|") != "ab|  cd" {
		t.Fatalf("lines = %#v", lines)
	}
}
