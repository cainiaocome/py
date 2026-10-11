package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTheFirstChunkPaintsImmediately(t *testing.T) {
	fake := newFakeChat("m")
	release := make(chan struct{})
	fake.stream = func(_ context.Context, _ string, _, onText func(string)) (string, error) {
		onText("first ")
		<-release
		onText("second")
		return "first second", nil
	}
	model := newFakeModel(t, fake)
	update(model, runeKey("q"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	cmd = update(model, cmd())

	if got := strings.Join(entryTexts(model), "\n"); !strings.Contains(got, "first ") {
		t.Fatalf("the first chunk should paint without waiting for the throttle: %v", entryTexts(model))
	}

	close(release)
	pump(t, model, cmd)
	if got := strings.Join(entryTexts(model), "\n"); !strings.Contains(got, "first second") {
		t.Fatalf("the finished answer is wrong: %v", entryTexts(model))
	}
}

func TestChunksAreAccumulatedEvenWhenTheThrottleSkipsRepaints(t *testing.T) {
	fake := newFakeChat("m")
	fake.stream = func(_ context.Context, _ string, _, onText func(string)) (string, error) {
		// Arrive far faster than liveRefreshInterval, so most of these do not
		// trigger a repaint; none of them may be lost.
		for _, chunk := range []string{"Hel", "lo, ", "\u4e16", "\u754c", "!"} {
			onText(chunk)
		}
		return "Hello, \u4e16\u754c!", nil
	}
	model := newFakeModel(t, fake)
	update(model, runeKey("q"))
	pump(t, model, update(model, tea.KeyMsg{Type: tea.KeyEnter}))

	rendered := strings.Join(entryTexts(model), "\n")
	if !strings.Contains(rendered, "Hello, \u4e16\u754c!") {
		t.Fatalf("chunks were dropped: %v", entryTexts(model))
	}
	if model.liveChunks != nil {
		t.Fatal("the chunk buffer should be released once the response finishes")
	}
	if model.responding() {
		t.Fatal("the response should be finished")
	}
}

func TestStreamingChunksSurviveAnInterrupt(t *testing.T) {
	fake := newFakeChat("m")
	fake.stream = func(ctx context.Context, _ string, _, onText func(string)) (string, error) {
		onText("partial ")
		<-ctx.Done()
		return "partial answer", ctx.Err()
	}
	model := newFakeModel(t, fake)
	update(model, runeKey("q"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	cmd = update(model, cmd())

	// Ctrl+C cancels, and the partial answer must still be there afterwards.
	update(model, tea.KeyMsg{Type: tea.KeyCtrlC})
	pump(t, model, cmd)

	rendered := strings.Join(entryTexts(model), "\n")
	if !strings.Contains(rendered, "partial answer") {
		t.Fatalf("the partial answer was lost: %v", entryTexts(model))
	}
	if !strings.Contains(rendered, "Response interrupted.") {
		t.Fatalf("the interrupt should be noted: %v", entryTexts(model))
	}
}
