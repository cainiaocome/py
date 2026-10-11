package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// This file holds every key binding in one table. The port originally had two
// large switch statements, which meant a key could be handled in one place, its
// mode check written somewhere else, and its documentation somewhere else
// again. Expressing bindings as data keeps those three in step and makes the
// vim layer a matrix rather than a pile of nested conditionals.

// ggWindow is how long the first "g" of a "gg" stays armed.
const ggWindow = 1500 * time.Millisecond

// modeMask selects the editor modes a binding applies in.
type modeMask uint8

const (
	inInsert modeMask = 1 << iota
	inReplace
	inNormal

	insertModes = inInsert | inReplace
	anyMode     = inInsert | inReplace | inNormal
)

// bits expands a mask into the individual modes, so a binding registered for
// insertModes is found from either insert mode or replace mode.
func (mask modeMask) bits() []modeMask {
	var modes []modeMask
	for _, bit := range []modeMask{inInsert, inReplace, inNormal} {
		if mask&bit != 0 {
			modes = append(modes, bit)
		}
	}
	return modes
}

// action is what a binding does.
type action func(*Model) tea.Cmd

// keyStroke is the key a binding is registered for, plus the editor mode it
// applies in.
type keyStroke struct {
	key  string
	mode modeMask
}

// keyBindings answers "what does this key do right now?". Registering by
// (key, mode) makes a duplicate impossible to hide: buildKeyBindings panics
// instead, so a shadowed binding fails loudly at startup rather than silently
// doing nothing.
var keyBindings = buildKeyBindings()

