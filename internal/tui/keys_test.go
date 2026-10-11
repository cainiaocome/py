package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyBindingsCoverTheDocumentedKeys(t *testing.T) {
	// buildKeyBindings panics if a binding is registered twice, so reaching
	// here already proves there are no hidden duplicates.
	normal := []string{
		"i", "a", "I", "A", "o", "O", "R", "r", "x", "delete", "D", "C",
		"h", "l", "w", "b", "e", "0", "$", "^", "backspace",
		"j", "k", "u", "d", " ", "g", "G", "p", "n",
	}
	for _, key := range normal {
		if _, ok := keyBindings[keyStroke{key: key, mode: inNormal}]; !ok {
			t.Errorf("normal mode has no binding for %q", key)
		}
	}

	// Vim letters must not be bound while typing, or they would be swallowed
	// instead of inserted.
	for _, key := range []string{"i", "a", "x", "D", "h", "l", "j", "k", "o", "O", "R", "p", "n", "g", "G", "u", "d", "w", "b", "e"} {
		if _, ok := keyBindings[keyStroke{key: key, mode: inInsert}]; ok {
			t.Errorf("insert mode should not bind %q", key)
		}
	}

	// Bindings that must work everywhere, including while typing.
	for _, key := range []string{"ctrl+c", "enter", "ctrl+d", "up", "down", "left", "right", "esc"} {
		for _, mode := range []modeMask{inInsert, inNormal, inReplace} {
			if _, ok := keyBindings[keyStroke{key: key, mode: mode}]; !ok {
				t.Errorf("%q is not bound in mode %v", key, mode)
			}
		}
	}
}

func TestReplaceModeSharesInsertBindings(t *testing.T) {
	// insertModes registers a binding for both modes, so typing still works
	// after switching to overtype.
	for _, key := range []string{"backspace", "ctrl+u", "ctrl+e"} {
		if _, ok := keyBindings[keyStroke{key: key, mode: inReplace}]; !ok {
			t.Errorf("%q is not bound in replace mode", key)
		}
	}
	if _, ok := keyBindings[keyStroke{key: "ctrl+e", mode: inNormal}]; ok {
		t.Error("ctrl+e does nothing in normal mode, so it should not be bound there")
	}
}

func TestVimLettersAreTypedInInsertMode(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	for _, key := range []string{"j", "k", "x", "D", "i", "o", "g", "p", "n"} {
		update(model, runeKey(key))
	}
	if got, want := model.editor.Text(), "jkxDiogpn"; got != want {
		t.Fatalf("editor = %q, want %q", got, want)
	}
	if model.editor.Mode() != ModeInsert {
		t.Fatalf("mode = %v, want insert", model.editor.Mode())
	}
}

func TestPendingReplaceConsumesTheNextKey(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.width, model.height = 40, 12
	fillTranscript(model, 60)
	model.scrollBack(5)
	model.editor.SetText("abc")
	model.editor.MoveHome()
	update(model, tea.KeyMsg{Type: tea.KeyEsc})

	paused := model.scrollOffset
	update(model, runeKey("r"))
	update(model, runeKey("j"))

	if got, want := model.editor.Text(), "jbc"; got != want {
		t.Fatalf("r then j -> %q, want %q (the key must be overtyped, not interpreted)", got, want)
	}
	if model.scrollOffset != paused {
		t.Fatalf("r then j scrolled the transcript: %d -> %d", paused, model.scrollOffset)
	}
}

func TestCtrlUIsContextSensitive(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.width, model.height = 40, 12
	fillTranscript(model, 60)
	model.scrollBack(5)

	// Insert mode with text deletes to the start of the line.
	update(model, runeKey("hello world"))
	update(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	if got := model.editor.Text(); got != "" {
		t.Fatalf("ctrl+u with text -> %q, want empty", got)
	}
	if model.scrollOffset != model.maxScrollOffset(model.viewportHeight())-5 {
		t.Fatal("ctrl+u deleted text and also scrolled")
	}

	// Insert mode with an empty buffer scrolls instead.
	before := model.scrollOffset
	update(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	if model.scrollOffset != before-model.halfPageLines() {
		t.Fatalf("ctrl+u on an empty buffer scrolled to %d, want %d",
			model.scrollOffset, before-model.halfPageLines())
	}

	// Normal mode always scrolls, never edits.
	update(model, runeKey("z"))
	update(model, tea.KeyMsg{Type: tea.KeyEsc})
	before = model.scrollOffset
	update(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	if got := model.editor.Text(); got != "z" {
		t.Fatalf("normal-mode ctrl+u edited the buffer: %q", got)
	}
	if model.scrollOffset == before {
		t.Fatal("normal-mode ctrl+u should scroll")
	}
}

func TestEveryNormalModeBindingRunsWithoutPanicking(t *testing.T) {
	// A smoke test over the whole table: no binding may crash on a fresh model
	// with an empty buffer and an empty transcript.
	for stroke := range keyBindings {
		if stroke.mode != inNormal {
			continue
		}
		model := newFakeModel(t, newFakeChat("m"))
		model.width, model.height = 40, 12
		update(model, tea.KeyMsg{Type: tea.KeyEsc})
		update(model, runeKey(stroke.key))
		if strings.Contains(model.editor.Text(), "\x00") {
			t.Fatalf("%q corrupted the buffer", stroke.key)
		}
	}
}
