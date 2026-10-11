package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cainiaocome/py/internal/chat"
)

func newTestModel(t *testing.T) *Model {
	t.Helper()
	model := New(chat.New("test-key", "test-model"))
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return model
}

func update(model *Model, msg tea.Msg) tea.Cmd {
	_, cmd := model.Update(msg)
	return cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func runeKey(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

func TestCtrlDNeverExits(t *testing.T) {
	model := newTestModel(t)
	model.editor.SetText("draft")

	if isQuit(update(model, tea.KeyMsg{Type: tea.KeyCtrlD})) {
		t.Fatal("Ctrl+D must never exit")
	}
}

func TestCtrlCExitsWhenReady(t *testing.T) {
	model := newTestModel(t)
	if !isQuit(update(model, tea.KeyMsg{Type: tea.KeyCtrlC})) {
		t.Fatal("Ctrl+C should exit while waiting for input")
	}
}

func TestExitCommandQuits(t *testing.T) {
	model := newTestModel(t)
	model.editor.SetText("/exit")
	if !isQuit(update(model, tea.KeyMsg{Type: tea.KeyEnter})) {
		t.Fatal("/exit should quit")
	}
}

func TestTypingAndModes(t *testing.T) {
	model := newTestModel(t)
	model.editor.SetText("")
	update(model, runeKey("hello"))
	if model.editor.Text() != "hello" {
		t.Fatalf("text = %q", model.editor.Text())
	}
	if !strings.Contains(model.View(), "INSERT") {
		t.Fatal("footer should show INSERT")
	}

	update(model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.editor.Mode() != ModeNormal {
		t.Fatal("Escape should enter normal mode")
	}
	if !strings.Contains(model.View(), "NORMAL") {
		t.Fatal("footer should show NORMAL")
	}
}

func TestCursorShapesMatchEditorMode(t *testing.T) {
	// Insert is a beam: a thin bar at the insertion point, never a block.
	if got := applyCursor("abc", 1, ModeInsert); got != "a"+beamCursor+"c" {
		t.Fatalf("insert cursor = %q", got)
	}
	if got := applyCursor("abc", 3, ModeInsert); got != "abc"+beamCursor {
		t.Fatalf("insert cursor at end = %q", got)
	}
	// Normal is a reverse-video block over the character under the cursor.
	if got := applyCursor("abc", 1, ModeNormal); !strings.Contains(got, "\x1b[7mb") {
		t.Fatalf("normal cursor = %q", got)
	}
	// Replace is an underline.
	if got := applyCursor("abc", 1, ModeReplace); !strings.Contains(got, "\x1b[4mb") {
		t.Fatalf("replace cursor = %q", got)
	}
}

func TestEditorDrawsModalCursor(t *testing.T) {
	model := newTestModel(t)
	model.editor.SetText("hi")

	insert := strings.Join(model.renderEditor(80, 4), "\n")
	if !strings.Contains(insert, beamCursor) {
		t.Fatalf("insert editor should draw a beam: %q", insert)
	}

	update(model, tea.KeyMsg{Type: tea.KeyEsc})
	normal := strings.Join(model.renderEditor(80, 4), "\n")
	if strings.Contains(normal, beamCursor) {
		t.Fatalf("normal editor should not draw a beam: %q", normal)
	}
	if !strings.Contains(normal, "\x1b[7m") {
		t.Fatalf("normal editor should draw a block: %q", normal)
	}
}

func TestCommandCompletionCycles(t *testing.T) {
	model := newTestModel(t)
	model.editor.SetText("/")
	model.refreshCompletions()
	if len(model.completions) != len(Commands) {
		t.Fatalf("completions = %v", model.completions)
	}

	model.cycleCompletions(1)
	if model.editor.Text() != "/start" {
		t.Fatalf("after down = %q", model.editor.Text())
	}
	model.cycleCompletions(-1)
	if model.editor.Text() != Commands[len(Commands)-1] {
		t.Fatalf("after up = %q", model.editor.Text())
	}

	model.editor.SetText("/cl")
	model.refreshCompletions()
	update(model, tea.KeyMsg{Type: tea.KeyTab})
	if model.editor.Text() != "/clear" {
		t.Fatalf("tab completion = %q", model.editor.Text())
	}
}

func TestCompletionsHideWithNewline(t *testing.T) {
	model := newTestModel(t)
	model.editor.SetText("/")
	model.refreshCompletions()
	if len(model.completions) == 0 {
		t.Fatal("expected completions")
	}
	model.editor.InsertText("\n")
	model.refreshCompletions()
	if len(model.completions) != 0 {
		t.Fatal("completions should hide after a newline")
	}
}

func TestModelSwitchByName(t *testing.T) {
	model := newTestModel(t)
	model.editor.SetText("/model gpt-oss:20b")
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.chat.ModelName() != "gpt-oss:20b" {
		t.Fatalf("model = %q", model.chat.ModelName())
	}
	if !strings.Contains(model.View(), "You [gpt-oss:20b] >") {
		t.Fatal("prompt should reflect the new model")
	}
}

func TestScrollKeys(t *testing.T) {
	model := newTestModel(t)
	model.height = 10
	model.entries = nil
	for i := 0; i < 40; i++ {
		model.appendText("line")
	}
	model.scrollBack(10)
	start := model.scrollOffset
	if model.followTail {
		t.Fatal("scrollBack should pause following")
	}

	model.editor.SetMode(ModeNormal)
	update(model, runeKey("j"))
	if model.scrollOffset != start+1 {
		t.Fatalf("j -> %d, want %d", model.scrollOffset, start+1)
	}
	update(model, runeKey("k"))
	if model.scrollOffset != start {
		t.Fatalf("k -> %d, want %d", model.scrollOffset, start)
	}
	half := model.halfPageLines()
	update(model, runeKey("d"))
	if model.scrollOffset != start+half {
		t.Fatalf("d -> %d, want %d", model.scrollOffset, start+half)
	}
}

func TestCtrlDScrollsWhenPaused(t *testing.T) {
	model := newTestModel(t)
	model.height = 10
	model.entries = nil
	for i := 0; i < 40; i++ {
		model.appendText("line")
	}
	model.scrollBack(10)
	start := model.scrollOffset

	update(model, tea.KeyMsg{Type: tea.KeyCtrlD})
	if model.scrollOffset <= start {
		t.Fatalf("Ctrl+D should scroll down: %d -> %d", start, model.scrollOffset)
	}
}

func TestMessageNavigationPins(t *testing.T) {
	model := newTestModel(t)
	model.height = 8
	model.entries = nil
	var users []*Entry
	for i := 0; i < 4; i++ {
		model.appendText("filler")
		entry := newTextEntry("You > question")
		model.entries = append(model.entries, entry)
		users = append(users, entry)
		model.appendText("answer")
	}
	for i := 0; i < 20; i++ {
		model.appendText("tail")
	}
	model.userEntries = users

	anchors := model.messageAnchors()
	if len(anchors) != 4 {
		t.Fatalf("anchors = %d, want 4", len(anchors))
	}
	for i := 1; i < len(anchors); i++ {
		if anchors[i].offset <= anchors[i-1].offset {
			t.Fatal("anchors should increase")
		}
	}

	model.scrollToLastMessage()
	if model.followTail {
		t.Fatal("last message should pin above the tail")
	}
	if model.scrollOffset != anchors[3].offset {
		t.Fatalf("pinned = %d, want %d", model.scrollOffset, anchors[3].offset)
	}

	model.previousMessage()
	if model.scrollOffset != anchors[2].offset {
		t.Fatalf("previous = %d, want %d", model.scrollOffset, anchors[2].offset)
	}
	model.nextMessage()
	if model.scrollOffset != anchors[3].offset {
		t.Fatalf("next = %d, want %d", model.scrollOffset, anchors[3].offset)
	}
}

func TestStartWithNoMessages(t *testing.T) {
	model := newTestModel(t)
	model.entries = nil
	model.editor.SetText("/start")
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	last := model.entries[len(model.entries)-1]
	if last.text != "No previous message to scroll to." {
		t.Fatalf("last entry = %q", last.text)
	}
}

func TestClearTranscript(t *testing.T) {
	model := newTestModel(t)
	model.appendText("something")
	entry := newTextEntry("You > hi")
	model.entries = append(model.entries, entry)
	model.userEntries = append(model.userEntries, entry)

	model.editor.SetText("/clear")
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if len(model.entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(model.entries))
	}
	if len(model.userEntries) != 0 {
		t.Fatal("user entries should be cleared")
	}
}

func TestCtrlUEmptyScrollsNonEmptyClears(t *testing.T) {
	model := newTestModel(t)
	model.height = 10
	model.entries = nil
	for i := 0; i < 40; i++ {
		model.appendText("line")
	}
	model.scrollBack(10)
	start := model.scrollOffset

	// Empty draft: Ctrl+U scrolls half a page up.
	update(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	if model.scrollOffset >= start {
		t.Fatalf("Ctrl+U on empty should scroll up: %d -> %d", start, model.scrollOffset)
	}
	// Non-empty draft: Ctrl+U clears to the line start instead.
	model.editor.SetText("abc")
	before := model.scrollOffset
	update(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	if model.editor.Text() != "" {
		t.Fatalf("Ctrl+U should clear the line, got %q", model.editor.Text())
	}
	if model.scrollOffset != before {
		t.Fatal("Ctrl+U with text should not scroll")
	}
}

func TestMouseWheelScrolls(t *testing.T) {
	model := newTestModel(t)
	model.height = 10
	model.entries = nil
	for i := 0; i < 40; i++ {
		model.appendText("line")
	}
	model.scrollBack(10)
	start := model.scrollOffset
	update(model, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if model.scrollOffset != start-3 {
		t.Fatalf("wheel up -> %d, want %d", model.scrollOffset, start-3)
	}
}
