"""Virtual-terminal interaction tests for the full-screen terminal UI."""

import asyncio
import re
import time
from contextlib import asynccontextmanager
from io import StringIO
from types import SimpleNamespace

import pyte
import pytest
from prompt_toolkit.data_structures import Size
from prompt_toolkit.input import create_pipe_input
from prompt_toolkit.key_binding.vi_state import InputMode
from prompt_toolkit.output.vt100 import Vt100_Output
from pydantic_ai.messages import ToolCallPart
from rich.console import Console
from rich.markdown import Markdown
from rich.text import Text

from ai_chat.chat import tool_activity
from ai_chat.ui import TerminalUI


def long_markdown() -> str:
    sections = ["# Transcript sentinel", "", "**BOLD_STYLE_SENTINEL**", ""]
    for index in range(1, 21):
        sections.extend(
            [
                f"## Section {index:02d}",
                "",
                (
                    f"Section body {index:02d} has enough text to wrap at a narrow "
                    "terminal width and verify transcript reflow after resizing."
                ),
                "",
            ]
        )
        if index == 10:
            sections.extend(
                [
                    "```python",
                    'value = "CODE_BLOCK_SENTINEL"',
                    "print(value)",
                    "```",
                    "",
                    "| key | value |",
                    "| --- | --- |",
                    "| sample | TABLE_SENTINEL |",
                    "",
                ]
            )
    sections.append("FINAL_TRANSCRIPT_SENTINEL")
    return "\n".join(sections)


class FakeChat:
    """Small streaming chat double with controllable gates and tool events."""

    def __init__(self) -> None:
        self.model_name = "model-a"
        self.web_enabled = False
        self.prompts: list[str] = []
        self.history: list[str] = []
        self.cleared = 0
        self.responses: dict[str, tuple[str, str]] = {}
        self.gates: dict[str, asyncio.Event] = {}
        self.errors: dict[str, BaseException] = {}
        self.before_chunk_errors: dict[str, BaseException] = {}
        self.activities: dict[str, list[ToolCallPart]] = {}
        self.started: dict[str, asyncio.Event] = {}
        self.finished: dict[str, asyncio.Event] = {}
        self.list_started = asyncio.Event()
        self.list_cancelled = asyncio.Event()
        self.list_gate: asyncio.Event | None = None

    def _event(self, events: dict[str, asyncio.Event], prompt: str) -> asyncio.Event:
        if prompt not in events:
            events[prompt] = asyncio.Event()
        return events[prompt]

    async def list_models(self) -> list[str]:
        self.list_started.set()
        if self.list_gate is not None:
            try:
                await self.list_gate.wait()
            except asyncio.CancelledError:
                self.list_cancelled.set()
                raise
        return ["model-a", "model-b"]

    def switch_model(self, model: str) -> None:
        self.model_name = model

    def clear(self) -> None:
        self.cleared += 1
        self.history.clear()

    def enable_web(self) -> bool:
        changed = not self.web_enabled
        self.web_enabled = True
        return changed

    async def stream(self, prompt: str, *, on_activity=None):
        self.prompts.append(prompt)
        self._event(self.started, prompt).set()
        error_before_chunk = self.before_chunk_errors.get(prompt)
        if error_before_chunk is not None:
            raise error_before_chunk
        partial, final = self.responses.get(
            prompt, (f"Partial answer for {prompt}", f"**Answer for {prompt}**")
        )
        yield partial
        if self.web_enabled and on_activity:
            for part in self.activities.get(prompt, []):
                status = tool_activity(part)
                if status:
                    on_activity(status)
        gate = self.gates.get(prompt)
        if gate is not None:
            await gate.wait()
        error = self.errors.get(prompt)
        if error is not None:
            raise error
        yield final
        self.history.append(prompt)
        self._event(self.finished, prompt).set()


class VirtualTerminal:
    def __init__(self, ui, input_pipe, output_stream, console_stream, dimensions):
        self.ui = ui
        self.input = input_pipe
        self.output_stream = output_stream
        self.console_stream = console_stream
        self.dimensions = dimensions
        self.screen = pyte.Screen(dimensions[1], dimensions[0])
        self.vt_stream = pyte.Stream(self.screen, strict=False)
        self.output_offset = 0
        self.task: asyncio.Task[None] | None = None

    def sync(self) -> None:
        output = self.output_stream.getvalue()
        self.vt_stream.feed(output[self.output_offset :])
        self.output_offset = len(output)

    def text(self) -> str:
        self.sync()
        return "\n".join(self.screen.display)

    def current_prompt_line(self) -> str:
        self.sync()
        render_info = self.ui.transcript_window.render_info
        if render_info is None:
            return ""
        row = render_info.window_height + 1
        return self.screen.display[row]

    def send(self, keys: str) -> None:
        self.input.send_text(keys)

    async def wait_for(self, predicate, description: str, timeout: float = 3.0):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            self.sync()
            if predicate():
                return
            if self.task is not None and self.task.done():
                error = self.task.exception()
                raise AssertionError(
                    f"UI exited before {description}; task exception={error!r}"
                )
            await asyncio.sleep(0.005)
        raise AssertionError(f"timed out waiting for {description}")

    async def submit(self, value: str = "") -> None:
        self.send(value + "\r")
        await self.wait_for(
            lambda: self.ui.waiting_for_input,
            "the next prompt",
        )

    async def close(self) -> None:
        if self.task is None or self.task.done():
            return
        # Shut down through the application itself; never rely on a key binding.
        if self.ui.app.is_running:
            self.ui.app.exit()
        try:
            await asyncio.wait_for(asyncio.shield(self.task), timeout=2)
        except TimeoutError:
            self.task.cancel()
            try:
                await self.task
            except asyncio.CancelledError:
                pass


@asynccontextmanager
async def running_ui(chat: FakeChat, *, width: int = 72, height: int = 18):
    with create_pipe_input() as input_pipe:
        output_stream = StringIO()
        console_stream = StringIO()
        dimensions = [height, width]
        vt_output = Vt100_Output(
            output_stream,
            get_size=lambda: Size(rows=dimensions[0], columns=dimensions[1]),
            enable_cpr=False,
        )
        ui = TerminalUI(chat, input=input_pipe, output=vt_output)
        console = Console(
            file=console_stream,
            force_terminal=True,
            color_system="standard",
            width=width,
            height=height,
        )
        terminal = VirtualTerminal(
            ui, input_pipe, output_stream, console_stream, dimensions
        )
        terminal.task = asyncio.create_task(ui.run(console))
        try:
            await terminal.wait_for(
                lambda: ui.app.is_running and ui.waiting_for_input,
                "initial prompt",
            )
            yield terminal
        finally:
            await terminal.close()


