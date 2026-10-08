"""Full-screen terminal UI with a fixed Vim input area and Rich transcript."""

from __future__ import annotations

import asyncio
import io
import time
import unicodedata
from copy import copy
from dataclasses import dataclass, field
from typing import Any

from loguru import logger
from prompt_toolkit import Application
from prompt_toolkit.buffer import Buffer
from prompt_toolkit.completion import CompleteEvent
from prompt_toolkit.cursor_shapes import ModalCursorShapeConfig
from prompt_toolkit.enums import EditingMode
from prompt_toolkit.filters import (
    Condition,
    has_completions,
    vi_insert_mode,
    vi_insert_multiple_mode,
    vi_navigation_mode,
    vi_replace_mode,
)
from prompt_toolkit.formatted_text import ANSI, FormattedText, split_lines
from prompt_toolkit.history import InMemoryHistory
from prompt_toolkit.input import Input, create_input
from prompt_toolkit.key_binding import KeyBindings, merge_key_bindings
from prompt_toolkit.layout import Dimension, Float, FloatContainer, HSplit, Layout
from prompt_toolkit.layout.containers import ScrollOffsets, Window
from prompt_toolkit.layout.controls import BufferControl, FormattedTextControl
from prompt_toolkit.layout.menus import CompletionsMenu
from prompt_toolkit.mouse_events import MouseEventType
from prompt_toolkit.output import Output, create_output
from prompt_toolkit.patch_stdout import patch_stdout
from prompt_toolkit.styles import Style
from prompt_toolkit.utils import get_cwidth
from rich.console import Console
from rich.markdown import Markdown
from rich.text import Text

from ai_chat.chat import Chat
from ai_chat.terminal import CommandCompleter, key_bindings, run_chat


@dataclass
class TranscriptEntry:
    """A Rich print call retained for live rendering and final scrollback."""

    objects: tuple[object, ...]
    options: dict[str, Any] = field(default_factory=dict)
    cached_width: int | None = None
    cached_ansi: ANSI | None = None
    live_response: bool = False

    def invalidate(self) -> None:
        self.cached_width = None
        self.cached_ansi = None

    def render(self, width: int) -> ANSI:
        if self.cached_ansi is not None and self.cached_width == width:
            return self.cached_ansi

        buffer = io.StringIO()
        renderer = Console(
            file=buffer,
            force_terminal=True,
            color_system="truecolor",
            width=max(1, width),
            highlight=False,
        )
        # prompt_toolkit's ANSI parser handles styles but not OSC 8 links.
        # Show URLs as text in the live view; retain original Rich renderables
        # (and their terminal hyperlinks) for the final shell transcript.
        display_objects = []
        for obj in self.objects:
            if isinstance(obj, Markdown):
                obj = copy(obj)
                obj.hyperlinks = False
            display_objects.append(obj)
        renderer.print(*display_objects, **self.options)
        self.cached_width = width
        self.cached_ansi = ANSI(buffer.getvalue())
        return self.cached_ansi


class TranscriptConsole(Console):
    """Console-shaped collector; print calls become transcript entries."""

    def __init__(self, ui: TerminalUI) -> None:
        super().__init__(file=io.StringIO(), force_terminal=False)
        self.ui = ui

    def print(self, *objects: object, **kwargs: Any) -> None:  # type: ignore[override]
        self.ui._append_entry(objects, kwargs)

    def begin_live_response(self) -> TranscriptEntry:
        entry = TranscriptEntry((Markdown(""),), live_response=True)
        self.ui.entries.append(entry)
        self.ui._active_response_entry = entry
        self.ui._changed()
        return entry

    def add_activity(self, text: str) -> None:
        entry = TranscriptEntry(
            (Text(_safe_literal(text), style="dim"),), {"markup": False}
        )
        response = self.ui._active_response_entry
        if response is None or response not in self.ui.entries:
            self.ui.entries.append(entry)
        else:
            at = self.ui.entries.index(response)
            self.ui.entries.insert(at, entry)
        self.ui._changed()

    def finish_live_response(self, entry: TranscriptEntry, text: str) -> None:
        if text:
            entry.objects = (Markdown(text),)
            entry.invalidate()
        else:
            try:
                self.ui.entries.remove(entry)
            except ValueError:
                pass
        self.ui._active_response_entry = None
        self.ui._changed()

    def clear_transcript(self) -> None:
        """Drop the displayed conversation after the model context is reset."""
        self.ui.entries.clear()
        self.ui._active_response_entry = None
        self.ui._user_entries.clear()
        self.ui._last_echo_entry = None
        self.ui._g_pending = False
        self.ui._follow_tail = True
        self.ui._scroll_offset = 0
        self.ui._changed()


