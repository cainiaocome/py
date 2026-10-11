package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cainiaocome/py/internal/command"
	"github.com/cainiaocome/py/internal/textutil"
)

// liveRefreshInterval throttles re-rendering the growing live answer. The first
// chunk always paints and the finished text is always painted once.
const liveRefreshInterval = 50 * time.Millisecond

// overlay is the modal thing that currently owns the input, if anything.
//
// Whether a response is streaming is deliberately *not* part of this: the user
// keeps typing, keeps seeing command completions, and keeps a draft while the
// model answers, so streaming and overlays are independent. Combining them into
// one enum is what made the command palette disappear during a response.
type overlay int

const (
	overlayNone overlay = iota
	overlayModelsLoading
	overlayModelPicker
)

type streamKind int

const (
	eventDelta streamKind = iota
	eventActivity
	eventDone
)

type streamEvent struct {
	kind streamKind
	text string
	err  error
}

type modelsEvent struct {
	models []string
	err    error
}

type anchor struct {
	entry  *Entry
	offset int
}

// Session is the conversation the UI drives. *chat.Chat implements it; tests
// substitute a fake so the model can be exercised without a network.
type Session interface {
	ModelName() string
	WebEnabled() bool
	ListModels(ctx context.Context) ([]string, error)
	SwitchModel(model string) error
	EnableWeb() bool
	Clear()
	// Stream runs one turn. onActivity receives a one-line tool status and
	// onText receives each fragment of the answer as it arrives; concatenating
	// those fragments yields the returned text. Both may be nil.
	Stream(ctx context.Context, prompt string, onActivity, onText func(string)) (string, error)
}

// Model is the full-screen Bubble Tea application.
type Model struct {
	chat   Session
	editor *Editor

	entries     []*Entry
	userEntries []*Entry
	lastEcho    *Entry
	liveEntry   *Entry

	width   int
	height  int
	overlay overlay
	status  string

	// followTail records the one thing the viewport cannot know: whether new
	// output should pull the window down with it, rather than the window merely
	// happening to sit at the bottom right now.
	followTail bool

	// viewport owns the transcript window: its position, its height and the
	// mouse wheel. syncViewport mirrors the model into it.
	viewport   viewport.Model
	viewSynced uint64
	viewWidth  int

	// transcript is a memoised render of entries. It is keyed by the revision
	// below and the width it was rendered at, so nothing can go stale by
	// forgetting to set a flag: every mutation bumps the revision through
	// touch().
	revision         uint64
	renderedRevision uint64
	renderedWidth    int
	renderedLines    []string

	completions     []string
	completionIndex int
	lastModels      []string

	// liveChunks accumulates the answer while it streams. Only the joined text
	// is handed to the transcript, so a long answer is copied once per repaint
	// instead of once per token.
	liveChunks []string

	stream    chan streamEvent
	cancel    context.CancelFunc
	lastPaint time.Time

	gPending bool
	gTime    time.Time

	// cursorAnchor is the terminal cell the real cursor should rest on. The
	// output writer created in Run reads it so the IME candidate window follows
	// the editor caret.
	cursorAnchor *CursorAnchor
}

// New builds the model and seeds the transcript banner.
func New(c Session) *Model {
	m := &Model{
		chat:            c,
		editor:          NewEditor(),
		overlay:         overlayNone,
		status:          "Ready",
		followTail:      true,
		completionIndex: -1,
		cursorAnchor:    NewCursorAnchor(),
		viewport:        viewport.New(0, 0),
	}
	m.appendText("AI Chat · Ollama Cloud · Model: " + c.ModelName())
	m.appendText("Vim editing · Enter: send · Alt+Enter: newline · Tab: complete · " + command.Hint())
	m.appendText("Web tools: disabled · " + command.EnableWeb)
	m.viewport.MouseWheelEnabled = true
	m.syncViewport()
	return m
}

