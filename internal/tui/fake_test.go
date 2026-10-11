package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeChat is a scriptable Session so full flows can be driven without a
// network, mirroring the Python suite's FakeChat.
type fakeChat struct {
	model   string
	web     bool
	models  []string
	listErr error
	stream  func(ctx context.Context, prompt string, onActivity, onText func(string)) (string, error)

	mu      sync.Mutex
	prompts []string
	clears  int
}

func newFakeChat(model string) *fakeChat { return &fakeChat{model: model} }

func (f *fakeChat) ModelName() string { return f.model }
func (f *fakeChat) WebEnabled() bool  { return f.web }

func (f *fakeChat) EnableWeb() bool {
	if f.web {
		return false
	}
	f.web = true
	return true
}

func (f *fakeChat) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clears++
}

func (f *fakeChat) SwitchModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" || strings.ContainsFunc(model, unicode.IsSpace) {
		return errors.New("invalid model")
	}
	f.model = model
	return nil
}

func (f *fakeChat) ListModels(context.Context) ([]string, error) { return f.models, f.listErr }

func (f *fakeChat) Stream(ctx context.Context, prompt string, onActivity, onText func(string)) (string, error) {
	f.mu.Lock()
	f.prompts = append(f.prompts, prompt)
	f.mu.Unlock()
	if f.stream != nil {
		return f.stream(ctx, prompt, onActivity, onText)
	}
	return "", nil
}

func (f *fakeChat) streamed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.prompts...)
}

func (f *fakeChat) clearCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clears
}

func newFakeModel(t *testing.T, fake *fakeChat) *Model {
	t.Helper()
	model := New(fake)
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return model
}

// pump executes stream commands until the response finishes.
func pump(t *testing.T, model *Model, cmd tea.Cmd) {
	t.Helper()
	for steps := 0; cmd != nil; steps++ {
		if steps > 10_000 {
			t.Fatal("stream did not finish")
		}
		cmd = update(model, cmd())
	}
}

// entryTexts returns the plain text of every retained transcript entry.
func entryTexts(model *Model) []string {
	var texts []string
	for _, entry := range model.entries {
		texts = append(texts, entry.text)
	}
	return texts
}

func indexOfText(texts []string, needle string) int {
	for index, text := range texts {
		if text == needle {
			return index
		}
	}
	return -1
}

func fillTranscript(model *Model, lines int) {
	model.entries = nil
	for i := 0; i < lines; i++ {
		model.appendText("line")
	}
}
