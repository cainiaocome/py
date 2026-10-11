package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEnterSendsAndAltEnterInsertsNewline(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.editor.SetText("hello")
	model.editor.MoveHome()

	update(model, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	if !strings.Contains(model.editor.Text(), "\n") {
		t.Fatalf("Alt+Enter should insert a newline: %q", model.editor.Text())
	}
	if len(model.chat.(*fakeChat).streamed()) != 0 {
		t.Fatal("Alt+Enter must not send")
	}

	before := len(model.entries)
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if len(model.entries) <= before {
		t.Fatal("Enter should echo the prompt")
	}
}

func TestCtrlEMovesToEndInInsertAndIsANoopInNormal(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.editor.SetText("hello")
	model.editor.MoveHome()
	if model.editor.CursorAtEnd() {
		t.Fatal("precondition: cursor should not be at the end")
	}
	update(model, tea.KeyMsg{Type: tea.KeyCtrlE})
	if !model.editor.CursorAtEnd() {
		t.Fatal("Ctrl+E should move to the end while inserting")
	}

	model.editor.MoveHome()
	model.editor.SetMode(ModeNormal)
	update(model, tea.KeyMsg{Type: tea.KeyCtrlE})
	if model.editor.CursorAtEnd() {
		t.Fatal("Ctrl+E should be a no-op in normal mode")
	}
}

func TestCtrlBAndCtrlFPageInInsertAndNormalMode(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.height = 10
	fillTranscript(model, 60)
	model.scrollBack(30)

	for _, mode := range []Mode{ModeInsert, ModeNormal} {
		model.editor.SetMode(mode)
		start := model.scrollOffset
		update(model, tea.KeyMsg{Type: tea.KeyCtrlB})
		if model.scrollOffset != start-model.pageLines() {
			t.Fatalf("%v: Ctrl+B -> %d, want %d", mode, model.scrollOffset, start-model.pageLines())
		}
		update(model, tea.KeyMsg{Type: tea.KeyCtrlF})
		if model.scrollOffset != start {
			t.Fatalf("%v: Ctrl+F -> %d, want %d", mode, model.scrollOffset, start)
		}
	}
}

func TestCtrlDHalfPageDownInInsertAndNormalMode(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.height = 10
	fillTranscript(model, 60)
	model.scrollBack(30)

	for _, mode := range []Mode{ModeInsert, ModeNormal} {
		model.editor.SetMode(mode)
		start := model.scrollOffset
		if isQuit(update(model, tea.KeyMsg{Type: tea.KeyCtrlD})) {
			t.Fatal("Ctrl+D must never exit")
		}
		if model.scrollOffset != start+model.halfPageLines() {
			t.Fatalf("%v: Ctrl+D -> %d, want %d", mode, model.scrollOffset, start+model.halfPageLines())
		}
	}
}

func TestBusyEnterKeepsTheDraft(t *testing.T) {
	fake := newFakeChat("m")
	release := make(chan struct{})
	fake.stream = func(_ context.Context, prompt string, _, onText func(string)) (string, error) {
		onText(prompt + " partial")
		<-release
		return prompt + " partial answer", nil
	}
	model := newFakeModel(t, fake)

	update(model, runeKey("hello"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.state != stateResponding {
		t.Fatalf("state = %v, want responding", model.state)
	}

	update(model, runeKey("draft"))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.editor.Text() != "draft" {
		t.Fatalf("the draft should survive a busy Enter: %q", model.editor.Text())
	}
	if !strings.Contains(model.status, "draft kept") {
		t.Fatalf("status = %q", model.status)
	}

	close(release)
	pump(t, model, cmd)
	if model.state != stateReady {
		t.Fatalf("state = %v, want ready", model.state)
	}
	if !strings.Contains(strings.Join(entryTexts(model), "\n"), "hello partial answer") {
		t.Fatalf("the answer should be retained: %v", entryTexts(model))
	}
}

func TestToolActivityIsInsertedBeforeTheLiveAnswer(t *testing.T) {
	fake := newFakeChat("m")
	fake.stream = func(_ context.Context, _ string, onActivity, onText func(string)) (string, error) {
		onActivity("Searching the web: cats")
		onText("answer")
		return "answer", nil
	}
	model := newFakeModel(t, fake)

	update(model, runeKey("q"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, model, cmd)

	texts := entryTexts(model)
	activity := indexOfText(texts, "Searching the web: cats")
	answer := indexOfText(texts, "answer")
	if activity < 0 || answer < 0 {
		t.Fatalf("entries = %v", texts)
	}
	if activity > answer {
		t.Fatalf("activity should precede the answer: %v", texts)
	}
}

func TestCancelledResponseKeepsPartialTextAndRecovers(t *testing.T) {
	fake := newFakeChat("m")
	started := make(chan struct{})
	fake.stream = func(ctx context.Context, _ string, _, onText func(string)) (string, error) {
		onText("partial")
		close(started)
		<-ctx.Done()
		return "partial", ctx.Err()
	}
	model := newFakeModel(t, fake)

	update(model, runeKey("q"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	cmd = update(model, cmd())
	<-started

	update(model, tea.KeyMsg{Type: tea.KeyCtrlC})
	pump(t, model, cmd)

	if model.state != stateReady {
		t.Fatalf("state = %v, want ready", model.state)
	}
	texts := strings.Join(entryTexts(model), "\n")
	if !strings.Contains(texts, "partial") {
		t.Fatalf("partial text was lost: %v", entryTexts(model))
	}
	if !strings.Contains(texts, "Response interrupted.") {
		t.Fatalf("missing interruption notice: %v", entryTexts(model))
	}
}

func TestFailureBeforeFirstChunkRemovesTheEmptyAnswer(t *testing.T) {
	fake := newFakeChat("m")
	fake.stream = func(context.Context, string, func(string), func(string)) (string, error) {
		return "", errors.New("boom")
	}
	model := newFakeModel(t, fake)

	update(model, runeKey("q"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, model, cmd)

	if model.liveEntry != nil {
		t.Fatal("the empty live answer should be removed")
	}
	texts := strings.Join(entryTexts(model), "\n")
	if !strings.Contains(texts, "Request failed.") {
		t.Fatalf("missing failure notice: %v", entryTexts(model))
	}
	for _, text := range entryTexts(model) {
		if text == "" {
			t.Fatal("an empty entry survived the failure")
		}
	}
}

func TestFailureAfterPartialKeepsThePartialText(t *testing.T) {
	fake := newFakeChat("m")
	fake.stream = func(_ context.Context, _ string, _, onText func(string)) (string, error) {
		onText("half an answer")
		return "half an answer", errors.New("boom")
	}
	model := newFakeModel(t, fake)

	update(model, runeKey("q"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, model, cmd)

	texts := strings.Join(entryTexts(model), "\n")
	if !strings.Contains(texts, "half an answer") {
		t.Fatalf("partial text was lost: %v", entryTexts(model))
	}
	if !strings.Contains(texts, "Request failed.") {
		t.Fatalf("missing failure notice: %v", entryTexts(model))
	}
}

func TestModelPickerListsSelectsAndCancels(t *testing.T) {
	fake := newFakeChat("model-a")
	fake.models = []string{"model-a", "model-b"}
	model := newFakeModel(t, fake)

	update(model, runeKey("/model"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.state != stateModelLoading {
		t.Fatalf("state = %v, want loading", model.state)
	}
	cmd = update(model, cmd())
	if cmd != nil {
		t.Fatal("listing models should be a single command")
	}
	if model.state != stateModelSelect {
		t.Fatalf("state = %v, want select", model.state)
	}
	texts := strings.Join(entryTexts(model), "\n")
	if !strings.Contains(texts, "1. model-a (current)") || !strings.Contains(texts, "2. model-b") {
		t.Fatalf("model list missing: %v", entryTexts(model))
	}

	update(model, runeKey("2"))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if fake.ModelName() != "model-b" {
		t.Fatalf("numeric selection -> %q", fake.ModelName())
	}

	// Re-open and cancel with an empty line.
	update(model, runeKey("/model"))
	cmd = update(model, tea.KeyMsg{Type: tea.KeyEnter})
	update(model, cmd())
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.state != stateReady {
		t.Fatalf("cancel state = %v", model.state)
	}
	if fake.ModelName() != "model-b" {
		t.Fatalf("cancel changed the model: %q", fake.ModelName())
	}
}

func TestModelPickerCtrlCCancelsWithoutExiting(t *testing.T) {
	fake := newFakeChat("m")
	model := newFakeModel(t, fake)

	update(model, runeKey("/model"))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if isQuit(update(model, tea.KeyMsg{Type: tea.KeyCtrlC})) {
		t.Fatal("Ctrl+C should cancel the picker, not exit")
	}
	if model.state != stateReady {
		t.Fatalf("state = %v, want ready", model.state)
	}
	if !strings.Contains(strings.Join(entryTexts(model), "\n"), "Model selection cancelled.") {
		t.Fatalf("missing cancel notice: %v", entryTexts(model))
	}
}

func TestModelListFailureRecovers(t *testing.T) {
	fake := newFakeChat("m")
	fake.listErr = errors.New("offline")
	model := newFakeModel(t, fake)

	update(model, runeKey("/model"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	update(model, cmd())
	if model.state != stateReady {
		t.Fatalf("state = %v, want ready", model.state)
	}
	if !strings.Contains(strings.Join(entryTexts(model), "\n"), "Could not select a model.") {
		t.Fatalf("missing failure notice: %v", entryTexts(model))
	}
}

func TestEnableWebIsIdempotentAndPersists(t *testing.T) {
	fake := newFakeChat("model-a")
	model := newFakeModel(t, fake)

	update(model, runeKey(EnableWebCommand))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !fake.WebEnabled() {
		t.Fatal("web should be enabled")
	}
	update(model, runeKey(EnableWebCommand))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(strings.Join(entryTexts(model), "\n"), "already enabled") {
		t.Fatalf("missing idempotent notice: %v", entryTexts(model))
	}

	update(model, runeKey("/model model-b"))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !fake.WebEnabled() {
		t.Fatal("a model switch should keep web tools")
	}
}

func TestClearResetsTheConversationAndTheTranscript(t *testing.T) {
	fake := newFakeChat("m")
	model := newFakeModel(t, fake)

	update(model, runeKey("hi"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, model, cmd)

	update(model, runeKey("/clear"))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if fake.clearCount() != 1 {
		t.Fatalf("clear count = %d, want 1", fake.clearCount())
	}
	if len(model.entries) != 0 {
		t.Fatalf("entries = %d, want 0", len(model.entries))
	}
}

func TestUnknownCommandNotice(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	update(model, runeKey("/nope"))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	last := model.entries[len(model.entries)-1]
	if !strings.Contains(last.text, "Unknown command. Available:") {
		t.Fatalf("last entry = %q", last.text)
	}
}

func TestPastedControlCharactersAreEscapedInTheTranscript(t *testing.T) {
	fake := newFakeChat("m")
	model := newFakeModel(t, fake)

	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[31m\t"), Paste: true}
	update(model, paste)
	update(model, tea.KeyMsg{Type: tea.KeyEnter})

	texts := strings.Join(entryTexts(model), "\n")
	if strings.Contains(texts, "\x1b[31m") {
		t.Fatalf("a raw escape sequence reached the transcript: %q", texts)
	}
	if !strings.Contains(texts, `\x1b`) || !strings.Contains(texts, `\t`) {
		t.Fatalf("control characters should be visible: %q", texts)
	}
}

func TestTranscriptLinesAreCachedUntilTheyAreDirtied(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	fillTranscript(model, 3)

	first := model.transcriptLines(80)
	second := model.transcriptLines(80)
	if fmt.Sprintf("%p", first) != fmt.Sprintf("%p", second) {
		t.Fatal("unchanged transcript lines should be served from the cache")
	}
	model.appendText("more")
	third := model.transcriptLines(80)
	if len(third) <= len(first) {
		t.Fatalf("appending an entry should rebuild the cache: %d -> %d", len(first), len(third))
	}
}

func TestEntryRenderCacheIsKeyedByWidthAndRevision(t *testing.T) {
	entry := newTextEntry("hello world")
	first := entry.lines(80)
	if &first[0] != &entry.lines(80)[0] {
		t.Fatal("the same width should reuse the cache")
	}
	entry.setText("changed")
	if &first[0] == &entry.lines(80)[0] {
		t.Fatal("a revision should invalidate the cache")
	}
	entry.lines(40)
	if &first[0] == &entry.lines(80)[0] {
		t.Fatal("a width change should invalidate the cache")
	}
}

func TestResizeReflowsAndKeepsTheFooterAtTheBottom(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	fillTranscript(model, 0)
	model.appendText(strings.Repeat("word ", 60))

	broad := model.transcriptLines(80)
	update(model, tea.WindowSizeMsg{Width: 40, Height: 24})
	narrow := model.transcriptLines(40)
	if len(narrow) <= len(broad) {
		t.Fatalf("a narrow terminal should reflow into more lines: %d -> %d", len(broad), len(narrow))
	}
	lines := strings.Split(model.View(), "\n")
	if footer := lines[len(lines)-1]; !strings.Contains(footer, "INSERT") {
		t.Fatalf("the footer should stay at the bottom: %q", footer)
	}
}

func TestScrollMathUsesTheLayoutHeight(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.height = 10
	model.editor.SetText("")
	// rows - (1 divider + editor + 1 footer) = 10 - (1 + 3 + 1)
	if got := model.viewportHeight(); got != 5 {
		t.Fatalf("viewportHeight = %d, want 5", got)
	}
	if got := model.pageLines(); got != 3 {
		t.Fatalf("pageLines = %d, want 3", got)
	}
	if got := model.halfPageLines(); got != 2 {
		t.Fatalf("halfPageLines = %d, want 2", got)
	}

	// A taller editor shrinks the viewport.
	model.editor.SetText(strings.Repeat("x", 400))
	if got := model.editorHeight(80); got != 4 {
		t.Fatalf("editorHeight = %d, want 4", got)
	}
	if got := model.viewportHeight(); got != 4 {
		t.Fatalf("viewportHeight with a tall editor = %d, want 4", got)
	}
}

func TestStartCommandPinsTheLastQuestion(t *testing.T) {
	fake := newFakeChat("m")
	model := newFakeModel(t, fake)
	model.height = 10
	fillTranscript(model, 0)
	var users []*Entry
	for i := 0; i < 3; i++ {
		model.appendText("filler")
		entry := newTextEntry("You > q")
		model.entries = append(model.entries, entry)
		users = append(users, entry)
		model.appendText("answer")
	}
	for i := 0; i < 30; i++ {
		model.appendText("tail")
	}
	model.userEntries = users

	update(model, runeKey("/start"))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})

	anchors := model.messageAnchors()
	if model.followTail {
		t.Fatal("/start should pin the last question above the tail")
	}
	if model.scrollOffset != anchors[len(anchors)-1].offset {
		t.Fatalf("pinned = %d, want %d", model.scrollOffset, anchors[len(anchors)-1].offset)
	}
}

func TestPrintTranscriptWritesTheFinalTranscript(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	fillTranscript(model, 0)
	model.appendText("kept line")

	var out strings.Builder
	model.PrintTranscript(&out)
	if !strings.Contains(out.String(), "kept line") {
		t.Fatalf("transcript = %q", out.String())
	}
}

func TestModelSelectionErrors(t *testing.T) {
	fake := newFakeChat("model-a")
	fake.models = []string{"model-a"}
	model := newFakeModel(t, fake)

	update(model, runeKey("/model 9"))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(strings.Join(entryTexts(model), "\n"), "Invalid model number. Model unchanged.") {
		t.Fatalf("entries = %v", entryTexts(model))
	}

	update(model, runeKey("/model a b"))
	update(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(strings.Join(entryTexts(model), "\n"), "Could not select a model.") {
		t.Fatalf("entries = %v", entryTexts(model))
	}
	if fake.ModelName() != "model-a" {
		t.Fatalf("rejected names changed the model: %q", fake.ModelName())
	}
}

func TestMouseWheelDownAndIgnoredActions(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.height = 10
	fillTranscript(model, 60)
	model.scrollBack(30)
	start := model.scrollOffset

	update(model, tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonWheelDown})
	if model.scrollOffset != start {
		t.Fatal("non-press mouse actions should be ignored")
	}
	update(model, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if model.scrollOffset != start+3 {
		t.Fatalf("wheel down -> %d, want %d", model.scrollOffset, start+3)
	}
}

func TestFollowTailNowResumesAndPinningPastTheEnd(t *testing.T) {
	model := newFakeModel(t, newFakeChat("m"))
	model.height = 10
	fillTranscript(model, 60)
	model.scrollBack(30)

	model.followTailNow()
	if !model.followTail || model.scrollOffset != 0 {
		t.Fatalf("followTailNow -> follow=%v offset=%d", model.followTail, model.scrollOffset)
	}
	if !strings.Contains(model.status, "Following") {
		t.Fatalf("status = %q", model.status)
	}

	model.scrollBack(1)
	model.pinOffset(1_000_000)
	if !model.followTail {
		t.Fatal("pinning past the end should resume following")
	}
}

func TestEffectiveWidthAndViewportDefaults(t *testing.T) {
	model := New(newFakeChat("m"))
	if model.Init() != nil {
		t.Fatal("Init should be a no-op")
	}
	if got := model.effectiveWidth(); got != 80 {
		t.Fatalf("effectiveWidth = %d, want 80", got)
	}
	if got := model.viewportHeight(); got != 19 {
		t.Fatalf("viewportHeight = %d, want 19", got)
	}
	if got := model.topOffset(10, 0); got != 10 {
		t.Fatalf("topOffset with an empty viewport = %d, want 10", got)
	}
}

func TestManualScrollIsPreservedWhileAResponseCompletes(t *testing.T) {
	fake := newFakeChat("m")
	release := make(chan struct{})
	fake.stream = func(_ context.Context, prompt string, _, onText func(string)) (string, error) {
		if prompt == "first" {
			onText("partial answer")
			<-release
			return "partial answer with NEW_TOKENS", nil
		}
		return "second answer", nil
	}
	model := newFakeModel(t, fake)
	model.height = 14
	fillTranscript(model, 40)

	update(model, runeKey("first"))
	cmd := update(model, tea.KeyMsg{Type: tea.KeyEnter})
	cmd = update(model, cmd()) // first paint

	model.scrollBack(5)
	if model.followTail {
		t.Fatal("scrolling should pause following")
	}
	paused := model.scrollOffset

	close(release)
	pump(t, model, cmd)

	if model.followTail {
		t.Fatal("completing a response should not resume following while paused")
	}
	if model.scrollOffset != paused {
		t.Fatalf("the paused offset changed: %d -> %d", paused, model.scrollOffset)
	}
	if !strings.Contains(strings.Join(entryTexts(model), "\n"), "NEW_TOKENS") {
		t.Fatalf("new tokens were dropped: %v", entryTexts(model))
	}

	update(model, runeKey("next"))
	cmd = update(model, tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, model, cmd)
	if !model.followTail {
		t.Fatal("a follow-up prompt should resume following the tail")
	}
}