// commandDescriptions is the palette meta text for each command, resolved once
// rather than on every frame.
var commandDescriptions = command.Descriptions()

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch message := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		m.touch()
	case tea.MouseMsg:
		cmd = m.handleMouse(message)
	case streamEvent:
		cmd = m.handleStream(message)
	case modelsEvent:
		cmd = m.handleModels(message)
	case tea.KeyMsg:
		cmd = m.handleKey(message)
	}
	// The viewport mirrors the model, so it is brought up to date in one place,
	// once per message. View itself stays free of bookkeeping.
	m.syncViewport()
	return m, cmd
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress {
		return nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.scrollBack(m.viewport.MouseWheelDelta)
	case tea.MouseButtonWheelDown:
		m.scrollForward(m.viewport.MouseWheelDelta)
	}
	return nil
}

func (m *Model) handleStream(event streamEvent) tea.Cmd {
	switch event.kind {
	case eventActivity:
		entry := newStyledEntry(event.text, styleDim)
		if index := m.indexOf(m.liveEntry); index < 0 {
			m.entries = append(m.entries, entry)
		} else {
			m.entries = insertAt(m.entries, index, entry)
		}
		m.touch()
		return nextStream(m.stream)
	case eventDelta:
		if m.liveEntry != nil {
			m.liveChunks = append(m.liveChunks, event.text)
			now := time.Now()
			if m.lastPaint.IsZero() || now.Sub(m.lastPaint) >= liveRefreshInterval {
				m.liveEntry.setText(strings.Join(m.liveChunks, ""))
				m.touch()
				m.lastPaint = now
			}
		}
		return nextStream(m.stream)
	case eventDone:
		if m.liveEntry != nil {
			if event.text != "" {
				m.liveEntry.setText(event.text)
			} else if index := m.indexOf(m.liveEntry); index >= 0 {
				m.entries = append(m.entries[:index], m.entries[index+1:]...)
			}
			m.liveEntry = nil
			m.touch()
		}
		// The returned text is authoritative, so anything still buffered here is
		// already accounted for.
		m.liveChunks = nil
		m.stream = nil
		m.cancel = nil
		m.status = "Ready"
		if event.err != nil {
			if errors.Is(event.err, context.Canceled) {
				m.appendStyled("Response interrupted.", styleYellow)
			} else {
				m.appendStyled("Request failed. Check your connection, API key and model; try again.", styleRed)
			}
		}
	}
	return nil
}

func (m *Model) handleModels(event modelsEvent) tea.Cmd {
	if m.overlay != overlayModelsLoading {
		return nil
	}
	if event.err != nil {
		m.appendStyled("Could not select a model. Try /model <name> or check your connection and key.", styleRed)
		m.overlay = overlayNone
		m.status = "Ready"
		return nil
	}
	for index, name := range event.models {
		marker := ""
		if name == m.chat.ModelName() {
			marker = " (current)"
		}
		m.appendText(fmt.Sprintf("  %d. %s%s", index+1, name, marker))
	}
	if len(event.models) == 0 {
		m.appendStyled("No models returned. Enter a model name manually.", styleDim)
	}
	m.overlay = overlayModelPicker
	m.status = "Ready"
	m.lastModels = event.models
	return nil
}

func (m *Model) submit() tea.Cmd {
	if m.responding() {
		m.status = "Responding · draft kept; Ctrl+C interrupts"
		return nil
	}
	if m.overlay == overlayModelsLoading {
		return nil
	}
	raw := m.editor.Text()
	value := strings.TrimSpace(raw)

	echo := newTextEntry(m.promptLabel() + textutil.SafeLiteral(raw))
	m.entries = append(m.entries, echo)
	m.touch()
	m.lastEcho = echo
	m.editor.Clear()
	m.refreshCompletions()
	m.gPending = false
	m.followTail = true
	m.status = "Submitted"

	if m.overlay == overlayModelPicker {
		m.overlay = overlayNone
		if value == "" {
			return nil
		}
		return m.applyModelSwitch(value)
	}
	if value == "" {
		return nil
	}
	spec, argument, known := command.Parse(value)
	if !known {
		if command.IsCommand(value) {
			m.appendStyled(command.UnknownNotice(), styleYellow)
			return nil
		}
		return m.startResponse(value)
	}
	switch spec.Name {
	case "/exit":
		return tea.Quit
	case command.EnableWeb:
		if m.chat.EnableWeb() {
			m.appendStyled("Web search and web fetch enabled for this session.", styleDim)
		} else {
			m.appendStyled("Web search and web fetch are already enabled.", styleDim)
		}
		return nil
	case "/clear":
		m.clearTranscript()
		return nil
	case "/start":
		m.scrollToLastMessage()
		return nil
	case "/model":
		return m.selectModel(argument)
	}
	return nil
}

