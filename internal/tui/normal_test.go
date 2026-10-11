package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cainiaocome/py/internal/command"
)

func normalEditor(t *testing.T, text string) *Model {
	t.Helper()
	model := newFakeModel(t, newFakeChat("m"))
	model.editor.SetText(text)
	model.editor.MoveHome()
	return model
}

func pressNormal(t *testing.T, model *Model, key string) {
	t.Helper()
	update(model, tea.KeyMsg{Type: tea.KeyEsc})
	if key != "" {
		update(model, runeKey(key))
	}
}

func TestNormalModeEntryKeys(t *testing.T) {
	model := normalEditor(t, "abc")
	pressNormal(t, model, "i")
	if model.editor.Mode() != ModeInsert {
		t.Fatal("i should switch to insert mode")
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "a")
	if model.editor.Mode() != ModeInsert || model.editor.cursor != 1 {
		t.Fatalf("a -> mode=%v cursor=%d", model.editor.Mode(), model.editor.cursor)
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "I")
	if model.editor.Mode() != ModeInsert || model.editor.cursor != 0 {
		t.Fatalf("I -> mode=%v cursor=%d", model.editor.Mode(), model.editor.cursor)
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "A")
	if model.editor.Mode() != ModeInsert || model.editor.cursor != 3 {
		t.Fatalf("A -> mode=%v cursor=%d", model.editor.Mode(), model.editor.cursor)
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "R")
	if model.editor.Mode() != ModeReplace {
		t.Fatalf("R -> mode=%v", model.editor.Mode())
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "o")
	if model.editor.Text() != "abc\n" || model.editor.Mode() != ModeInsert {
		t.Fatalf("o -> %q mode=%v", model.editor.Text(), model.editor.Mode())
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "O")
	if model.editor.Text() != "\nabc" || model.editor.Mode() != ModeInsert {
		t.Fatalf("O -> %q mode=%v", model.editor.Text(), model.editor.Mode())
	}
}

func TestNormalModeEditingAndMotions(t *testing.T) {
	model := normalEditor(t, "abc")
	pressNormal(t, model, "x")
	if model.editor.Text() != "bc" {
		t.Fatalf("x -> %q", model.editor.Text())
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "D")
	if model.editor.Text() != "" {
		t.Fatalf("D -> %q", model.editor.Text())
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "C")
	if model.editor.Text() != "" || model.editor.Mode() != ModeInsert {
		t.Fatalf("C -> %q mode=%v", model.editor.Text(), model.editor.Mode())
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "r")
	update(model, runeKey("X"))
	if model.editor.Text() != "Xbc" {
		t.Fatalf("r X -> %q", model.editor.Text())
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "l")
	if model.editor.cursor != 1 {
		t.Fatalf("l -> cursor %d", model.editor.cursor)
	}
	pressNormal(t, model, "h")
	if model.editor.cursor != 0 {
		t.Fatalf("h -> cursor %d", model.editor.cursor)
	}

	model = normalEditor(t, "abc")
	pressNormal(t, model, "$")
	if model.editor.cursor != 3 {
		t.Fatalf("$ -> cursor %d", model.editor.cursor)
	}

	model = normalEditor(t, "foo bar")
	pressNormal(t, model, "w")
	if model.editor.cursor != 4 {
		t.Fatalf("w -> cursor %d", model.editor.cursor)
	}
}

func TestNormalModeBackspaceMovesLeft(t *testing.T) {
	model := normalEditor(t, "abc")
	model.editor.MoveEnd()
	update(model, tea.KeyMsg{Type: tea.KeyEsc})
	update(model, tea.KeyMsg{Type: tea.KeyBackspace})
	if model.editor.cursor != 2 {
		t.Fatalf("normal-mode backspace -> cursor %d, want 2", model.editor.cursor)
	}
}

func TestCommandPaletteRendersCommandsAndDescriptions(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.editor.SetText("/")
	model.refreshCompletions()

	view := model.View()
	for _, name := range command.Names() {
		if !strings.Contains(view, name) {
			t.Fatalf("palette is missing %s:\n%s", name, view)
		}
	}
	if !strings.Contains(view, "Show or switch the model") {
		t.Fatalf("palette should show descriptions:\n%s", view)
	}

	model.cycleCompletions(1)
	if !strings.Contains(model.View(), styleMenuSel) {
		t.Fatal("the selected command should be highlighted")
	}
}

func TestMarkdownEntriesRenderThroughGlamour(t *testing.T) {
	entry := newMarkdownEntry("# Title\n\nSome **bold** text with a [link](https://example.com).\n")
	rendered := strings.Join(entry.lines(80), "\n")
	if !strings.Contains(rendered, "Title") {
		t.Fatalf("markdown was not rendered: %q", rendered)
	}
	if strings.Contains(rendered, "# Title") || strings.Contains(rendered, "**bold**") {
		t.Fatalf("markdown syntax leaked into the output: %q", rendered)
	}
	if !strings.Contains(rendered, "link") {
		t.Fatalf("link text is missing: %q", rendered)
	}
}

func TestViewRendersTheLiveMarkdownAnswer(t *testing.T) {
	fake := newFakeChat("m")
	fake.stream = func(_ context.Context, _ string, _, onText func(string)) (string, error) {
		onText("# Heading\n\nbody")
		return "# Heading\n\nbody", nil
	}
	model := newFakeModel(t, fake)

	update(model, runeKey("q"))
	pump(t, model, update(model, tea.KeyMsg{Type: tea.KeyEnter}))

	view := model.View()
	if !strings.Contains(view, "Heading") || strings.Contains(view, "# Heading") {
		t.Fatalf("the live answer was not rendered as markdown:\n%s", view)
	}
}
