package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// streamingModel returns a model with a response in flight, plus the command
// that will finish it.
func streamingModel(t *testing.T) (*Model, func()) {
	t.Helper()
	fake := newFakeChat("m")
	release := make(chan struct{})
	fake.stream = func(_ context.Context, _ string, _, onText func(string)) (string, error) {
		onText("partial")
		<-release
		return "partial answer", nil
	}
	model := newFakeModel(t, fake)
	update(model, runeKey("question"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	update(model, cmd()) // first paint
	if !model.responding() {
		t.Fatal("the model should be responding")
	}
	return model, func() { close(release) }
}

func TestCommandPaletteShowsWhileTheModelIsResponding(t *testing.T) {
	model, release := streamingModel(t)

	update(model, runeKey("/"))
	if len(model.completions) == 0 {
		t.Fatal("typing / during a response should offer commands")
	}
	view := model.View()
	for _, command := range Commands {
		if !strings.Contains(view, command) {
			t.Fatalf("the palette should list %s while responding:\n%s", command, view)
		}
	}

	// The user can keep editing and cycling the palette without interrupting.
	update(model, tea.KeyMsg{Type: tea.KeyTab})
	if got := model.editor.Text(); got != Commands[0] {
		t.Fatalf("tab should complete during a response: %q", got)
	}
	if !model.responding() {
		t.Fatal("using the palette must not disturb the response")
	}
	release()
}

func TestCommandPaletteIsHiddenWhileTheModelPickerOwnsTheInput(t *testing.T) {
	fake := newFakeChat("m")
	fake.models = []string{"m"}
	model := newFakeModel(t, fake)

	update(model, runeKey("/model"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, model, cmd)
	if model.overlay != overlayModelPicker {
		t.Fatalf("overlay = %v, want the picker", model.overlay)
	}

	update(model, runeKey("/"))
	if len(model.completions) != 0 {
		t.Fatal("the picker owns the input, so completions should stay hidden")
	}
	if menu := model.menuLines(80); menu != nil {
		t.Fatalf("no palette should be drawn over the model picker: %v", menu)
	}
}

func TestRespondingTracksTheStreamRatherThanASeparateFlag(t *testing.T) {
	model, release := streamingModel(t)
	release()

	// Drain the remaining events; the stream is only done once they are read.
	// (streamingModel already consumed the first paint.)
	for model.responding() {
		event := <-model.stream
		update(model, event)
	}
	if model.responding() {
		t.Fatal("a finished response should not report as responding")
	}
	if model.stream != nil || model.cancel != nil {
		t.Fatal("a finished response should clear its channel and cancel func")
	}
}

func TestStreamCommandsAreNotExecutedWhileResponding(t *testing.T) {
	model, release := streamingModel(t)

	update(model, runeKey("/exit"))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})

	if !model.responding() {
		t.Fatal("/exit must not quit while a response is streaming")
	}
	if got := model.editor.Text(); got != "/exit" {
		t.Fatalf("the command should be kept as a draft: %q", got)
	}
	release()
}
