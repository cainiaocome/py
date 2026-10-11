package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// spaceKey is what Bubble Tea delivers for a lone space: it has its own key
// type, with Runes left empty for backwards compatibility.
func spaceKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
}

func TestSpaceIsInsertedInInsertMode(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	update(model, runeKey("hello"))
	update(model, spaceKey())
	update(model, runeKey("world"))

	if got := model.editor.Text(); got != "hello world" {
		t.Fatalf("editor = %q, want %q", got, "hello world")
	}
}

func TestSpaceIsInsertedInReplaceMode(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.editor.SetText("abc")
	model.editor.SetMode(ModeReplace)
	model.editor.MoveHome()

	update(model, spaceKey())
	if got := model.editor.Text(); got != " bc" {
		t.Fatalf("editor = %q, want %q", got, " bc")
	}
}

func TestSpaceStillPagesTheTranscriptInNormalMode(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.height = 12
	fillTranscript(model, 80)
	update(model, tea.KeyMsg{Type: tea.KeyEsc})
	model.scrollBack(model.pageLines() * 2)

	before := model.scrollOffset
	update(model, spaceKey())
	if model.editor.Text() != "" {
		t.Fatalf("normal-mode space edited the buffer: %q", model.editor.Text())
	}
	if model.scrollOffset != before+model.pageLines() {
		t.Fatalf("normal-mode space scrolled to %d, want %d", model.scrollOffset, before+model.pageLines())
	}
}

func TestSpaceReplacesACharacterAfterTheRoperator(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.editor.SetText("abc")
	model.editor.MoveHome()
	update(model, tea.KeyMsg{Type: tea.KeyEsc})
	update(model, runeKey("r"))
	update(model, spaceKey())

	if got := model.editor.Text(); got != " bc" {
		t.Fatalf("r<space> -> %q, want %q", got, " bc")
	}
	if model.editor.Mode() != ModeNormal {
		t.Fatalf("r<space> left the editor in %v", model.editor.Mode())
	}
}
