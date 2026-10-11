package tui

import (
	"strings"
	"testing"
)

func TestEditorInsertEntryPoints(t *testing.T) {
	cases := []struct {
		name   string
		apply  func(*Editor)
		text   string
		cursor int
	}{
		{"Append", (*Editor).Append, "abc", 1},
		{"InsertAtLineStart", (*Editor).InsertAtLineStart, "abc", 0},
		{"AppendLineEnd", (*Editor).AppendLineEnd, "abc", 3},
		{"OpenBelow", (*Editor).OpenBelow, "abc\n", 4},
		{"OpenAbove", (*Editor).OpenAbove, "\nabc", 0},
	}
	for _, test := range cases {
		editor := NewEditor()
		editor.SetText("abc")
		editor.MoveHome()
		test.apply(editor)
		if editor.Text() != test.text || editor.cursor != test.cursor || editor.Mode() != ModeInsert {
			t.Fatalf("%s -> %q cursor=%d mode=%v", test.name, editor.Text(), editor.cursor, editor.Mode())
		}
	}
}

func TestEditorDeletionHelpers(t *testing.T) {
	editor := NewEditor()
	editor.SetText("abc")
	editor.MoveEnd()
	editor.Backspace()
	if editor.Text() != "ab" || editor.cursor != 2 {
		t.Fatalf("Backspace -> %q cursor=%d", editor.Text(), editor.cursor)
	}
	editor.MoveHome()
	editor.DeleteForward()
	if editor.Text() != "b" || editor.cursor != 0 {
		t.Fatalf("DeleteForward -> %q cursor=%d", editor.Text(), editor.cursor)
	}

	editor.SetText("abcdef")
	editor.MoveHome()
	editor.MoveRight()
	editor.MoveRight()
	editor.DeleteToLineEnd()
	if editor.Text() != "ab" {
		t.Fatalf("DeleteToLineEnd -> %q", editor.Text())
	}
}

func TestEditorReplaceCharSkipsNewlines(t *testing.T) {
	editor := NewEditor()
	editor.SetText("a\nb")
	editor.MoveHome()
	editor.MoveRight() // on the newline
	editor.ReplaceChar('X')
	if editor.Text() != "a\nb" {
		t.Fatalf("ReplaceChar should not replace a newline: %q", editor.Text())
	}
	if editor.Pending() != 0 {
		t.Fatal("no operator should be pending")
	}
	editor.SetPending('r')
	if editor.Pending() != 'r' {
		t.Fatal("SetPending should remember the operator")
	}
	editor.ClearPending()
	if editor.Pending() != 0 {
		t.Fatal("ClearPending should forget the operator")
	}
}

func TestEditorMoveUpDownLeftAndCaretMotion(t *testing.T) {
	editor := NewEditor()
	editor.SetText("one\ntwo\nthree")
	editor.cursor = 0

	editor.MoveDown()
	if !strings.HasPrefix(string(editor.buffer[editor.cursor:]), "two") {
		t.Fatalf("MoveDown -> cursor %d", editor.cursor)
	}
	editor.MoveDown()
	if !strings.HasPrefix(string(editor.buffer[editor.cursor:]), "three") {
		t.Fatalf("MoveDown -> cursor %d", editor.cursor)
	}
	editor.MoveUp()
	editor.MoveUp()
	if editor.cursor != 0 {
		t.Fatalf("MoveUp -> cursor %d", editor.cursor)
	}
	editor.MoveLeft()
	if editor.cursor != 0 {
		t.Fatalf("MoveLeft should clamp at 0: %d", editor.cursor)
	}

	editor.SetText("   indented")
	editor.MoveEnd()
	editor.Motion("^")
	if editor.cursor != 3 {
		t.Fatalf("^ -> cursor %d, want 3", editor.cursor)
	}
	editor.Motion("0")
	if editor.cursor != 0 {
		t.Fatalf("0 -> cursor %d", editor.cursor)
	}
	editor.Motion("l")
	if editor.cursor != 1 {
		t.Fatalf("l -> cursor %d", editor.cursor)
	}
}

func TestModeLabels(t *testing.T) {
	if ModeInsert.Label() != "INSERT" || ModeNormal.Label() != "NORMAL" || ModeReplace.Label() != "REPLACE" {
		t.Fatalf("labels = %q %q %q", ModeInsert.Label(), ModeNormal.Label(), ModeReplace.Label())
	}
}