func (m *Model) selectModel(argument string) tea.Cmd {
	if argument != "" {
		return m.applyModelSwitch(argument)
	}
	m.appendText("Current model: " + m.chat.ModelName())
	channel := make(chan modelsEvent, 1)
	m.overlay = overlayModelsLoading
	go func() {
		models, err := m.chat.ListModels(context.Background())
		channel <- modelsEvent{models: models, err: err}
		close(channel)
	}()
	return nextModels(channel)
}

func (m *Model) applyModelSwitch(argument string) tea.Cmd {
	argument = strings.TrimSpace(argument)
	if argument == "" {
		return nil
	}
	if isDecimal(argument) {
		index := 0
		for _, r := range argument {
			index = index*10 + int(r-'0')
		}
		index--
		if index < 0 || index >= len(m.modelList()) {
			m.appendStyled("Invalid model number. Model unchanged.", styleYellow)
			return nil
		}
		argument = m.modelList()[index]
	}
	if err := m.chat.SwitchModel(argument); err != nil {
		m.appendStyled("Could not select a model. Try /model <name> or check your connection and key.", styleRed)
		return nil
	}
	m.appendText("Model: " + m.chat.ModelName() + " · conversation retained")
	return nil
}

// modelList holds the last fetched models for numeric selection.
func (m *Model) modelList() []string { return m.lastModels }

func (m *Model) startResponse(prompt string) tea.Cmd {
	m.markLastMessage()
	m.appendStyled("Assistant ["+m.chat.ModelName()+"]", styleHeader)
	m.liveEntry = newMarkdownEntry("")
	m.liveEntry.live = true
	m.liveChunks = nil
	m.entries = append(m.entries, m.liveEntry)
	m.touch()
	m.status = "Responding · Ctrl+C to interrupt"
	m.lastPaint = time.Time{}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	channel := make(chan streamEvent, 256)
	m.stream = channel
	go func() {
		text, err := m.chat.Stream(ctx, prompt,
			func(status string) { channel <- streamEvent{kind: eventActivity, text: status} },
			func(full string) { channel <- streamEvent{kind: eventDelta, text: full} },
		)
		channel <- streamEvent{kind: eventDone, text: text, err: err}
		close(channel)
	}()
	return nextStream(channel)
}

func (m *Model) onInterrupt() tea.Cmd {
	if m.responding() {
		if m.cancel != nil {
			m.cancel()
		}
		m.status = "Interrupting response…"
		return nil
	}
	if m.overlay != overlayNone {
		m.overlay = overlayNone
		m.appendStyled("Model selection cancelled.", styleDim)
		m.status = "Ready"
		return nil
	}
	return tea.Quit
}

func (m *Model) clearTranscript() {
	m.chat.Clear()
	m.entries = nil
	m.liveEntry = nil
	m.userEntries = nil
	m.lastEcho = nil
	m.gPending = false
	m.followTail = true
	m.touch()
}

func (m *Model) markLastMessage() {
	if m.lastEcho == nil {
		return
	}
	if len(m.userEntries) > 0 && m.userEntries[len(m.userEntries)-1] == m.lastEcho {
		return
	}
	m.userEntries = append(m.userEntries, m.lastEcho)
}

func (m *Model) scrollToLastMessage() {
	anchors := m.messageAnchors()
	if len(anchors) == 0 {
		m.notifyNoMessage()
		return
	}
	m.pinOffset(anchors[len(anchors)-1].offset)
}

func (m *Model) previousMessage() {
	anchors := m.messageAnchors()
	if len(anchors) == 0 {
		m.notifyNoMessage()
		return
	}
	current := m.scrollOffset()
	target := anchors[0].offset
	for _, item := range anchors {
		if item.offset < current {
			target = item.offset
		}
	}
	m.pinOffset(target)
}

