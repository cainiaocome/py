package repl

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeChat is a scriptable session for the line interface.
type fakeChat struct {
	model   string
	web     bool
	models  []string
	listErr error
	stream  func(ctx context.Context, prompt string, onActivity, onText func(string)) (string, error)
	cleared int
}

func (f *fakeChat) ModelName() string { return f.model }
func (f *fakeChat) WebEnabled() bool  { return f.web }

func (f *fakeChat) EnableWeb() bool {
	if f.web {
		return false
	}
	f.web = true
	return true
}

func (f *fakeChat) Clear() { f.cleared++ }

func (f *fakeChat) SwitchModel(model string) error {
	model = strings.TrimSpace(model)
	if model == "" {
		return errors.New("invalid model")
	}
	f.model = model
	return nil
}

func (f *fakeChat) ListModels(context.Context) ([]string, error) { return f.models, f.listErr }

func (f *fakeChat) Stream(ctx context.Context, prompt string, onActivity, onText func(string)) (string, error) {
	if f.stream != nil {
		return f.stream(ctx, prompt, onActivity, onText)
	}
	return "answer to " + prompt, nil
}

func runRepl(t *testing.T, chat *fakeChat, input string) string {
	t.Helper()
	var out strings.Builder
	if err := Run(chat, strings.NewReader(input), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return out.String()
}

func TestCommandsAndRecovery(t *testing.T) {
	chat := &fakeChat{model: "m"}
	chat.stream = func(_ context.Context, prompt string, _, _ func(string)) (string, error) {
		if prompt == "boom" {
			return "", errors.New("nope")
		}
		return "answer to " + prompt, nil
	}
	out := runRepl(t, chat, "hello\n/clear\nboom\nafter\n/exit\n")

	for _, want := range []string{
		"Assistant [m]",
		"answer to hello",
		"Conversation cleared.",
		"Request failed. Check your connection, API key and model; try again.",
		"answer to after",
		"Goodbye.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	if chat.cleared != 1 {
		t.Fatalf("cleared = %d, want 1", chat.cleared)
	}
}

func TestStreamFailureNoticeAndToolActivity(t *testing.T) {
	chat := &fakeChat{model: "m"}
	chat.stream = func(_ context.Context, _ string, onActivity, _ func(string)) (string, error) {
		onActivity("Searching the web: cats")
		return "found it", nil
	}
	out := runRepl(t, chat, "search\n/exit\n")
	if !strings.Contains(out, "Searching the web: cats") || !strings.Contains(out, "found it") {
		t.Fatalf("output = %s", out)
	}
}

func TestUnknownCommand(t *testing.T) {
	out := runRepl(t, &fakeChat{model: "m"}, "/nope\n/exit\n")
	if !strings.Contains(out, "Unknown command. Available: /start, /clear, /exit, /model, /enable-web-search-and-web-fetch") {
		t.Fatalf("output = %s", out)
	}
}

func TestStartCommandWithoutFullScreenUI(t *testing.T) {
	out := runRepl(t, &fakeChat{model: "m"}, "/start\n/exit\n")
	if !strings.Contains(out, "/start works in the full-screen interface.") {
		t.Fatalf("output = %s", out)
	}
}

func TestEnableWebIsIdempotent(t *testing.T) {
	chat := &fakeChat{model: "m"}
	out := runRepl(t, chat, "/enable-web-search-and-web-fetch\n/enable-web-search-and-web-fetch\n/exit\n")
	if !chat.WebEnabled() {
		t.Fatal("web should be enabled")
	}
	if !strings.Contains(out, "enabled for this session.") {
		t.Fatalf("missing enable notice: %s", out)
	}
	if !strings.Contains(out, "already enabled.") {
		t.Fatalf("missing idempotent notice: %s", out)
	}
}

func TestModelSwitchByName(t *testing.T) {
	chat := &fakeChat{model: "m"}
	out := runRepl(t, chat, "/model gpt-oss:20b\n/exit\n")
	if chat.ModelName() != "gpt-oss:20b" {
		t.Fatalf("model = %q", chat.ModelName())
	}
	if !strings.Contains(out, "Model: gpt-oss:20b · conversation retained") {
		t.Fatalf("output = %s", out)
	}
}

func TestModelListAndNumericSelection(t *testing.T) {
	chat := &fakeChat{model: "model-a", models: []string{"model-a", "model-b"}}
	out := runRepl(t, chat, "/model\n2\n/exit\n")
	if chat.ModelName() != "model-b" {
		t.Fatalf("model = %q", chat.ModelName())
	}
	if !strings.Contains(out, "1. model-a (current)") || !strings.Contains(out, "2. model-b") {
		t.Fatalf("output = %s", out)
	}
	if !strings.Contains(out, "Model: model-b · conversation retained") {
		t.Fatalf("output = %s", out)
	}
}

func TestModelListCancelAndInvalidNumber(t *testing.T) {
	chat := &fakeChat{model: "model-a", models: []string{"model-a", "model-b"}}
	out := runRepl(t, chat, "/model\n9\n/exit\n")
	if chat.ModelName() != "model-a" {
		t.Fatalf("invalid number changed the model: %q", chat.ModelName())
	}
	if !strings.Contains(out, "Invalid model number. Model unchanged.") {
		t.Fatalf("output = %s", out)
	}

	out = runRepl(t, chat, "/model\n\n/exit\n")
	if chat.ModelName() != "model-a" {
		t.Fatalf("cancel changed the model: %q", chat.ModelName())
	}
}

func TestModelListFailure(t *testing.T) {
	chat := &fakeChat{model: "m", listErr: errors.New("offline")}
	out := runRepl(t, chat, "/model\n/exit\n")
	if !strings.Contains(out, "Could not select a model. Try /model <name> or check your connection and key.") {
		t.Fatalf("output = %s", out)
	}
}

func TestEOFPrintsGoodbye(t *testing.T) {
	out := runRepl(t, &fakeChat{model: "m"}, "hello\n")
	if !strings.Contains(out, "Goodbye.") {
		t.Fatalf("EOF should print Goodbye: %s", out)
	}
}
