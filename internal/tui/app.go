package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cainiaocome/py/internal/textutil"
)

// liveRefreshInterval throttles re-rendering the growing live answer. The first
// chunk always paints and the finished text is always painted once.
const liveRefreshInterval = 50 * time.Millisecond

type appState int

const (
	stateReady appState = iota
	stateResponding
	stateModelLoading
	stateModelSelect
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

	width  int
	height int
	state  appState
	status string

	followTail   bool
	scrollOffset int
	lineCount    int
	linesCache   []string
	linesWidth   int
	linesDirty   bool

	completions     []string
	completionIndex int
	lastModels      []string

	stream      chan streamEvent
	cancel      context.CancelFunc
	pendingText string
	lastPaint   time.Time

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
		state:           stateReady,
		status:          "Ready",
		followTail:      true,
		linesDirty:      true,
		completionIndex: -1,
		cursorAnchor:    NewCursorAnchor(),
	}
	m.entries = append(m.entries,
		newTextEntry("AI Chat · Ollama Cloud · Model: "+c.ModelName()),
		newTextEntry("Vim editing · Enter: send · Alt+Enter: newline · Tab: complete · /start · /clear · /exit · /model"),
		newTextEntry("Web tools: disabled · "+EnableWebCommand),
	)
	return m
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch message := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = message.Width
		m.height = message.Height
		m.linesDirty = true
		return m, nil
	case tea.MouseMsg:
		return m, m.handleMouse(message)
	case streamEvent:
		return m, m.handleStream(message)
	case modelsEvent:
		return m, m.handleModels(message)
	case tea.KeyMsg:
		return m, m.handleKey(message)
	}
	return m, nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) tea.Cmd {
	if msg.Action != tea.MouseActionPress {
		return nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.scrollBack(3)
	case tea.MouseButtonWheelDown:
		m.scrollForward(3)
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
		m.linesDirty = true
		return nextStream(m.stream)
	case eventDelta:
		if m.liveEntry != nil {
			m.pendingText = event.text
			now := time.Now()
			if m.lastPaint.IsZero() || now.Sub(m.lastPaint) >= liveRefreshInterval {
				m.liveEntry.setText(event.text)
				m.linesDirty = true
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
			m.linesDirty = true
		}
		m.pendingText = ""
		m.stream = nil
		m.cancel = nil
		m.state = stateReady
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
	if m.state != stateModelLoading {
		return nil
	}
	if event.err != nil {
		m.appendStyled("Could not select a model. Try /model <name> or check your connection and key.", styleRed)
		m.state = stateReady
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
	m.state = stateModelSelect
	m.status = "Ready"
	m.lastModels = event.models
	return nil
}

func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if key != "g" {
		m.gPending = false
	}

	switch key {
	case "ctrl+c":
		return m.onInterrupt()
	case "ctrl+b":
		m.scrollBack(m.pageLines())
		return nil
	case "ctrl+f":
		m.scrollForward(m.pageLines())
		return nil
	case "pgup", "ctrl+pgup":
		m.scrollBack(m.pageLines())
		return nil
	case "pgdown", "ctrl+pgdown":
		m.scrollForward(m.pageLines())
		return nil
	case "ctrl+end":
		m.followTailNow()
		return nil
	case "ctrl+d":
		// Ctrl+D only ever scrolls half a page down; it must never exit.
		m.scrollForward(m.halfPageLines())
		return nil
	case "enter":
		return m.submit()
	case "alt+enter":
		m.editor.Newline()
		m.edited()
		return nil
	case "tab":
		m.cycleCompletions(1)
		return nil
	case "down":
		if len(m.completions) > 0 {
			m.cycleCompletions(1)
			return nil
		}
		m.editor.MoveDown()
		m.edited()
		return nil
	case "up":
		if len(m.completions) > 0 {
			m.cycleCompletions(-1)
			return nil
		}
		m.editor.MoveUp()
		m.edited()
		return nil
	case "left":
		m.editor.MoveLeft()
		m.edited()
		return nil
	case "right":
		m.editor.MoveRight()
		m.edited()
		return nil
	case "home":
		m.editor.MoveHome()
		m.edited()
		return nil
	case "end":
		if !m.followTail {
			m.followTailNow()
			return nil
		}
		m.editor.MoveEnd()
		m.edited()
		return nil
	case "ctrl+u":
		if m.editor.Mode() == ModeNormal || m.editor.Empty() {
			m.scrollBack(m.halfPageLines())
			return nil
		}
		m.editor.DeleteToLineStart()
		m.edited()
		return nil
	case "ctrl+e":
		if m.editor.Mode() == ModeNormal {
			return nil
		}
		m.editor.MoveEnd()
		m.edited()
		return nil
	case "esc", "escape":
		m.editor.SetMode(ModeNormal)
		m.edited()
		return nil
	case "backspace":
		if m.editor.Mode() == ModeNormal {
			m.editor.MoveLeft()
		} else {
			m.editor.Backspace()
		}
		m.edited()
		return nil
	case "delete":
		m.editor.DeleteForward()
		m.edited()
		return nil
	}

	if m.editor.Mode() == ModeNormal {
		return m.handleNormalKey(msg)
	}
	switch msg.Type {
	case tea.KeyRunes:
		m.editor.InsertText(string(msg.Runes))
	case tea.KeySpace:
		// Bubble Tea reports a lone space as KeySpace rather than KeyRunes, so
		// it has to be handled explicitly or typed spaces would vanish.
		m.editor.InsertText(" ")
	default:
		return nil
	}
	m.edited()
	return nil
}

func (m *Model) handleNormalKey(msg tea.KeyMsg) tea.Cmd {
	if m.editor.Pending() == 'r' {
		switch {
		case msg.Type == tea.KeyRunes && len(msg.Runes) > 0:
			m.editor.ReplaceChar(msg.Runes[0])
		case msg.Type == tea.KeySpace:
			m.editor.ReplaceChar(' ')
		}
		m.editor.ClearPending()
		m.edited()
		return nil
	}

	switch msg.String() {
	case "g":
		now := time.Now()
		if m.gPending && now.Sub(m.gTime) <= 1500*time.Millisecond {
			m.gPending = false
			m.previousMessage()
		} else {
			m.gPending = true
			m.gTime = now
		}
		return nil
	case "G":
		m.followTailNow()
		return nil
	case "p":
		m.previousMessage()
		return nil
	case "n":
		m.nextMessage()
		return nil
	case "j":
		m.scrollForward(1)
		return nil
	case "k":
		m.scrollBack(1)
		return nil
	case "u":
		m.scrollBack(m.halfPageLines())
		return nil
	case "d":
		m.scrollForward(m.halfPageLines())
		return nil
	case " ":
		m.scrollForward(m.pageLines())
		return nil
	case "i":
		m.editor.InsertMode()
	case "a":
		m.editor.Append()
	case "I":
		m.editor.InsertAtLineStart()
	case "A":
		m.editor.AppendLineEnd()
	case "o":
		m.editor.OpenBelow()
	case "O":
		m.editor.OpenAbove()
	case "R":
		m.editor.ReplaceMode()
	case "x", "delete":
		m.editor.DeleteForward()
	case "D":
		m.editor.DeleteToLineEnd()
	case "C":
		m.editor.DeleteToLineEnd()
		m.editor.InsertMode()
	case "r":
		m.editor.SetPending('r')
	case "h", "l", "w", "b", "e", "0", "$", "^":
		m.editor.Motion(msg.String())
	case "backspace":
		m.editor.MoveLeft()
	default:
		return nil
	}
	m.edited()
	return nil
}

func (m *Model) submit() tea.Cmd {
	if m.state == stateResponding || m.state == stateModelLoading {
		m.status = "Responding · draft kept; Ctrl+C interrupts"
		return nil
	}
	raw := m.editor.Text()
	value := strings.TrimSpace(raw)

	echo := newTextEntry(m.promptLabel() + textutil.SafeLiteral(raw))
	m.entries = append(m.entries, echo)
	m.linesDirty = true
	m.lastEcho = echo
	m.editor.Clear()
	m.refreshCompletions()
	m.gPending = false
	m.followTail = true
	m.scrollOffset = 0
	m.status = "Submitted"

	if m.state == stateModelSelect {
		m.state = stateReady
		if value == "" {
			return nil
		}
		return m.applyModelSwitch(value)
	}
	if value == "" {
		return nil
	}
	switch value {
	case "/exit":
		return tea.Quit
	case EnableWebCommand:
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
	}
	first := value
	if index := strings.IndexAny(value, " \t"); index >= 0 {
		first = value[:index]
	}
	if first == "/model" {
		return m.selectModel(strings.TrimSpace(strings.TrimPrefix(value, "/model")))
	}
	if strings.HasPrefix(value, "/") {
		m.appendStyled("Unknown command. Available: "+commandNames(), styleYellow)
		return nil
	}
	return m.startResponse(value)
}

func (m *Model) selectModel(argument string) tea.Cmd {
	if argument != "" {
		return m.applyModelSwitch(argument)
	}
	m.appendText("Current model: " + m.chat.ModelName())
	channel := make(chan modelsEvent, 1)
	m.state = stateModelLoading
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
	m.entries = append(m.entries, m.liveEntry)
	m.linesDirty = true
	m.state = stateResponding
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
	switch m.state {
	case stateResponding:
		if m.cancel != nil {
			m.cancel()
		}
		m.status = "Interrupting response…"
		return nil
	case stateModelLoading, stateModelSelect:
		m.state = stateReady
		m.appendStyled("Model selection cancelled.", styleDim)
		m.status = "Ready"
		return nil
	default:
		return tea.Quit
	}
}

func (m *Model) clearTranscript() {
	m.chat.Clear()
	m.entries = nil
	m.liveEntry = nil
	m.userEntries = nil
	m.lastEcho = nil
	m.gPending = false
	m.followTail = true
	m.scrollOffset = 0
	m.linesDirty = true
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
	current := m.currentTopOffset()
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
	current := m.currentTopOffset()
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

func (m *Model) pinOffset(offset int) {
	if offset < m.maxScrollOffset(m.viewportHeight()) {
		m.followTail = false
		m.scrollOffset = offset
		m.status = "At message · gg/p back · n forward · G end"
	} else {
		m.followTail = true
		m.scrollOffset = 0
		m.status = "Following transcript"
	}
}

func (m *Model) followTailNow() {
	m.followTail = true
	m.scrollOffset = 0
	m.status = "Following transcript"
}

func (m *Model) scrollBack(amount int) {
	height := m.viewportHeight()
	maxOffset := m.maxScrollOffset(height)
	current := maxOffset
	if !m.followTail {
		current = min(m.scrollOffset, maxOffset)
	}
	m.followTail = false
	m.scrollOffset = max(0, current-amount)
	m.status = "Transcript paused · End follows new output"
}

func (m *Model) scrollForward(amount int) {
	if m.followTail {
		return
	}
	height := m.viewportHeight()
	maxOffset := m.maxScrollOffset(height)
	newOffset := min(maxOffset, m.scrollOffset+amount)
	if newOffset >= maxOffset {
		m.followTailNow()
		return
	}
	m.scrollOffset = newOffset
	m.status = "Transcript paused · End follows new output"
}

func (m *Model) currentTopOffset() int {
	if m.followTail {
		return m.maxScrollOffset(m.viewportHeight())
	}
	return m.scrollOffset
}

func (m *Model) maxScrollOffset(height int) int {
	m.transcriptLines(m.effectiveWidth())
	return max(0, m.lineCount-height)
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
	promptWidth := runeLen(m.promptLabel())
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

func (m *Model) refreshCompletions() {
	m.completions = nil
	m.completionIndex = -1
	if m.state != stateReady {
		return
	}
	text := m.editor.Text()
	if !strings.HasPrefix(text, "/") || strings.Contains(text, "\n") || !m.editor.CursorAtEnd() {
		return
	}
	m.completions = matchCommands(text)
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
	m.linesDirty = true
}

func (m *Model) appendStyled(text, style string) {
	m.entries = append(m.entries, newStyledEntry(text, style))
	m.linesDirty = true
}

func (m *Model) transcriptLines(width int) []string {
	if !m.linesDirty && m.linesWidth == width && m.linesCache != nil {
		return m.linesCache
	}
	var lines []string
	for _, entry := range m.entries {
		lines = append(lines, entry.lines(width)...)
	}
	lines = append(lines, "")
	m.linesCache = lines
	m.linesWidth = width
	m.linesDirty = false
	m.lineCount = len(lines)
	return lines
}

func (m *Model) topOffset(total, viewport int) int {
	if m.followTail {
		return max(0, total-viewport)
	}
	return min(max(0, m.scrollOffset), max(0, total-viewport))
}

// View implements tea.Model.
func (m *Model) View() string {
	width := m.effectiveWidth()
	height := m.height
	if height < 1 {
		height = 24
	}
	lines := m.transcriptLines(width)
	editorHeight := m.editorHeight(width)
	viewport := max(1, height-(1+editorHeight+1))
	top := m.topOffset(len(lines), viewport)
	visible := make([]string, viewport)
	for i := 0; i < viewport; i++ {
		if index := top + i; index >= 0 && index < len(lines) {
			visible[i] = lines[index]
		}
	}
	if menu := m.menuLines(width); len(menu) > 0 {
		start := viewport - len(menu)
		if start < 0 {
			start = 0
		}
		for i, line := range menu {
			if start+i < viewport {
				visible[start+i] = line
			}
		}
	}
	editor, cursorRow, cursorColumn := m.renderEditor(width, editorHeight)
	parts := make([]string, 0, len(visible)+len(editor)+2)
	parts = append(parts, visible...)
	parts = append(parts, sgr("90", strings.Repeat("─", width)))
	parts = append(parts, editor...)
	parts = append(parts, m.renderFooter(width))
	// Tell the output writer where the caret is. Terminals anchor the IME
	// candidate window at the hardware cursor, and Bubble Tea would otherwise
	// leave it in the bottom-left corner. The frame always fills the screen, so
	// the line counted from the top is also the line the terminal shows.
	cursorLine := len(visible) + 1 + cursorRow
	m.cursorAnchor.Set(cursorLine+1, cursorColumn+1)
	return strings.Join(parts, "\n")
}

// renderEditor renders the editor block. It also reports the caret as a 0-based
// row within that block and a 0-based display column within that row.
func (m *Model) renderEditor(width, height int) (editor []string, cursorRow, cursorColumn int) {
	prompt := m.promptLabel()
	promptWidth := runeLen(prompt)
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
	cursorColumn = cursorCol
	if cursorRow == 0 {
		cursorColumn += promptWidth
	}
	if cursorRow >= 0 && cursorRow < len(editor) {
		editor[cursorRow] = applyCursor(editor[cursorRow], cursorColumn, m.editor.Mode())
	}
	return editor, cursorRow, cursorColumn
}

func (m *Model) renderFooter(width int) string {
	text := " " + m.editor.Mode().Label() + "  " + m.status +
		"  Enter send · Alt+Enter newline · Tab complete · PgUp/PgDn scroll"
	return sgr(ansiReverse, padRight(truncateRunes(text, width), width))
}

func (m *Model) menuLines(width int) []string {
	if m.state != stateReady || len(m.completions) == 0 {
		return nil
	}
	const maxMenu = 5
	var lines []string
	for index, command := range m.completions {
		if index >= maxMenu {
			break
		}
		text := " " + command
		if description := CommandDescriptions[command]; description != "" {
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
	return func() tea.Msg {
		event, ok := <-channel
		if !ok {
			return streamEvent{kind: eventDone}
		}
		return event
	}
}

func nextModels(channel <-chan modelsEvent) tea.Cmd {
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