func (m *Model) nextMessage() {
	anchors := m.messageAnchors()
	if len(anchors) == 0 {
		m.notifyNoMessage()
		return
	}
	current := m.scrollOffset()
	for _, item := range anchors {
		if item.offset > current {
			m.pinOffset(item.offset)
			return
		}
	}
}

func (m *Model) messageAnchors() []anchor {
	width := m.effectiveWidth()
	wanted := make(map[*Entry]bool, len(m.userEntries))
	for _, entry := range m.userEntries {
		wanted[entry] = true
	}
	var anchors []anchor
	offset := 0
	for _, entry := range m.entries {
		if wanted[entry] {
			anchors = append(anchors, anchor{entry: entry, offset: offset})
		}
		offset += len(entry.lines(width))
	}
	return anchors
}

func (m *Model) notifyNoMessage() {
	m.appendStyled("No previous message to scroll to.", styleDim)
}

// syncViewport gives the viewport the window size, the transcript and, while
// the user is following, the bottom of it. It is idempotent and cheap, so it can
// be called whenever the window has to be accurate.
func (m *Model) syncViewport() {
	width := m.effectiveWidth()
	m.viewport.Width = width
	m.viewport.Height = m.viewportHeight()
	if m.viewSynced != m.revision || m.viewWidth != width {
		m.viewport.SetContent(strings.Join(m.transcriptLines(width), "\n"))
		m.viewSynced = m.revision
		m.viewWidth = width
	}
	if m.followTail {
		m.viewport.GotoBottom()
	}
}

// scrollOffset is the line at the top of the window. While following it is the
// furthest the transcript can scroll, which is where the viewport sits.
func (m *Model) scrollOffset() int {
	m.syncViewport()
	return m.viewport.YOffset
}

func (m *Model) pinOffset(offset int) {
	m.syncViewport()
	if offset < m.maxScrollOffset(m.viewportHeight()) {
		m.followTail = false
		m.viewport.SetYOffset(offset)
		m.status = "At message · gg/p back · n forward · G end"
		return
	}
	m.followTailNow()
}

func (m *Model) followTailNow() {
	m.followTail = true
	m.syncViewport()
	m.viewport.GotoBottom()
	m.status = "Following transcript"
}

func (m *Model) scrollBack(amount int) {
	if amount <= 0 {
		return
	}
	m.syncViewport()
	m.followTail = false
	m.viewport.SetYOffset(max(0, m.viewport.YOffset-amount))
	m.status = "Transcript paused · End follows new output"
}

func (m *Model) scrollForward(amount int) {
	if amount <= 0 || m.followTail {
		return
	}
	m.syncViewport()
	m.viewport.SetYOffset(m.viewport.YOffset + amount)
	if m.viewport.AtBottom() {
		m.followTailNow()
		return
	}
	m.status = "Transcript paused · End follows new output"
}

// maxScrollOffset is the furthest the transcript window can move.
func (m *Model) maxScrollOffset(height int) int {
	m.syncViewport()
	return max(0, m.viewport.TotalLineCount()-height)
}

func (m *Model) pageLines() int { return max(1, m.viewportHeight()-2) }

func (m *Model) halfPageLines() int { return max(1, m.viewportHeight()/2) }

func (m *Model) viewportHeight() int {
	rows := m.height
	if rows < 1 {
		rows = 24
	}
	editor := m.editorHeight(m.effectiveWidth())
	return max(1, rows-(1+editor+1))
}

func (m *Model) editorHeight(width int) int {
	promptWidth := displayWidth(m.promptLabel())
	available := max(1, width-promptWidth)
	visual := 0
	for _, line := range strings.Split(m.editor.Text(), "\n") {
		visual += max(1, (runeLen(line)+available-1)/available)
	}
	return min(4, max(3, visual))
}

func (m *Model) effectiveWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

func (m *Model) promptLabel() string { return "You [" + m.chat.ModelName() + "] > " }

// responding reports whether a response is streaming. It is derived from the
// stream channel instead of being stored, so the two can never disagree.
func (m *Model) responding() bool { return m.stream != nil }