def chars_for_text(screen: pyte.Screen, needle: str):
    for row in range(screen.lines):
        cells = [screen.buffer[row][column] for column in range(screen.columns)]
        text = "".join(cell.data for cell in cells)
        offset = text.find(needle)
        if offset >= 0:
            yield cells[offset : offset + len(needle)]


def has_bold_text_in_output(output: str, width: int, height: int, needle: str) -> bool:
    screen = pyte.HistoryScreen(width, height, history=2000)
    pty_output = re.sub(r"(?<!\r)\n", "\r\n", output)
    pyte.Stream(screen, strict=False).feed(pty_output)
    rows = list(screen.history.top) + [
        screen.buffer[row] for row in range(screen.lines)
    ]
    for row in rows:
        cells = [row[column] for column in range(screen.columns)]
        text = "".join(cell.data for cell in cells)
        offset = text.find(needle)
        if offset >= 0 and all(
            cell.bold for cell in cells[offset : offset + len(needle)]
        ):
            return True
    return False


def entry_contains(ui: TerminalUI, needle: str) -> bool:
    for entry in ui.entries:
        for obj in entry.objects:
            if isinstance(obj, Text) and needle in obj.plain:
                return True
            if isinstance(obj, str) and needle in obj:
                return True
    return False


@pytest.mark.asyncio
async def test_ctrl_u_clears_line_content_in_insert_mode():
    chat = FakeChat()
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("hello world")
        await terminal.wait_for(
            lambda: terminal.ui.buffer.text == "hello world", "typed text"
        )
        terminal.send("\x15")  # Ctrl+U
        await terminal.wait_for(
            lambda: terminal.ui.buffer.text == "", "Ctrl+U clears the line"
        )
        assert terminal.ui.buffer.cursor_position == 0


@pytest.mark.asyncio
async def test_ctrl_b_and_ctrl_f_page_in_insert_and_normal_mode():
    chat = FakeChat()
    chat.responses["long"] = (long_markdown(), long_markdown())
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("long\r")
        await terminal.wait_for(
            lambda: "long" in chat.history and terminal.ui.waiting_for_input,
            "long transcript",
        )
        assert terminal.ui.following_tail

        terminal.send("\x02")  # Ctrl+B in insert mode
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail and terminal.ui.scroll_offset > 0,
            "insert Ctrl+B pages up",
        )
        insert_offset = terminal.ui.scroll_offset
        terminal.send("\x06")  # Ctrl+F in insert mode
        await terminal.wait_for(
            lambda: (
                terminal.ui.following_tail or terminal.ui.scroll_offset > insert_offset
            ),
            "insert Ctrl+F pages down",
        )

        terminal.send("\x1b")
        await terminal.wait_for(
            lambda: terminal.ui.app.vi_state.input_mode == InputMode.NAVIGATION,
            "normal mode",
        )
        terminal.send("\x02")
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail and terminal.ui.scroll_offset > 0,
            "normal Ctrl+B pages up",
        )
        normal_offset = terminal.ui.scroll_offset
        terminal.send("\x06")
        await terminal.wait_for(
            lambda: (
                terminal.ui.following_tail or terminal.ui.scroll_offset > normal_offset
            ),
            "normal Ctrl+F pages down",
        )


@pytest.mark.asyncio
async def test_ctrl_u_half_page_up_in_normal_mode():
    chat = FakeChat()
    chat.responses["long"] = (long_markdown(), long_markdown())
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("long\r")
        await terminal.wait_for(
            lambda: "long" in chat.history and terminal.ui.waiting_for_input,
            "long transcript",
        )
        terminal.send("\x1b")
        await terminal.wait_for(
            lambda: terminal.ui.app.vi_state.input_mode == InputMode.NAVIGATION,
            "normal mode",
        )
        terminal.send("\x15")  # Ctrl+U half page up in normal mode
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail and terminal.ui.scroll_offset > 0,
            "normal Ctrl+U half page up",
        )


@pytest.mark.asyncio
async def test_ctrl_u_empty_insert_prompt_scrolls_half_page_up():
    chat = FakeChat()
    chat.responses["long"] = (long_markdown(), long_markdown())
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("long\r")
        await terminal.wait_for(
            lambda: "long" in chat.history and terminal.ui.waiting_for_input,
            "long transcript",
        )
        # Nothing typed and following the tail: Ctrl+U reads back a half page.
        assert terminal.ui.buffer.text == ""
        assert terminal.ui.following_tail
        terminal.send("\x15")
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail and terminal.ui.scroll_offset > 0,
            "empty insert Ctrl+U half page up",
        )


@pytest.mark.asyncio
async def test_ctrl_d_insert_scrolls_down_and_never_exits():
    chat = FakeChat()
    chat.responses["long"] = (long_markdown(), long_markdown())
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("long\r")
        await terminal.wait_for(
            lambda: "long" in chat.history and terminal.ui.waiting_for_input,
            "long transcript",
        )

        # A draft at the bottom is preserved: Ctrl+D must not exit or edit.
        terminal.send("draft")
        await terminal.wait_for(lambda: terminal.ui.buffer.text == "draft", "draft")
        terminal.send("\x04")
        await asyncio.sleep(0.2)
        assert terminal.ui.app.is_running
        assert terminal.ui.buffer.text == "draft"
        terminal.send("\x15")
        await terminal.wait_for(lambda: terminal.ui.buffer.text == "", "cleared")

        # Empty prompt, scrolled up: Ctrl+D moves half a page down to the tail.
        terminal.send("\x15")
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail and terminal.ui.scroll_offset > 0,
            "scrolled up",
        )
        raised = terminal.ui.scroll_offset
        terminal.send("\x04")
        await terminal.wait_for(
            lambda: terminal.ui.following_tail or terminal.ui.scroll_offset > raised,
            "insert Ctrl+D scrolls half page down",
        )
        await terminal.wait_for(lambda: terminal.ui.following_tail, "back at the tail")

        # Ctrl+D at the bottom keeps scrolling (a no-op) instead of exiting.
        terminal.send("\x04")
        await asyncio.sleep(0.2)
        assert terminal.ui.app.is_running
        assert terminal.ui.waiting_for_input
        assert terminal.ui.following_tail