func buildKeyBindings() map[keyStroke]action {
	table := make(map[keyStroke]action, 64)
	add := func(mode modeMask, run action, keys ...string) {
		for _, key := range keys {
			for _, bit := range mode.bits() {
				stroke := keyStroke{key: key, mode: bit}
				if _, exists := table[stroke]; exists {
					panic("duplicate key binding for " + key)
				}
				table[stroke] = run
			}
		}
	}

	// edit runs a buffer change and refreshes the completion state.
	edit := func(change func(*Editor)) action {
		return func(m *Model) tea.Cmd {
			change(m.editor)
			m.edited()
			return nil
		}
	}
	// plain runs a binding that does not touch the buffer.
	plain := func(run func(*Model)) action {
		return func(m *Model) tea.Cmd {
			run(m)
			return nil
		}
	}

	// Everything below applies in every mode.
	add(anyMode, func(m *Model) tea.Cmd { return m.onInterrupt() }, "ctrl+c")
	add(anyMode, plain(func(m *Model) { m.scrollBack(m.pageLines()) }), "ctrl+b", "pgup", "ctrl+pgup")
	add(anyMode, plain(func(m *Model) { m.scrollForward(m.pageLines()) }), "ctrl+f", "pgdown", "ctrl+pgdown")
	// Ctrl+D only ever scrolls half a page down; it must never exit. A user who
	// keeps scrolling can reach the end of a long answer without realizing it
	// and would otherwise quit the program by accident.
	add(anyMode, plain((*Model).followTailNow), "ctrl+end")
	add(anyMode, plain(func(m *Model) { m.scrollForward(m.halfPageLines()) }), "ctrl+d")
	add(anyMode, (*Model).submit, "enter")
	add(anyMode, edit((*Editor).Newline), "alt+enter")
	add(anyMode, plain(func(m *Model) { m.cycleCompletions(1) }), "tab")
	add(anyMode, plain(func(m *Model) {
		if len(m.completions) > 0 {
			m.cycleCompletions(1)
			return
		}
		m.editor.MoveDown()
		m.edited()
	}), "down")
	add(anyMode, plain(func(m *Model) {
		if len(m.completions) > 0 {
			m.cycleCompletions(-1)
			return
		}
		m.editor.MoveUp()
		m.edited()
	}), "up")
	add(anyMode, edit((*Editor).MoveLeft), "left")
	add(anyMode, edit((*Editor).MoveRight), "right")
	add(anyMode, edit((*Editor).MoveHome), "home")
	add(anyMode, plain(func(m *Model) {
		// End doubles as "follow the tail" while the transcript is paused.
		if !m.followTail {
			m.followTailNow()
			return
		}
		m.editor.MoveEnd()
		m.edited()
	}), "end")
	add(anyMode, edit(func(e *Editor) { e.SetMode(ModeNormal) }), "esc", "escape")
	add(anyMode, edit((*Editor).DeleteForward), "delete")

	// Input editing, in the modes where typing makes sense.
	add(insertModes, plain(func(m *Model) {
		if m.editor.Empty() {
			m.scrollBack(m.halfPageLines())
			return
		}
		m.editor.DeleteToLineStart()
		m.edited()
	}), "ctrl+u")
	add(insertModes, edit((*Editor).MoveEnd), "ctrl+e")
	add(insertModes, edit((*Editor).Backspace), "backspace")

	// Vim normal mode: motions, entry points and single-key edits.
	add(inNormal, plain(func(m *Model) { m.scrollBack(m.halfPageLines()) }), "ctrl+u")
	add(inNormal, edit((*Editor).MoveLeft), "backspace")
	add(inNormal, plain((*Model).previousMessage), "p")
	add(inNormal, plain((*Model).nextMessage), "n")
	add(inNormal, plain((*Model).followTailNow), "G")
	add(inNormal, plain(func(m *Model) { m.scrollForward(1) }), "j")
	add(inNormal, plain(func(m *Model) { m.scrollBack(1) }), "k")
	add(inNormal, plain(func(m *Model) { m.scrollBack(m.halfPageLines()) }), "u")
	add(inNormal, plain(func(m *Model) { m.scrollForward(m.halfPageLines()) }), "d")
	add(inNormal, plain(func(m *Model) { m.scrollForward(m.pageLines()) }), " ")
	add(inNormal, plain(func(m *Model) {
		now := time.Now()
		if m.gPending && now.Sub(m.gTime) <= ggWindow {
			m.gPending = false
			m.previousMessage()
			return
		}
		m.gPending = true
		m.gTime = now
	}), "g")
	add(inNormal, edit((*Editor).InsertMode), "i")
	add(inNormal, edit((*Editor).Append), "a")
	add(inNormal, edit((*Editor).InsertAtLineStart), "I")
	add(inNormal, edit((*Editor).AppendLineEnd), "A")
	add(inNormal, edit((*Editor).OpenBelow), "o")
	add(inNormal, edit((*Editor).OpenAbove), "O")
	add(inNormal, edit((*Editor).ReplaceMode), "R")
	add(inNormal, plain(func(m *Model) { m.editor.SetPending('r') }), "r")
	add(inNormal, edit((*Editor).DeleteForward), "x")
	add(inNormal, edit((*Editor).DeleteToLineEnd), "D")
	add(inNormal, edit(func(e *Editor) {
		e.DeleteToLineEnd()
		e.InsertMode()
	}), "C")
	add(inNormal, edit(func(e *Editor) { e.Motion("h") }), "h")
	add(inNormal, edit(func(e *Editor) { e.Motion("l") }), "l")
	add(inNormal, edit(func(e *Editor) { e.Motion("w") }), "w")
	add(inNormal, edit(func(e *Editor) { e.Motion("b") }), "b")
	add(inNormal, edit(func(e *Editor) { e.Motion("e") }), "e")
	add(inNormal, edit(func(e *Editor) { e.Motion("0") }), "0")
	add(inNormal, edit(func(e *Editor) { e.Motion("$") }), "$")
	add(inNormal, edit(func(e *Editor) { e.Motion("^") }), "^")

	return table
}

// handleKey routes a key press to the binding registered for the current mode,
// falling back to text input.
func (m *Model) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	if key != "g" {
		m.gPending = false
	}
	mode := m.editor.Mode().mask()

	// After "r" the next key is the replacement character, whatever it is, so
	// it must not run the binding it would otherwise trigger.
	if mode == inNormal && m.editor.Pending() == 'r' {
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

	if run, ok := keyBindings[keyStroke{key: key, mode: mode}]; ok {
		return run(m)
	}
	if mode == inNormal {
		return nil
	}

	// Anything else is text. Bubble Tea reports a lone space as KeySpace rather
	// than KeyRunes, so both must be handled or typed spaces vanish.
	switch msg.Type {
	case tea.KeyRunes:
		m.editor.InsertText(string(msg.Runes))
	case tea.KeySpace:
		m.editor.InsertText(" ")
	default:
		return nil
	}
	m.edited()
	return nil
}