class TranscriptControl(FormattedTextControl):
    """Formatted transcript control that owns wheel scrolling."""

    def __init__(self, ui: TerminalUI) -> None:
        self.ui = ui
        super().__init__(
            ui._transcript_text,
            focusable=False,
            show_cursor=False,
            get_cursor_position=ui._transcript_cursor,
        )

    def mouse_handler(self, mouse_event):
        if mouse_event.event_type == MouseEventType.SCROLL_UP:
            self.ui._scroll_back(3)
            return None
        if mouse_event.event_type == MouseEventType.SCROLL_DOWN:
            self.ui._scroll_forward(3)
            return None
        return super().mouse_handler(mouse_event)


class TerminalUI:
    """Own the prompt_toolkit app and bridge its input to the chat controller."""

    def __init__(
        self,
        chat: Chat,
        *,
        input: Input | None = None,
        output: Output | None = None,
    ) -> None:
        self.chat = chat
        self.entries: list[TranscriptEntry] = []
        self.transcript_console = TranscriptConsole(self)
        self._owns_input = input is None
        self._input: Input = input if input is not None else create_input()
        self._output: Output = output if output is not None else create_output()
        self._pending_prompt: asyncio.Future[str] | None = None
        self._prompt_label = "You > "
        self._response_task: asyncio.Task[Any] | None = None
        self._controller_task: asyncio.Task[None] | None = None
        self._controller_error: BaseException | None = None
        self._closing = False
        self._status = "Ready"
        self._follow_tail = True
        self._scroll_offset = 0
        self._active_response_entry: TranscriptEntry | None = None
        self._user_entries: list[TranscriptEntry] = []
        self._last_echo_entry: TranscriptEntry | None = None
        self._g_pending = False
        self._g_time = 0.0
        self._line_count = 0
        self._text_length = 0

        self.buffer = Buffer(
            completer=CommandCompleter(),
            history=InMemoryHistory(),
            complete_while_typing=True,
            multiline=True,
            accept_handler=self._accept_input,
        )
        self.buffer.on_text_changed += self._on_text_changed
        self.buffer.on_cursor_position_changed += self._on_buffer_changed

        self._buffer_control = BufferControl(buffer=self.buffer, focusable=True)
        self._transcript_control = TranscriptControl(self)
        self.transcript_window = Window(
            content=self._transcript_control,
            wrap_lines=False,
            height=Dimension(weight=1, min=1),
            get_vertical_scroll=self._transcript_scroll,
            scroll_offsets=ScrollOffsets(top=0, bottom=0),
            always_hide_cursor=True,
            style="class:transcript",
        )
        self._editor_window = Window(
            content=self._buffer_control,
            wrap_lines=True,
            height=self._editor_dimension,
            get_line_prefix=self._line_prefix,
            style="class:editor",
        )
        self._footer_window = Window(
            content=FormattedTextControl(self._footer_text, focusable=False),
            height=1,
            style="class:footer",
        )
        self._root = HSplit(
            [
                self.transcript_window,
                Window(height=1, char="─", style="class:divider"),
                self._editor_window,
                self._footer_window,
            ]
        )
        root = FloatContainer(
            content=self._root,
            floats=[
                Float(
                    xcursor=True,
                    ycursor=True,
                    content=CompletionsMenu(max_height=5, scroll_offset=1),
                )
            ],
        )
        self._keys = self._create_key_bindings()
        self.app: Application[None] = Application(
            layout=Layout(root, focused_element=self._buffer_control),
            key_bindings=self._keys,
            editing_mode=EditingMode.VI,
            cursor=ModalCursorShapeConfig(),
            full_screen=True,
            mouse_support=True,
            min_redraw_interval=1 / 12,
            input=self._input,
            output=self._output,
            style=Style.from_dict(
                {
                    "prompt": "bold ansicyan",
                    "editor": "",
                    "footer": "reverse",
                    "divider": "ansibrightblack",
                    "completion-menu": "bg:#303030 #ffffff",
                    "completion-menu.completion": "bg:#303030 #ffffff",
                    "completion-menu.completion.current": "bg:#005f87 #ffffff",
                    "completion-menu.meta.completion": "bg:#303030 #808080",
                    "completion-menu.meta.completion.current": "bg:#005f87 #afd7ff",
                }
            ),
        )
        # Escape is ambiguous with the start of terminal escape sequences and
        # with Alt+Enter, so prompt_toolkit briefly buffers it. Keep the parser
        # and key timeouts short (not the 0.5s/1.0s defaults) so a lone Escape
        # leaves insert mode promptly while Alt+Enter is still recognized.
        self.app.ttimeoutlen = 0.05
        self.app.timeoutlen = 0.05

    @property
    def waiting_for_input(self) -> bool:
        return self._pending_prompt is not None and not self._pending_prompt.done()

    @property
    def response_running(self) -> bool:
        return self._response_task is not None and not self._response_task.done()

    @property
    def following_tail(self) -> bool:
        return self._follow_tail

    @property
    def scroll_offset(self) -> int:
        return self._scroll_offset

    @property
    def status(self) -> str:
        return self._status

    async def prompt_async(self, label: str) -> str:
        """Wait for one accepted line while keeping the full-screen editor live."""
        if self._closing:
            raise EOFError
        if self.waiting_for_input:
            raise RuntimeError("A terminal prompt is already waiting for input")
        self._prompt_label = label
        self._set_status("Ready")
        loop = asyncio.get_running_loop()
        future: asyncio.Future[str] = loop.create_future()
        self._pending_prompt = future
        self._changed()
        try:
            return await future
        finally:
            if self._pending_prompt is future:
                self._pending_prompt = None
            self._changed()

    async def render_response(self, chat: Chat, prompt: str, console: Console) -> None:
        """Stream Markdown and tool activity into the live transcript."""
        del console  # The supplied console is this UI's transcript collector.
        self._response_task = asyncio.current_task()
        target = self.transcript_console
        entry = target.begin_live_response()
        text = ""
        self._set_status("Responding · Ctrl+C to interrupt")

        def activity(message: str) -> None:
            target.add_activity(message)

        try:
            async for text in chat.stream(prompt, on_activity=activity):
                entry.objects = (Markdown(text),)
                entry.invalidate()
                self._changed()
        finally:
            target.finish_live_response(entry, text)
            if self._response_task is asyncio.current_task():
                self._response_task = None
            self._set_status("Ready")

    async def run(self, console: Console) -> None:
        """Run controller and application, then emit stable Rich scrollback."""
        try:
            with patch_stdout():
                await self.app.run_async(pre_run=self._start_controller)
        except EOFError:
            # A terminal can report EOF when its input stream is closed. Treat
            # that as the same clean exit as Ctrl+D at an empty prompt.
            pass
        finally:
            self._closing = True
            if self._controller_task is not None and not self._controller_task.done():
                self._controller_task.cancel()
                try:
                    await self._controller_task
                except asyncio.CancelledError:
                    pass
            self._release_input()
            for entry in self.entries:
                console.print(*entry.objects, **entry.options)
        if self._controller_error is not None:
            raise self._controller_error

    def _start_controller(self) -> None:
        if self._controller_task is None:
            self._controller_task = asyncio.create_task(self._run_controller())

    async def _run_controller(self) -> None:
        try:
            await run_chat(
                self.chat,
                self,
                self.transcript_console,
                response_renderer=self.render_response,
                on_clear=self.transcript_console.clear_transcript,
                on_start=self.scroll_to_last_message,
                on_message=self.mark_last_message,
            )
        except asyncio.CancelledError:
            raise
        except Exception as exc:  # noqa: BLE001 - controller boundary preserves failures
            self._controller_error = exc
            logger.warning("Terminal controller failed ({})", type(exc).__name__)
        finally:
            if self.app.is_running:
                self.app.exit()

    def _release_input(self) -> None:
        if self._owns_input:
            self._input.close()

    def _accept_input(self, buffer: Buffer) -> bool:
        future = self._pending_prompt
        if future is None or future.done():
            self._set_status("Responding · draft kept; Ctrl+C interrupts")
            return True

        value = buffer.text
        literal_label = self._prompt_label
        self.transcript_console.print(
            Text(_safe_literal(literal_label + value)), markup=False
        )
        if self.entries:
            self._last_echo_entry = self.entries[-1]
        self._g_pending = False
        self._follow_tail = True
        self._scroll_offset = 0
        future.set_result(value)
        self._set_status("Submitted")
        return False

    def mark_last_message(self) -> None:
        """Remember the echoed line that began the latest assistant turn."""
        entry = self._last_echo_entry
        if entry is None or (self._user_entries and self._user_entries[-1] is entry):
            return
        self._user_entries.append(entry)

    def scroll_to_last_message(self) -> None:
        """Pin the last answered question at the top so its reply reads in full."""
        anchors = self._message_anchors()
        if not anchors:
            self._notify_no_message()
            return
        self._pin_offset(anchors[-1][1])

    def previous_message(self) -> None:
        """Move to the previous question, or the last one when following the tail."""
        anchors = self._message_anchors()
        if not anchors:
            self._notify_no_message()
            return
        current = self._current_top_offset()
        target = anchors[0][1]
        for _, offset in anchors:
            if offset < current:
                target = offset
        self._pin_offset(target)

    def next_message(self) -> None:
        """Move to the next question, when one exists."""
        anchors = self._message_anchors()
        if not anchors:
            self._notify_no_message()
            return
        current = self._current_top_offset()
        for _, offset in anchors:
            if offset > current:
                self._pin_offset(offset)
                return

    def _message_anchors(self) -> list[tuple[TranscriptEntry, int]]:
        """First transcript line of every question, in order."""
        width = self._transcript_width()
        wanted = {id(entry) for entry in self._user_entries}
        anchors: list[tuple[TranscriptEntry, int]] = []
        offset = 0
        for entry in self.entries:
            if id(entry) in wanted:
                anchors.append((entry, offset))
            offset += self._visible_lines(entry, width)
        return anchors

    def _visible_lines(self, entry: TranscriptEntry, width: int) -> int:
        # split_lines always yields one extra trailing (empty) line.
        fragments = list(entry.render(width).__pt_formatted_text__())
        return max(0, len(list(split_lines(FormattedText(fragments)))) - 1)

    def _current_top_offset(self) -> int:
        if self._follow_tail:
            return self._max_scroll_offset(self._viewport_height())
        return self._scroll_offset

    def _pin_offset(self, offset: int) -> None:
        if offset < self._max_scroll_offset(self._viewport_height()):
            self._follow_tail = False
            self._scroll_offset = offset
            self._set_status("At message · gg/p back · n forward · G end")
        else:
            # Too close to the bottom to pin at the top; the tail is the best view.
            self._follow_tail = True
            self._scroll_offset = 0
            self._set_status("Following transcript")
        self._changed()

    def _notify_no_message(self) -> None:
        self.transcript_console.print(
            Text("No previous message to scroll to.", style="dim"), markup=False
        )

    def _create_key_bindings(self) -> KeyBindings:
        bindings = KeyBindings()

        @bindings.add("enter", eager=True)
        def submit(event) -> None:
            if self.waiting_for_input:
                event.current_buffer.validate_and_handle()
            else:
                self._set_status("Responding · draft kept; Ctrl+C interrupts")

        @bindings.add("c-c", eager=True)
        def interrupt(event) -> None:
            del event
            if self.response_running:
                assert self._response_task is not None
                self._response_task.cancel()
                self._set_status("Interrupting response…")
            elif self.waiting_for_input:
                assert self._pending_prompt is not None
                self._pending_prompt.set_exception(KeyboardInterrupt())
                self._set_status("Prompt cancelled")
            elif self._controller_task is not None and not self._controller_task.done():
                self._controller_task.cancel()
                self._set_status("Cancelling model selection…")

        @bindings.add("c-b", eager=True)
        def page_up_both(event) -> None:
            del event
            self._scroll_back(self._page_lines())

        @bindings.add("c-f", eager=True)
        def page_down_both(event) -> None:
            del event
            self._scroll_forward(self._page_lines())

        @bindings.add("pageup", eager=True)
        @bindings.add("c-pageup", eager=True)
        def page_up(event) -> None:
            del event
            self._scroll_back(self._page_lines())

        @bindings.add("pagedown", eager=True)
        @bindings.add("c-pagedown", eager=True)
        def page_down(event) -> None:
            del event
            self._scroll_forward(self._page_lines())

        @bindings.add("tab", eager=True)
        def complete_command(event) -> None:
            # Apply synchronously so Tab is not raced by the automatic
            # completion triggered on every keystroke.
            if self.buffer.complete_state is None:
                completions = list(
                    self.buffer.completer.get_completions(
                        self.buffer.document,
                        CompleteEvent(completion_requested=True),
                    )
                )
                if completions:
                    self.buffer.apply_completion(completions[0])
            else:
                self._cycle_completions(1)

        @bindings.add("down", filter=has_completions, eager=True)
        def next_completion(event) -> None:
            self._cycle_completions(1)

        @bindings.add("up", filter=has_completions, eager=True)
        def previous_completion(event) -> None:
            self._cycle_completions(-1)

        editing_modes = vi_insert_mode | vi_insert_multiple_mode | vi_replace_mode

        @bindings.add("c-u", filter=editing_modes, eager=True)
        def clear_line_or_scroll_up(event) -> None:
            # With nothing typed, Ctrl+U scrolls half a page up so the user can
            # read back; otherwise it discards from the cursor to the line start.
            buffer = event.current_buffer
            if not buffer.text:
                self._scroll_back(self._half_page_lines())
                return
            start = -buffer.document.get_start_of_line_position()
            if start > 0:
                buffer.delete_before_cursor(count=start)

        @bindings.add("c-u", filter=vi_navigation_mode, eager=True)
        def half_page_up(event) -> None:
            del event
            self._scroll_back(self._half_page_lines())

        @bindings.add("c-d", eager=True)
        def half_page_down(event) -> None:
            # Ctrl+D only ever scrolls; it must never exit, so a user who keeps
            # scrolling cannot be surprised by the program quitting at the end.
            del event
            self._scroll_forward(self._half_page_lines())

        @bindings.add("c-e", filter=editing_modes, eager=True)
        def end_of_line(event) -> None:
            buffer = event.current_buffer
            buffer.cursor_position += buffer.document.get_end_of_line_position()

        @bindings.add("c-e", filter=vi_navigation_mode, eager=True)
        def ignore_ctrl_e(event) -> None:
            del event

        @bindings.add("g", filter=vi_navigation_mode)
        def gg_prefix(event) -> None:
            # `gg` is two keys, but the short key timeout can flush a lone `g`.
            # Remember it so a second, slower `g` still moves to the previous
            # question instead of being dropped.
            del event
            now = time.monotonic()
            if self._g_pending and now - self._g_time <= 1.5:
                self._g_pending = False
                self.previous_message()
            else:
                self._g_pending = True
                self._g_time = now

        @bindings.add("g", "g", filter=vi_navigation_mode)
        def gg_previous(event) -> None:
            del event
            self._g_pending = False
            self.previous_message()

        @bindings.add("p", filter=vi_navigation_mode)
        def p_previous(event) -> None:
            del event
            self._g_pending = False
            self.previous_message()

        @bindings.add("n", filter=vi_navigation_mode)
        def n_next(event) -> None:
            del event
            self._g_pending = False
            self.next_message()

        @bindings.add("j", filter=vi_navigation_mode, eager=True)
        def scroll_line_down(event) -> None:
            del event
            self._scroll_forward(1)

        @bindings.add("k", filter=vi_navigation_mode, eager=True)
        def scroll_line_up(event) -> None:
            del event
            self._scroll_back(1)

        @bindings.add("G", filter=vi_navigation_mode, eager=True)
        @bindings.add(
            "end", filter=Condition(lambda: not self._follow_tail), eager=True
        )
        @bindings.add("c-end", eager=True)
        def follow_tail(event) -> None:
            del event
            self._follow_tail = True
            self._scroll_offset = 0
            self._set_status("Following transcript")
            self._changed()

        # Reuse the controller's command completion, Tab behavior, and the
        # Escape-then-Enter newline binding (Alt+Enter sends the same bytes).
        return merge_key_bindings([key_bindings(), bindings])

    def _append_entry(
        self, objects: tuple[object, ...], options: dict[str, Any]
    ) -> None:
        self.entries.append(TranscriptEntry(tuple(objects), dict(options)))
        self._changed()

    def _set_status(self, value: str) -> None:
        self._status = value
        self._changed()

    def _changed(self, *_: object) -> None:
        if hasattr(self, "app") and self.app.is_running:
            self.app.invalidate()

    def _on_buffer_changed(self, *_: object) -> None:
        self._changed()

    def _cycle_completions(self, step: int) -> None:
        """Cycle command candidates without falling back to the typed prefix."""
        state = self.buffer.complete_state
        if state is None or not state.completions:
            return
        count = len(state.completions)
        if state.complete_index is None:
            index = 0 if step > 0 else count - 1
        else:
            index = (state.complete_index + step) % count
        self.buffer.go_to_completion(index)

    def _on_text_changed(self, *_: object) -> None:
        # prompt_toolkit only auto-completes after insertion, so a command
        # palette would vanish when deleting. Rebuild it after shrink edits;
        # applying a completion only ever grows the text, so it is not re-run.
        previous, self._text_length = self._text_length, len(self.buffer.text)
        if (
            self._text_length < previous
            and self.buffer.text.startswith("/")
            and "\n" not in self.buffer.text
        ):
            self.buffer.start_completion()
        self._changed()

    def _line_prefix(self, lineno: int, wrap_count: int) -> FormattedText:
        """Render the prompt inline before the first input line."""
        if lineno == 0 and wrap_count == 0:
            return FormattedText([("class:prompt", self._prompt_label)])
        # Keep wrapped and continuation lines aligned under the first column.
        return FormattedText([("class:prompt", " " * get_cwidth(self._prompt_label))])

    def _footer_text(self) -> FormattedText:
        mode = "INSERT"
        current_mode = self.app.vi_state.input_mode.value
        mode = {
            "vi-navigation": "NORMAL",
            "vi-replace": "REPLACE",
            "vi-replace-single": "REPLACE",
            "vi-insert-multiple": "INSERT",
        }.get(current_mode, "INSERT")
        pieces = [
            ("bold", f" {mode} "),
            ("class:status", f"  {self._status}"),
            ("", "  Enter send · Alt+Enter newline · Tab complete · PgUp/PgDn scroll"),
        ]
        return FormattedText(pieces)

    def _editor_dimension(self) -> Dimension:
        # Keep a stable input box (3 lines for a short or empty draft) that grows
        # to at most 4 lines for longer drafts, so the layout does not shrink
        # after a response once the transcript grows. The prompt prefix reduces
        # the usable width, so account for it when counting wrapped lines.
        columns = max(1, self._output.get_size().columns)
        available = max(1, columns - get_cwidth(self._prompt_label))
        visual_lines = 0
        for line in self.buffer.text.split("\n"):
            line_width = sum(get_cwidth(character) for character in line)
            visual_lines += max(1, (line_width + available - 1) // available)
        visible_lines = min(4, max(3, visual_lines))
        return Dimension(min=3, preferred=visible_lines, max=visible_lines)

    def _transcript_width(self) -> int:
        return max(1, self._output.get_size().columns)

    def _viewport_height(self) -> int:
        """Transcript height from the layout, not a possibly stale render."""
        rows = max(1, self._output.get_size().rows)
        editor = self._editor_dimension().preferred
        editor_lines = max(1, editor) if isinstance(editor, int) else 1
        # The transcript shares the screen with divider, editor and footer.
        return max(1, rows - (1 + editor_lines + 1))

    def _max_scroll_offset(self, height: int) -> int:
        self._transcript_text()  # Refresh the measured line count.
        return max(0, self._line_count - height)

    def _rendered_entries(self) -> list[ANSI]:
        width = self._transcript_width()
        return [entry.render(width) for entry in self.entries]

    def _transcript_text(self) -> FormattedText:
        rendered = self._rendered_entries()
        fragments: list[tuple[str, str]] = []
        for item in rendered:
            fragments.extend(item.__pt_formatted_text__())
        content = FormattedText(fragments)
        self._line_count = len(list(split_lines(content)))
        return content

    def _transcript_cursor(self):
        if not self._line_count:
            return None
        from prompt_toolkit.data_structures import Point

        line = self._line_count - 1 if self._follow_tail else self._scroll_offset
        return Point(x=0, y=max(0, min(line, self._line_count - 1)))

    def _transcript_scroll(self, window: Window) -> int:
        if self._follow_tail:
            return max(0, self._line_count - 1)
        return self._scroll_offset

    def _page_lines(self) -> int:
        return max(1, self._viewport_height() - 2)

    def _half_page_lines(self) -> int:
        return max(1, self._viewport_height() // 2)

    def _scroll_back(self, amount: int) -> None:
        height = self._viewport_height()
        max_offset = self._max_scroll_offset(height)
        # Follow the tail from the true bottom, and never keep an offset that
        # a stale render measurement allowed to drift past the current bottom.
        current = (
            max_offset if self._follow_tail else min(self._scroll_offset, max_offset)
        )
        self._follow_tail = False
        self._scroll_offset = max(0, current - amount)
        self._set_status("Transcript paused · End follows new output")

    def _scroll_forward(self, amount: int) -> None:
        if self._follow_tail:
            # Already following the bottom; scrolling down stays at the tail.
            return
        height = self._viewport_height()
        max_offset = self._max_scroll_offset(height)
        new_offset = min(max_offset, self._scroll_offset + amount)
        if new_offset >= max_offset:
            self._follow_tail = True
            self._scroll_offset = 0
            self._set_status("Following transcript")
        else:
            self._scroll_offset = new_offset
            self._set_status("Transcript paused · End follows new output")


async def run_terminal(chat: Chat, console: Console) -> None:
    """Run the full-screen UI and print the finished transcript to scrollback."""
    ui = TerminalUI(chat)
    await ui.run(console)


def _safe_literal(value: str) -> str:
    """Make control characters visible rather than writing terminal commands."""
    result: list[str] = []
    for character in value:
        if character == "\n":
            result.append(character)
        elif character == "\t":
            result.append("\\t")
        elif unicodedata.category(character) == "Cc":
            codepoint = ord(character)
            result.append(
                f"\\x{codepoint:02x}" if codepoint <= 0xFF else f"\\u{codepoint:04x}"
            )
        else:
            result.append(character)
    return "".join(result)