@pytest.mark.asyncio
async def test_ctrl_d_insert_draft_noop_and_normal_half_page_down():
    chat = FakeChat()
    chat.responses["long"] = (long_markdown(), long_markdown())
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("long\r")
        await terminal.wait_for(
            lambda: "long" in chat.history and terminal.ui.waiting_for_input,
            "long transcript",
        )

        # Insert mode with a draft: Ctrl+D must neither exit nor edit.
        terminal.send("draft")
        await terminal.wait_for(lambda: terminal.ui.buffer.text == "draft", "draft")
        terminal.send("\x04")
        await asyncio.sleep(0.2)
        assert terminal.ui.buffer.text == "draft"
        assert terminal.ui.app.is_running
        terminal.send("\x15")
        await terminal.wait_for(lambda: terminal.ui.buffer.text == "", "cleared")

        # Normal mode: Ctrl+D scrolls the transcript half a page down.
        terminal.send("\x1b")
        await terminal.wait_for(
            lambda: terminal.ui.app.vi_state.input_mode == InputMode.NAVIGATION,
            "normal mode",
        )
        terminal.send("\x02")
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail and terminal.ui.scroll_offset > 0,
            "normal Ctrl+B pages up",
        )
        up_offset = terminal.ui.scroll_offset
        terminal.send("\x04")
        await terminal.wait_for(
            lambda: terminal.ui.following_tail or terminal.ui.scroll_offset > up_offset,
            "normal Ctrl+D half page down",
        )


@pytest.mark.asyncio
async def test_ctrl_e_insert_end_of_line_and_normal_noop():
    chat = FakeChat()
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("hello world")
        await terminal.wait_for(
            lambda: terminal.ui.buffer.text == "hello world", "typed text"
        )
        terminal.ui.buffer.cursor_position = 5
        await asyncio.sleep(0.1)
        assert terminal.ui.buffer.cursor_position == 5
        terminal.send("\x05")  # Ctrl+E
        await terminal.wait_for(
            lambda: terminal.ui.buffer.cursor_position == len("hello world"),
            "insert Ctrl+E goes to line end",
        )

        # Normal mode with a longer buffer: the default binding would move the
        # cursor down, so Ctrl+E must leave it at the top of the buffer.
        terminal.ui.buffer.text = "\n".join(f"line {index}" for index in range(8))
        terminal.send("\x1b")
        await terminal.wait_for(
            lambda: terminal.ui.app.vi_state.input_mode == InputMode.NAVIGATION,
            "normal mode",
        )
        terminal.ui.buffer.cursor_position = 0
        await asyncio.sleep(0.1)
        terminal.send("\x05")
        await asyncio.sleep(0.2)
        assert terminal.ui.buffer.cursor_position == 0
        assert terminal.ui.buffer.document.cursor_position_row == 0


@pytest.mark.asyncio
async def test_escape_enters_normal_mode_without_a_long_delay():
    chat = FakeChat()
    async with running_ui(chat, width=72, height=14) as terminal:
        assert terminal.ui.app.timeoutlen is not None
        assert terminal.ui.app.timeoutlen <= 0.1
        assert terminal.ui.app.ttimeoutlen <= 0.1
        terminal.send("abc")
        await terminal.wait_for(lambda: terminal.ui.buffer.text == "abc", "typed text")
        assert terminal.ui.app.vi_state.input_mode == InputMode.INSERT
        start = time.monotonic()
        terminal.send("\x1b")
        # The old defaults (0.5s parser + 1.0s key timeout) took over a second;
        # fail fast if Escape is buffered again waiting for a longer binding.
        await terminal.wait_for(
            lambda: terminal.ui.app.vi_state.input_mode == InputMode.NAVIGATION,
            "Escape enters normal mode",
            timeout=1.0,
        )
        assert time.monotonic() - start < 0.6


@pytest.mark.asyncio
async def test_clear_command_removes_displayed_history():
    chat = FakeChat()
    chat.responses["hello"] = ("Answer", "**Answer for hello**")
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("hello\r")
        await terminal.wait_for(
            lambda: "hello" in chat.history and terminal.ui.waiting_for_input,
            "first response",
        )
        await terminal.wait_for(
            lambda: any(
                isinstance(obj, Markdown) and "Answer for hello" in obj.markup
                for entry in terminal.ui.entries
                for obj in entry.objects
            ),
            "answer rendered",
        )
        assert len(terminal.ui.entries) > 1

        terminal.send("/clear\r")
        await terminal.wait_for(
            lambda: (
                chat.cleared == 1
                and entry_contains(terminal.ui, "Conversation cleared")
            ),
            "clear applied",
        )
        # Only the confirmation remains: the banner, user line and answer go.
        assert len(terminal.ui.entries) == 1
        assert not any(
            isinstance(obj, Markdown) and "Answer for hello" in obj.markup
            for entry in terminal.ui.entries
            for obj in entry.objects
        )
        assert not entry_contains(terminal.ui, "You [model-a]")
        assert terminal.ui.following_tail


@pytest.mark.asyncio
async def test_start_command_pins_the_last_question_at_the_top():
    chat = FakeChat()
    chat.responses["long"] = (long_markdown(), long_markdown())
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("long\r")
        await terminal.wait_for(
            lambda: "long" in chat.history and terminal.ui.waiting_for_input,
            "long transcript",
        )
        assert terminal.ui.following_tail

        terminal.send("/start\r")
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail and terminal.ui.scroll_offset > 0,
            "/start pins the last question",
        )
        offset = terminal.ui.scroll_offset
        # The question is the top row, so the reply reads from its start.
        await terminal.wait_for(
            lambda: terminal.text().splitlines()[0].startswith("You [model-a] > long"),
            "/start shows the question at the top",
        )

        # Repeating it is idempotent: it targets the question, not its own echo.
        terminal.send("/start\r")
        await asyncio.sleep(0.2)
        assert terminal.ui.app.is_running
        assert not terminal.ui.following_tail
        assert terminal.ui.scroll_offset == offset
        assert terminal.ui.buffer.text == ""


@pytest.mark.asyncio
async def test_start_command_after_clear_reports_nothing_to_scroll():
    chat = FakeChat()
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("/clear\r")
        await terminal.wait_for(lambda: chat.cleared == 1, "clear applied")
        terminal.send("/start\r")
        await terminal.wait_for(
            lambda: entry_contains(terminal.ui, "No previous message to scroll to"),
            "/start reports nothing to scroll",
        )
        assert terminal.ui.app.is_running