func (m *Model) refreshCompletions() {
	m.completions = nil
	m.completionIndex = -1
	if m.overlay != overlayNone {
		return
	}
	text := m.editor.Text()
	if !strings.HasPrefix(text, "/") || strings.Contains(text, "\n") || !m.editor.CursorAtEnd() {
		return
	}
	m.completions = command.Match(text)
}

func (m *Model) cycleCompletions(step int) {
	count := len(m.completions)
	if count == 0 {
		return
	}
	index := 0
	if m.completionIndex < 0 {
		if step > 0 {
			index = 0
		} else {
			index = count - 1
		}
	} else {
		index = ((m.completionIndex+step)%count + count) % count
	}
	m.completionIndex = index
	m.editor.SetText(m.completions[index])
}

func (m *Model) edited() { m.refreshCompletions() }

func (m *Model) appendText(text string) {
	m.entries = append(m.entries, newTextEntry(text))
	m.touch()
}

func (m *Model) appendStyled(text, style string) {
	m.entries = append(m.entries, newStyledEntry(text, style))
	m.touch()
}

// touch marks the transcript as needing a rebuild. Every mutation of the
// entries goes through it, which is what makes the cache below impossible to
// forget: there is no flag to set.
func (m *Model) touch() { m.revision++ }

// transcriptLines renders every entry at width, memoised until the model
// changes or the width does. View calls it, so it must not mutate anything
// except the cache itself.
func (m *Model) transcriptLines(width int) []string {
	if m.renderedLines != nil && m.renderedRevision == m.revision && m.renderedWidth == width {
		return m.renderedLines
	}
	var lines []string
	for _, entry := range m.entries {
		lines = append(lines, entry.lines(width)...)
	}
	lines = append(lines, "")
	m.renderedLines = lines
	m.renderedWidth = width
	m.renderedRevision = m.revision
	return lines
}

// View implements tea.Model. It reads the viewport that Update keeps in sync
// with the model, so it does not have to reason about scroll positions.
func (m *Model) View() string {
	width := m.effectiveWidth()
	editorHeight := m.editorHeight(width)
	visible := strings.Split(m.viewport.View(), "\n")
	window := len(visible)
	// The completion menu is drawn over the bottom of the transcript window,
	// right above the input line.
	if menu := m.menuLines(width); len(menu) > 0 {
		start := max(0, window-len(menu))
		for i, line := range menu {
			if start+i < window {
				visible[start+i] = line
			}
		}
	}
	editor, cursorRow, cursorCell := m.renderEditor(width, editorHeight)
	parts := make([]string, 0, window+len(editor)+2)
	parts = append(parts, visible...)
	parts = append(parts, sgr("90", strings.Repeat("─", width)))
	parts = append(parts, editor...)
	parts = append(parts, m.renderFooter(width))
	// Tell the output writer where the caret is. Terminals anchor the IME
	// candidate window at the hardware cursor, and Bubble Tea would otherwise
	// leave it in the bottom-left corner. The frame always fills the screen, so
	// the line counted from the top is also the line the terminal shows.
	cursorLine := len(visible) + 1 + cursorRow
	m.cursorAnchor.Set(cursorLine+1, cursorCell+1)
	return strings.Join(parts, "\n")
}

// renderEditor renders the editor block. It also reports the caret as a 0-based
// row within that block and its 0-based display column within that row, which
// View uses to place the hardware cursor.
func (m *Model) renderEditor(width, height int) (editor []string, cursorRow, cursorCell int) {
	prompt := m.promptLabel()
	promptWidth := displayWidth(prompt)
	promptRunes := runeLen(prompt)
	lines, cursorLine, cursorCol := m.editor.VisualLines(width, promptWidth)
	start := 0
	if len(lines) > height {
		start = cursorLine - height + 1
		start = max(0, start)
		start = min(start, len(lines)-height)
	}
	editor = make([]string, 0, height)
	for i := 0; i < height; i++ {
		index := start + i
		if index >= len(lines) {
			editor = append(editor, "")
			continue
		}
		line := lines[index]
		if index == 0 {
			line = prompt + line
		}
		editor = append(editor, line)
	}
	cursorRow = cursorLine - start
	if cursorLine >= 0 && cursorLine < len(lines) {
		// The caret sits after cursorCol runes of the line, so its screen cell
		// is the display width of that prefix. Counting runes would place it a
		// cell early for every full-width character before it, which is exactly
		// where the terminal anchors the IME candidate window.
		cursorCell = displayWidth(firstRunes(lines[cursorLine], cursorCol))
		if cursorRow == 0 {
			cursorCell += promptWidth
		}
	}
	if cursorRow >= 0 && cursorRow < len(editor) {
		runeIndex := cursorCol
		if cursorRow == 0 {
			runeIndex += promptRunes
		}
		editor[cursorRow] = applyCursor(editor[cursorRow], runeIndex, m.editor.Mode())
	}
	return editor, cursorRow, cursorCell
}