@pytest.mark.asyncio
async def test_normal_mode_message_navigation_keys():
    chat = FakeChat()
    chat.responses["one"] = (long_markdown(), long_markdown())
    chat.responses["two"] = (long_markdown(), long_markdown())
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("one\r")
        await terminal.wait_for(
            lambda: "one" in chat.history and terminal.ui.waiting_for_input,
            "first answer",
        )
        terminal.send("two\r")
        await terminal.wait_for(
            lambda: "two" in chat.history and terminal.ui.waiting_for_input,
            "second answer",
        )
        terminal.send("\x1b")
        await terminal.wait_for(
            lambda: terminal.ui.app.vi_state.input_mode == InputMode.NAVIGATION,
            "normal mode",
        )
        assert terminal.ui.following_tail

        # A slow `g` `g` still works: the short timeout flushes the first `g`.
        terminal.send("g")
        await asyncio.sleep(0.15)
        terminal.send("g")
        await terminal.wait_for(
            lambda: terminal.text().splitlines()[0].startswith("You [model-a] > two"),
            "slow gg goes to the last question",
        )
        # A fast `gg` walks one question further back.
        terminal.send("gg")
        await terminal.wait_for(
            lambda: terminal.text().splitlines()[0].startswith("You [model-a] > one"),
            "gg goes to the previous question",
        )
        # `n` and `p` step forward and back between questions.
        terminal.send("n")
        await terminal.wait_for(
            lambda: terminal.text().splitlines()[0].startswith("You [model-a] > two"),
            "n goes forward",
        )
        terminal.send("p")
        await terminal.wait_for(
            lambda: terminal.text().splitlines()[0].startswith("You [model-a] > one"),
            "p goes back",
        )
        terminal.send("n")
        await terminal.wait_for(
            lambda: terminal.text().splitlines()[0].startswith("You [model-a] > two"),
            "n returns to the last question",
        )
        # There is no question after the last one.
        terminal.send("n")
        await asyncio.sleep(0.1)
        assert terminal.text().splitlines()[0].startswith("You [model-a] > two")
        assert not terminal.ui.following_tail

        # `G` goes to the real end and follows new output.
        terminal.send("G")
        await terminal.wait_for(lambda: terminal.ui.following_tail, "G goes to the end")
        assert terminal.ui.scroll_offset == 0


@pytest.mark.asyncio
async def test_normal_mode_j_k_scroll_one_line():
    chat = FakeChat()
    chat.responses["long"] = (long_markdown(), long_markdown())
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("long\r")
        await terminal.wait_for(
            lambda: "long" in chat.history and terminal.ui.waiting_for_input,
            "long transcript",
        )
        terminal.send("\x1b")
        await terminal.wait_for(
            lambda: terminal.ui.app.vi_state.input_mode == InputMode.NAVIGATION,
            "normal mode",
        )
        assert terminal.ui.following_tail

        terminal.send("k")
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail, "k pauses the transcript"
        )
        first = terminal.ui.scroll_offset
        assert first > 0
        terminal.send("k")
        await terminal.wait_for(
            lambda: terminal.ui.scroll_offset == first - 1, "k scrolls one line up"
        )
        terminal.send("j")
        await terminal.wait_for(
            lambda: terminal.ui.scroll_offset == first, "j scrolls one line down"
        )


@pytest.mark.asyncio
async def test_transcript_lines_are_cached_until_content_changes():
    chat = FakeChat()
    chat.responses["long"] = (long_markdown(), long_markdown())
    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("long\r")
        await terminal.wait_for(
            lambda: "long" in chat.history and terminal.ui.waiting_for_input,
            "long transcript",
        )
        ui = terminal.ui
        width = ui._transcript_width()

        first = ui._transcript_lines(width)
        # Every frame while scrolling reuses the assembled list and the cached
        # per-entry split instead of re-splitting the whole transcript.
        assert ui._transcript_lines(width) is first
        assert ui._line_count == len(first)
        entry = ui.entries[-1]
        assert entry.lines(width) is entry.lines(width)
        ui._scroll_back(3)
        ui._scroll_forward(10)
        assert ui._transcript_lines(width) is first

        # A content change (as one streamed token does) rebuilds the assembly.
        entry.invalidate()
        second = ui._transcript_lines(width)
        assert second is not first
        assert ui._line_count == len(second)

        # A new width rebuilds the per-entry split too.
        assert ui._transcript_lines(width - 20) is not second


@pytest.mark.asyncio
async def test_streaming_keeps_input_pinned_and_draft_survives_busy_enter():
    chat = FakeChat()
    gate = asyncio.Event()
    chat.gates["slow"] = gate
    chat.responses["slow"] = (long_markdown(), long_markdown() + "\n\nSTREAM_END")

    async with running_ui(chat, width=64, height=14) as terminal:
        terminal.send("slow\r")
        await terminal.wait_for(
            lambda: (
                chat._event(chat.started, "slow").is_set()
                and terminal.ui.response_running
            ),
            "stream start",
        )
        await terminal.wait_for(
            lambda: "FINAL_TRANSCRIPT_SENTINEL" in terminal.text(),
            "long transcript rendering",
        )
        assert terminal.ui.following_tail
        terminal.send("draft while busy")
        await terminal.wait_for(
            lambda: "draft while busy" in terminal.text(), "busy draft display"
        )
        terminal.send("\r")
        await terminal.wait_for(
            lambda: "draft kept" in terminal.ui.status, "busy Enter handling"
        )
        assert terminal.ui.buffer.text == "draft while busy"
        assert chat.prompts == ["slow"]
        lines = terminal.text().splitlines()
        assert any("draft while busy" in line for line in lines[-4:])

        gate.set()
        await terminal.wait_for(
            lambda: "slow" in chat.history and terminal.ui.waiting_for_input,
            "completed stream",
        )
        await terminal.wait_for(
            lambda: "STREAM_END" in terminal.text(), "streamed tail is visible"
        )
        assert terminal.ui.buffer.text == "draft while busy"


@pytest.mark.asyncio
async def test_live_stream_repaints_are_throttled_but_finish_complete():
    class ChunkedChat(FakeChat):
        async def stream(self, prompt, *, on_activity=None):
            self.prompts.append(prompt)
            self._event(self.started, prompt).set()
            for index in range(400):
                yield "x" * (index + 1)
            self.history.append(prompt)
            self._event(self.finished, prompt).set()

    chat = ChunkedChat()
    async with running_ui(chat, width=72, height=14) as terminal:
        ui = terminal.ui
        terminal.send("chunky\r")
        await terminal.wait_for(
            lambda: "chunky" in chat.history and ui.waiting_for_input,
            "chunked stream finished",
        )
        response = next(entry for entry in ui.entries if entry.live_response)
        # 400 chunks arrive far faster than the refresh interval, so the live
        # entry is repainted a handful of times instead of once per chunk.
        assert response.revision < 50
        # The final paint always contains the complete answer.
        assert any(
            isinstance(obj, Markdown) and "x" * 400 in obj.markup
            for obj in response.objects
        )


@pytest.mark.asyncio
async def test_follow_tail_pause_page_and_mouse_scrolling_then_end():
    chat = FakeChat()
    chat.responses["long"] = (long_markdown(), long_markdown())

    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("long\r")
        await terminal.wait_for(
            lambda: "long" in chat.history and terminal.ui.waiting_for_input,
            "long transcript completion",
        )
        assert terminal.ui.following_tail

        terminal.send("\x1b[5~")  # xterm PageUp
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail and terminal.ui.scroll_offset > 0,
            "PageUp scroll pause",
        )
        old_offset = terminal.ui.scroll_offset
        assert old_offset > 0

        # SGR mouse wheel up; coordinates are inside the transcript viewport.
        terminal.send("\x1b[<64;2;2M")
        await terminal.wait_for(
            lambda: terminal.ui.scroll_offset < old_offset,
            "mouse wheel scroll",
        )
        up_offset = terminal.ui.scroll_offset
        terminal.send("\x1b[<65;2;2M")
        await terminal.wait_for(
            lambda: terminal.ui.scroll_offset > up_offset,
            "mouse wheel scroll down",
        )
        for _ in range(40):
            if terminal.ui.scroll_offset == 0:
                break
            previous = terminal.ui.scroll_offset
            terminal.send("\x1b[5~")
            await terminal.wait_for(
                lambda previous=previous: terminal.ui.scroll_offset < previous,
                "PageUp moves toward transcript start",
            )
        assert terminal.ui.scroll_offset == 0
        assert not terminal.ui.following_tail
        await terminal.wait_for(
            lambda: "BOLD_STYLE_SENTINEL" in terminal.text(),
            "styled text at transcript start",
        )
        bold_cells = next(chars_for_text(terminal.screen, "BOLD_STYLE_SENTINEL"))
        assert all(cell.bold for cell in bold_cells)

        for _ in range(40):
            if terminal.ui.following_tail:
                break
            previous = terminal.ui.scroll_offset
            terminal.send("\x1b[6~")
            await terminal.wait_for(
                lambda previous=previous: (
                    terminal.ui.following_tail or terminal.ui.scroll_offset > previous
                ),
                "PageDown moves toward transcript tail",
            )
        assert terminal.ui.following_tail
        terminal.send("\x1b[5~")
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail and terminal.ui.scroll_offset > 0,
            "second PageUp scroll pause",
        )
        terminal.send("\x1b[4~")  # xterm End
        await terminal.wait_for(lambda: terminal.ui.following_tail, "End follows tail")


@pytest.mark.asyncio
async def test_scroll_math_uses_layout_height_instead_of_stale_render_info():
    """Regression: PageUp mixed a fresh line count with a stale window height.

    It could therefore leave the paused offset exactly at the true bottom; the
    next wheel-down then snapped to the tail (offset 0) instead of moving down.
    """
    chat = FakeChat()
    chat.responses["long"] = (long_markdown(), long_markdown())

    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("long\r")
        await terminal.wait_for(
            lambda: "long" in chat.history and terminal.ui.waiting_for_input,
            "long transcript completion",
        )
        ui = terminal.ui
        await terminal.wait_for(lambda: ui._line_count > 20, "measured transcript")
        true_height = ui._viewport_height()
        # A stale render can claim a viewport that is far shorter than the
        # layout currently allocates; the scroll math must ignore it.
        ui.transcript_window.render_info = SimpleNamespace(
            window_height=1, content_height=1
        )
        ui._scroll_back(true_height - 2)
        max_offset = ui._max_scroll_offset(true_height)
        assert not ui.following_tail
        assert ui.scroll_offset == max_offset - (true_height - 2)
        assert ui.scroll_offset < max_offset

        before = ui.scroll_offset
        ui._scroll_forward(3)
        assert not ui.following_tail
        assert ui.scroll_offset > before


@pytest.mark.asyncio
async def test_manual_scroll_is_preserved_across_a_followup_prompt():
    chat = FakeChat()
    gate = asyncio.Event()
    chat.responses["first"] = (
        long_markdown(),
        long_markdown() + "\n\nNEW_TOKENS_SENTINEL",
    )
    chat.gates["first"] = gate

    async with running_ui(chat, width=72, height=14) as terminal:
        terminal.send("first\r")
        await terminal.wait_for(
            lambda: (
                terminal.ui.response_running
                and "FINAL_TRANSCRIPT_SENTINEL" in terminal.text()
            ),
            "first response streaming",
        )
        terminal.send("\x1b[5~")
        await terminal.wait_for(
            lambda: not terminal.ui.following_tail and terminal.ui.scroll_offset > 0,
            "manual scroll pause",
        )
        paused_offset = terminal.ui.scroll_offset
        gate.set()
        await terminal.wait_for(
            lambda: "first" in chat.history and terminal.ui.waiting_for_input,
            "response completes while paused",
        )
        assert not terminal.ui.following_tail
        assert terminal.ui.scroll_offset == paused_offset
        assert any(
            isinstance(obj, Markdown) and "NEW_TOKENS_SENTINEL" in obj.markup
            for entry in terminal.ui.entries
            for obj in entry.objects
        )
        terminal.send("next\r")
        await terminal.wait_for(
            lambda: "next" in chat.history and terminal.ui.waiting_for_input,
            "follow-up response completion",
        )
        assert terminal.ui.following_tail