func (m *Model) renderFooter(width int) string {
	text := " " + m.editor.Mode().Label() + "  " + m.status +
		"  Enter send · Alt+Enter newline · Tab complete · PgUp/PgDn scroll"
	return sgr(ansiReverse, padRight(truncateRunes(text, width), width))
}

func (m *Model) menuLines(width int) []string {
	if m.overlay != overlayNone || len(m.completions) == 0 {
		return nil
	}
	const maxMenu = 5
	var lines []string
	for index, name := range m.completions {
		if index >= maxMenu {
			break
		}
		text := " " + name
		if description := commandDescriptions[name]; description != "" {
			text += "  " + description
		}
		text = padRight(truncateRunes(text, width), width)
		if index == m.completionIndex {
			lines = append(lines, sgr(styleMenuSel, text))
		} else {
			lines = append(lines, sgr(styleMenu, text))
		}
	}
	return lines
}

func applyCursor(line string, column int, mode Mode) string {
	runes := []rune(line)
	if column < 0 {
		column = 0
	}
	// Modal cursor shapes mirror prompt_toolkit's ModalCursorShapeConfig:
	// insert is a beam, normal is a block, replace is an underline.
	if mode == ModeInsert {
		if column >= len(runes) {
			return line + beamCursor
		}
		return string(runes[:column]) + beamCursor + string(runes[column+1:])
	}
	style := ansiReverse
	if mode == ModeReplace {
		style = ansiUnder
	}
	if column >= len(runes) {
		return line + "\x1b[" + style + "m \x1b[0m"
	}
	return string(runes[:column]) + "\x1b[" + style + "m" + string(runes[column]) + ansiReset + string(runes[column+1:])
}

func (m *Model) indexOf(target *Entry) int {
	for index, entry := range m.entries {
		if entry == target {
			return index
		}
	}
	return -1
}

func insertAt(entries []*Entry, index int, entry *Entry) []*Entry {
	entries = append(entries, nil)
	copy(entries[index+1:], entries[index:])
	entries[index] = entry
	return entries
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func nextStream(channel <-chan streamEvent) tea.Cmd {
	// Receiving from a nil channel blocks forever, so a stopped stream must not
	// schedule another read.
	if channel == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-channel
		if !ok {
			return streamEvent{kind: eventDone}
		}
		return event
	}
}

func nextModels(channel <-chan modelsEvent) tea.Cmd {
	if channel == nil {
		return nil
	}
	return func() tea.Msg {
		event, ok := <-channel
		if !ok {
			return modelsEvent{err: errors.New("model list closed")}
		}
		return event
	}
}

// Run starts the full-screen UI and prints the finished transcript to stdout.
func Run(c Session) error {
	model := New(c)
	program := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		// Keep the real cursor on the editor caret so the terminal puts the IME
		// candidate window there instead of in the bottom-left corner.
		tea.WithOutput(NewCursorWriter(os.Stdout, model.cursorAnchor)),
	)
	final, err := program.Run()
	if finalModel, ok := final.(*Model); ok {
		finalModel.PrintTranscript(os.Stdout)
	}
	return err
}

// PrintTranscript writes the retained transcript to w after the UI exits.
func (m *Model) PrintTranscript(w io.Writer) {
	width := m.effectiveWidth()
	for _, entry := range m.entries {
		for _, line := range entry.lines(width) {
			fmt.Fprintln(w, line)
		}
	}
}