@pytest.mark.asyncio
async def test_resize_reflows_markdown_and_keeps_editor_at_bottom():
    chat = FakeChat()
    chat.responses["resize"] = (long_markdown(), long_markdown())

    async with running_ui(chat, width=80, height=20) as terminal:
        terminal.send("resize\r")
        await terminal.wait_for(
            lambda: "resize" in chat.history and terminal.ui.waiting_for_input,
            "initial transcript completion",
        )
        assert terminal.ui.transcript_window.render_info.window_width == 80

        terminal.dimensions[:] = [11, 40]
        terminal.screen.resize(lines=11, columns=40)
        terminal.ui.app.invalidate()
        await terminal.wait_for(
            lambda: (
                terminal.ui.transcript_window.render_info is not None
                and terminal.ui.transcript_window.render_info.window_width == 40
            ),
            "narrow terminal resize",
        )
        wrapped_draft = "WRAP_START_" + "x" * 70 + "_WRAP_END"
        terminal.send(wrapped_draft)
        await terminal.wait_for(
            lambda: "WRAP_END" in terminal.text(), "wrapped resized input visibility"
        )
        assert terminal.ui.buffer.text == wrapped_draft
        bottom_lines = terminal.text().splitlines()[-7:]
        assert any("WRAP_START" in line for line in bottom_lines)
        assert any("WRAP_END" in line for line in bottom_lines)
        assert terminal.ui.entries
        response = next(
            entry
            for entry in terminal.ui.entries
            if any(isinstance(obj, Markdown) for obj in entry.objects)
        )
        assert response.cached_width == 40


@pytest.mark.asyncio
async def test_vim_cursor_modes_multiline_input_and_web_command_completion():
    chat = FakeChat()
    async with running_ui(chat, width=80, height=18) as terminal:
        terminal.sync()
        terminal.send("\x1b")
        await terminal.wait_for(lambda: "NORMAL" in terminal.text(), "Vim normal mode")
        terminal.sync()
        assert "\x1b[2 q" in terminal.output_stream.getvalue()

        terminal.send("R")
        await terminal.wait_for(
            lambda: "REPLACE" in terminal.text(), "Vim replace mode"
        )
        terminal.sync()
        assert "\x1b[4 q" in terminal.output_stream.getvalue()

        terminal.send("\x1b")
        await terminal.wait_for(
            lambda: "NORMAL" in terminal.text(), "return to Vim normal mode"
        )
        terminal.send("i")
        await terminal.wait_for(lambda: "INSERT" in terminal.text(), "Vim insert mode")
        terminal.sync()
        assert "\x1b[6 q" in terminal.output_stream.getvalue()

        terminal.send("first line\x1b\rsecond line")
        await terminal.wait_for(
            lambda: terminal.ui.buffer.text == "first line\nsecond line",
            "Alt+Enter multiline input",
        )
        assert chat.prompts == []
        terminal.send("\r")
        await terminal.wait_for(
            lambda: (
                chat.prompts == ["first line\nsecond line"]
                and terminal.ui.waiting_for_input
            ),
            "Enter submits the multiline prompt",
        )

        terminal.send("/en\t\t")
        await terminal.wait_for(
            lambda: terminal.ui.buffer.text == "/enable-web-search-and-web-fetch",
            "Tab command completion",
        )
        terminal.send("\r")
        await terminal.wait_for(lambda: chat.web_enabled, "web enable command")


@pytest.mark.asyncio
async def test_web_opt_in_persists_through_model_and_clear_and_displays_safe_tool_details():
    chat = FakeChat()
    query = "literal <b>query</b> \x1b[31mred\x1b[0m"
    url = "https://example.test/a?q=<tag>\x1b[2J"
    chat.activities["web"] = [
        ToolCallPart("web_search", {"query": query}),
        ToolCallPart("web_fetch", {"url": url}),
    ]

    async with running_ui(chat, width=90, height=20) as terminal:
        terminal.send("web\r")
        await terminal.wait_for(
            lambda: "web" in chat.history and terminal.ui.waiting_for_input,
            "ordinary response with web tools disabled",
        )
        assert not chat.web_enabled
        assert not any(
            isinstance(obj, Text)
            and obj.plain.startswith(("Searching the web:", "Fetching page:"))
            for entry in terminal.ui.entries
            for obj in entry.objects
        )

        terminal.send("/enable-web-search-and-web-fetch\r")
        await terminal.wait_for(
            lambda: (
                chat.web_enabled
                and entry_contains(terminal.ui, "enabled for this session")
            ),
            "web tools enabled",
        )
        terminal.send("/model model-b\r")
        await terminal.wait_for(
            lambda: (
                chat.model_name == "model-b"
                and entry_contains(terminal.ui, "Model: model-b")
            ),
            "model switch",
        )
        await terminal.wait_for(
            lambda: "You [model-b] >" in terminal.current_prompt_line(),
            "prompt after model switch",
        )
        assert chat.web_enabled

        terminal.send("/clear\r")
        await terminal.wait_for(
            lambda: (
                chat.cleared == 1
                and entry_contains(terminal.ui, "Conversation cleared")
            ),
            "conversation clear",
        )
        await terminal.wait_for(
            lambda: "You [model-b] >" in terminal.current_prompt_line(),
            "prompt after clear",
        )
        assert chat.web_enabled

        terminal.send("web\r")
        await terminal.wait_for(
            lambda: "web" in chat.history and terminal.ui.waiting_for_input,
            "tool activity completion",
        )
        texts = [
            obj.plain
            for entry in terminal.ui.entries
            for obj in entry.objects
            if isinstance(obj, Text)
        ]
        search_status = next(
            text for text in texts if text.startswith("Searching the web:")
        )
        fetch_status = next(text for text in texts if text.startswith("Fetching page:"))
        assert search_status == "Searching the web: literal <b>query</b>  [31mred [0m"
        assert fetch_status == "Fetching page: https://example.test/a?q=<tag> [2J"
        assert texts.index(search_status) < texts.index(fetch_status)
        status_entry = next(
            entry
            for entry in terminal.ui.entries
            if any(
                isinstance(obj, Text) and "<b>query</b>" in obj.plain
                for obj in entry.objects
            )
        )
        rendered_status = status_entry.render(90).value
        assert "<b>query</b>" in rendered_status
        assert "\x1b[31mred" not in rendered_status
        assert chat.history == ["web"]


@pytest.mark.asyncio
async def test_model_picker_cancel_and_ctrl_c_while_model_list_is_waiting():
    chat = FakeChat()
    chat.list_gate = asyncio.Event()
    async with running_ui(chat, width=80, height=18) as terminal:
        terminal.send("/model\r")
        await terminal.wait_for(
            lambda: chat.list_started.is_set(), "model list request"
        )
        terminal.send("\x03")
        await terminal.wait_for(
            lambda: (
                chat.list_cancelled.is_set()
                and "You [model-a] >" in terminal.current_prompt_line()
            ),
            "model list cancellation recovery",
        )
        assert chat.model_name == "model-a"

        chat.list_gate = None
        terminal.send("/model\r")
        await terminal.wait_for(
            lambda: "Model number or name" in terminal.current_prompt_line(),
            "model picker prompt",
        )
        terminal.send("\r")
        await terminal.wait_for(
            lambda: "You [model-a] >" in terminal.current_prompt_line(),
            "empty model picker cancellation",
        )
        assert chat.model_name == "model-a"

        terminal.send("/model\r")
        await terminal.wait_for(
            lambda: "Model number or name" in terminal.current_prompt_line(),
            "second model picker prompt",
        )
        terminal.send("2\r")
        await terminal.wait_for(
            lambda: (
                chat.model_name == "model-b"
                and entry_contains(terminal.ui, "Model: model-b")
            ),
            "numeric model selection",
        )


@pytest.mark.asyncio
@pytest.mark.asyncio
async def test_idle_ctrl_c_restores_terminal_and_prints_goodbye_once():
    chat = FakeChat()
    async with running_ui(chat, width=72, height=16) as terminal:
        terminal.sync()
        terminal.send("\x03")
        await terminal.wait_for(lambda: terminal.task.done(), "clean application exit")
        raw_terminal = terminal.output_stream.getvalue()
        assert "\x1b[?1049h" in raw_terminal
        assert "\x1b[?1049l" in raw_terminal
        assert terminal.ui.app.is_running is False
        assert terminal.input.closed is False
        assert terminal.console_stream.getvalue().count("Goodbye") == 1


@pytest.mark.asyncio
async def test_cancelled_response_keeps_partial_markdown_and_allows_next_turn():
    chat = FakeChat()
    gate = asyncio.Event()
    chat.responses["cancel me"] = ("# Partial\n\nCANCELLED_PARTIAL_SENTINEL", "final")
    chat.gates["cancel me"] = gate

    async with running_ui(chat, width=72, height=16) as terminal:
        terminal.send("cancel me\r")
        await terminal.wait_for(
            lambda: (
                "CANCELLED_PARTIAL_SENTINEL" in terminal.text()
                and terminal.ui.response_running
            ),
            "partial response before interrupt",
        )
        terminal.send("\x03")
        await terminal.wait_for(
            lambda: not terminal.ui.response_running and terminal.ui.waiting_for_input,
            "response interruption",
        )
        assert "CANCELLED_PARTIAL_SENTINEL" in terminal.text()
        assert chat.history == []
        assert chat.model_name == "model-a"

        terminal.send("after cancel\r")
        await terminal.wait_for(
            lambda: "after cancel" in chat.history and terminal.ui.waiting_for_input,
            "new turn after cancellation",
        )


@pytest.mark.asyncio
@pytest.mark.parametrize("shutdown", ["input-eof", "run-cancel"])
async def test_shutdown_during_stream_flushes_partial_and_cleans_up(shutdown):
    chat = FakeChat()
    gate = asyncio.Event()
    partial = "# Active shutdown\n\nSHUTDOWN_PARTIAL_SENTINEL"
    chat.responses["blocked"] = (partial, partial + "\n\nNEVER_SENT")
    chat.gates["blocked"] = gate

    async with running_ui(chat, width=72, height=16) as terminal:
        terminal.send("blocked\r")
        await terminal.wait_for(
            lambda: (
                terminal.ui.response_running
                and "SHUTDOWN_PARTIAL_SENTINEL" in terminal.text()
            ),
            "active response before shutdown",
        )

        if shutdown == "input-eof":
            terminal.input.close()
            await terminal.wait_for(
                lambda: terminal.task.done(), "EOF shuts down the full-screen app"
            )
            assert terminal.task.exception() is None
        else:
            terminal.task.cancel()
            with pytest.raises(asyncio.CancelledError):
                await terminal.task

        assert terminal.ui.app.is_running is False
        assert terminal.ui.response_running is False
        assert terminal.ui._controller_task is not None
        assert terminal.ui._controller_task.done()
        assert (
            terminal.console_stream.getvalue().count("SHUTDOWN_PARTIAL_SENTINEL") == 1
        )
        assert "NEVER_SENT" not in terminal.console_stream.getvalue()
        if shutdown == "run-cancel":
            assert terminal.input.closed is False


@pytest.mark.asyncio
async def test_failure_recovers_and_final_transcript_is_written_once_with_rich_style():
    chat = FakeChat()
    response = long_markdown()
    chat.responses["long final"] = (response, response)
    chat.errors["bad"] = RuntimeError("provider detail must stay private")

    async with running_ui(chat, width=64, height=14) as terminal:
        terminal.send("bad\r")
        await terminal.wait_for(
            lambda: (
                "Request failed" in terminal.text() and terminal.ui.waiting_for_input
            ),
            "request error recovery",
        )
        assert (
            "provider detail must stay private"
            not in terminal.console_stream.getvalue()
        )

        terminal.send("long final\r")
        await terminal.wait_for(
            lambda: "long final" in chat.history and terminal.ui.waiting_for_input,
            "long final response",
        )
        terminal.send("\x03")
        await terminal.wait_for(lambda: terminal.task.done(), "final UI exit")

    final_output = terminal.console_stream.getvalue()
    assert final_output.count("FINAL_TRANSCRIPT_SENTINEL") == 1
    assert final_output.count("CODE_BLOCK_SENTINEL") == 1
    assert final_output.count("TABLE_SENTINEL") == 1
    assert "provider detail must stay private" not in final_output
    assert "\x1b[" in final_output
    assert final_output.count("Goodbye") == 1
    assert has_bold_text_in_output(
        final_output, width=64, height=14, needle="BOLD_STYLE_SENTINEL"
    )


@pytest.mark.asyncio
async def test_failure_before_first_chunk_removes_empty_response_and_recovers():
    chat = FakeChat()
    chat.before_chunk_errors["bad before first chunk"] = RuntimeError(
        "private auth detail"
    )

    async with running_ui(chat, width=72, height=16) as terminal:
        terminal.send("bad before first chunk\r")
        await terminal.wait_for(
            lambda: (
                "Request failed" in terminal.text()
                and "You [model-a] >" in terminal.current_prompt_line()
            ),
            "friendly error and next prompt",
        )
        assert not terminal.ui.response_running
        assert not any(
            isinstance(obj, Markdown) and not obj.markup
            for entry in terminal.ui.entries
            for obj in entry.objects
        )
        assert "private auth detail" not in terminal.text()

        terminal.send("recovered turn\r")
        await terminal.wait_for(
            lambda: (
                "recovered turn" in chat.history
                and "You [model-a] >" in terminal.current_prompt_line()
            ),
            "successful turn after early stream failure",
        )
        assert any(
            isinstance(obj, Markdown) and obj.markup
            for entry in terminal.ui.entries
            for obj in entry.objects
        )
        terminal.send("\x03")
        await terminal.wait_for(lambda: terminal.task.done(), "recovered UI exit")


@pytest.mark.asyncio
async def test_bracketed_paste_keeps_escape_and_tab_literal_in_user_transcript():
    chat = FakeChat()
    payload = "literal\tTAB and ESC:\x1b[2J"

    async with running_ui(chat, width=72, height=16) as terminal:
        terminal.send("\x1b[200~" + payload + "\x1b[201~\r")
        await terminal.wait_for(
            lambda: (
                chat.history == [payload]
                and "You [model-a] >" in terminal.current_prompt_line()
            ),
            "bracketed paste accepted as prompt data",
        )
        escaped_echo = "You [model-a] > literal\\tTAB and ESC:\\x1b[2J"
        user_entry = next(
            entry
            for entry in terminal.ui.entries
            if any(
                isinstance(obj, Text) and obj.plain.startswith("You [model-a] > ")
                for obj in entry.objects
            )
        )
        echo = next(obj.plain for obj in user_entry.objects if isinstance(obj, Text))
        assert echo == escaped_echo
        assert "\x1b[2J" not in echo
        terminal.send("\x03")
        await terminal.wait_for(lambda: terminal.task.done(), "pasted prompt UI exit")


@pytest.mark.asyncio
async def test_markdown_links_render_readably_live_and_remain_hyperlinked_in_scrollback():
    chat = FakeChat()
    linked_markdown = "# Links\n\n[Official docs](https://example.test/document)"
    chat.responses["links"] = (linked_markdown, linked_markdown)

    async with running_ui(chat, width=44, height=16) as terminal:
        terminal.send("links\r")
        await terminal.wait_for(
            lambda: "links" in chat.history and terminal.ui.waiting_for_input,
            "linked response completion",
        )
        await terminal.wait_for(
            lambda: (
                "Official docs" in terminal.text()
                and "https://example.test/document" in terminal.text()
            ),
            "readable live link label and URL",
        )
        live_text = terminal.text()
        assert "8;id=" not in live_text
        assert "8;;" not in live_text
        assert "\x1b]8;" not in terminal.output_stream.getvalue()
        markdown_entry = next(
            entry
            for entry in terminal.ui.entries
            for obj in entry.objects
            if isinstance(obj, Markdown)
        )
        assert markdown_entry.objects[0].hyperlinks is True

        terminal.dimensions[:] = [16, 32]
        terminal.screen.resize(lines=16, columns=32)
        terminal.ui.app.invalidate()
        await terminal.wait_for(
            lambda: (
                terminal.ui.transcript_window.render_info is not None
                and terminal.ui.transcript_window.render_info.window_width == 32
            ),
            "narrow linked transcript resize",
        )
        await terminal.wait_for(
            lambda: (
                "Official docs" in terminal.text()
                and "https://example.test/document" in terminal.text()
            ),
            "readable wrapped link after resize",
        )
        terminal.send("\x03")
        await terminal.wait_for(lambda: terminal.task.done(), "linked UI exit")

    final_output = terminal.console_stream.getvalue()
    assert "Official docs" in final_output
    assert "https://example.test/document" in final_output
    assert "\x1b]8;" in final_output


@pytest.mark.asyncio
async def test_command_palette_lists_and_filters_commands_live():
    chat = FakeChat()
    async with running_ui(chat, width=90, height=22) as terminal:
        terminal.send("/")
        await terminal.wait_for(
            lambda: terminal.ui.buffer.complete_state is not None, "command palette"
        )
        assert len(terminal.ui.buffer.complete_state.completions) == 5
        await terminal.wait_for(
            lambda: all(
                command in terminal.text()
                for command in (
                    "/start",
                    "/clear",
                    "/exit",
                    "/model",
                    "/enable-web-search-and-web-fetch",
                )
            ),
            "all commands listed",
        )

        terminal.send("c")
        await terminal.wait_for(
            lambda: (
                terminal.ui.buffer.complete_state is not None
                and [
                    completion.text
                    for completion in terminal.ui.buffer.complete_state.completions
                ]
                == ["/clear"]
            ),
            "palette filters as the user types",
        )
        await terminal.wait_for(
            lambda: "Clear conversation and transcript" in terminal.text(),
            "candidate description",
        )

        # No match hides the palette; deleting reveals it again (prompt_toolkit
        # only auto-completes after insertion, so the UI rebuilds it on shrink).
        terminal.send("z")
        await terminal.wait_for(
            lambda: terminal.ui.buffer.complete_state is None, "palette hidden"
        )
        terminal.send("\x7f")
        await terminal.wait_for(
            lambda: (
                terminal.ui.buffer.complete_state is not None
                and [
                    completion.text
                    for completion in terminal.ui.buffer.complete_state.completions
                ]
                == ["/clear"]
            ),
            "palette rebuilt after deletion",
        )


@pytest.mark.asyncio
async def test_command_palette_selection_and_tab_complete_run_commands():
    chat = FakeChat()
    async with running_ui(chat, width=90, height=22) as terminal:
        terminal.send("/cl")
        await terminal.wait_for(
            lambda: terminal.ui.buffer.complete_state is not None, "command palette"
        )
        terminal.send("\x1b[B")  # Down arrow highlights/inserts the match
        await terminal.wait_for(
            lambda: terminal.ui.buffer.text == "/clear", "selection applied"
        )
        terminal.send("\r")
        await terminal.wait_for(lambda: chat.cleared == 1, "/clear ran")
        assert terminal.ui.buffer.text == ""

        terminal.send("/mo")
        await terminal.wait_for(
            lambda: terminal.ui.buffer.complete_state is not None, "second palette"
        )
        terminal.send("\t")
        await terminal.wait_for(
            lambda: terminal.ui.buffer.text == "/model", "Tab completes"
        )
