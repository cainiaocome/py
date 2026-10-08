"""Terminal-level regression tests for streamed Rich Markdown output.

Rich Live redraws use cursor movement and erasure sequences that StringIO
assertions cannot observe. These tests feed the real rendered bytes into a
small terminal emulator and inspect both its visible screen and scrollback.
"""

import asyncio
import re
from io import StringIO

import pyte
import pytest
from rich.console import Console
from rich.markdown import Markdown

from ai_chat import terminal
from ai_chat.terminal import render_response

ACTIVITY = "Searching the web…"
SHORT_MARKDOWN = """\
# Scrollback check

**BOLD_STYLE_SENTINEL**

A short paragraph with `inline code`.
"""


def long_markdown(section_count: int = 14) -> str:
    sections = ["# Long scrollback check", ""]
    for index in range(1, section_count + 1):
        sections.extend(
            [
                f"## Section {index:02d}",
                "",
                (
                    f"Section marker {index:02d} has **BOLD_STYLE_SENTINEL** and "
                    "a deliberately long sentence that wraps at narrow terminal "
                    "widths while the response is still arriving."
                ),
                "",
                f"- first list item {index:02d}",
                f"- second list item {index:02d}",
                "",
                "```python",
                f'value_{index:02d} = "code marker {index:02d}"',
                f"print(value_{index:02d})",
                "```",
                "",
                "| key | value |",
                "| --- | --- |",
                f"| row {index:02d} | table marker {index:02d} |",
                "",
            ]
        )
    sections.append("FINAL_MARKDOWN_SENTINEL")
    return "\n".join(sections)


class StreamingChat:
    def __init__(self, chunks: list[str], error: BaseException | None = None):
        self.chunks = chunks
        self.error = error

    async def stream(self, _prompt, *, on_activity=None):
        for index, chunk in enumerate(self.chunks):
            yield chunk
            if index == 0 and on_activity:
                on_activity(ACTIVITY)
        if self.error is not None:
            raise self.error


class DeterministicLive(terminal.Live):
    """Use Rich's real Live renderer while refreshing only on each update."""

    def __init__(self, *args, **kwargs):
        kwargs["auto_refresh"] = False
        super().__init__(*args, **kwargs)

    def update(self, renderable, *, refresh=False):
        super().update(renderable, refresh=True)


def capture_console(width: int, height: int) -> tuple[Console, StringIO]:
    output = StringIO()
    return (
        Console(
            file=output,
            force_terminal=True,
            color_system="standard",
            width=width,
            height=height,
        ),
        output,
    )


def emulate_terminal(output: str, width: int, height: int) -> pyte.HistoryScreen:
    # A PTY normally maps output LF to CRLF. StringIO does not apply that
    # terminal driver behavior, so reproduce it before parsing ANSI sequences.
    pty_output = re.sub(r"(?<!\r)\n", "\r\n", output)
    screen = pyte.HistoryScreen(width, height, history=2000)
    pyte.Stream(screen).feed(pty_output)
    return screen


def cell_rows(screen: pyte.HistoryScreen):
    history = list(screen.history.top)
    visible = [screen.buffer[row] for row in range(screen.lines)]
    return history + visible


def row_text(row, width: int) -> str:
    return "".join(row[column].data for column in range(width)).rstrip()


def terminal_rows(screen: pyte.HistoryScreen) -> list[str]:
    rows = [row_text(row, screen.columns) for row in cell_rows(screen)]
    return rows


def response_rows(rows: list[str]) -> list[str]:
    start = rows.index("PREVIOUS_TURN_SENTINEL") + 1
    end = rows.index("NEXT_TURN_SENTINEL", start)
    response = [row for row in rows[start:end] if ACTIVITY not in row]
    while response and not response[0].strip():
        response.pop(0)
    while response and not response[-1].strip():
        response.pop()
    return response


def has_bold_text(screen: pyte.HistoryScreen, text: str) -> bool:
    for row in cell_rows(screen):
        chars = [row[column] for column in range(screen.columns)]
        rendered = "".join(char.data for char in chars)
        offset = rendered.find(text)
        if offset >= 0 and all(
            char.bold for char in chars[offset : offset + len(text)]
        ):
            return True
    return False


def expected_static_rows(markdown: str, width: int, height: int) -> list[str]:
    console, output = capture_console(width, height)
    console.print("PREVIOUS_TURN_SENTINEL", markup=False)
    console.print(Markdown(markdown))
    console.print("NEXT_TURN_SENTINEL", markup=False)
    rows = terminal_rows(emulate_terminal(output.getvalue(), width, height))
    return response_rows(rows)


async def render_in_terminal(
    monkeypatch,
    markdown: str,
    width: int,
    height: int,
    *,
    error: BaseException | None = None,
):
    monkeypatch.setattr(terminal, "Live", DeterministicLive)
    console, output = capture_console(width, height)
    console.print("PREVIOUS_TURN_SENTINEL", markup=False)
    # Cumulative snapshots model the stream interface used by Chat.stream.
    chunks = [markdown[: max(1, len(markdown) * index // 8)] for index in range(1, 9)]
    caught = None
    try:
        await render_response(StreamingChat(chunks, error), "question", console)
    except (RuntimeError, asyncio.CancelledError) as exc:
        caught = exc
    console.print("NEXT_TURN_SENTINEL", markup=False)
    return emulate_terminal(output.getvalue(), width, height), caught, output.getvalue()


@pytest.mark.parametrize("width,height", [(40, 8), (80, 12)])
@pytest.mark.parametrize(
    "markdown", [SHORT_MARKDOWN, long_markdown()], ids=["short", "multi-page"]
)
async def test_markdown_scrollback_matches_a_single_static_render(
    monkeypatch, width, height, markdown
):
    screen, caught, raw_output = await render_in_terminal(
        monkeypatch, markdown, width, height
    )

    assert caught is None
    rows = terminal_rows(screen)
    assert response_rows(rows) == expected_static_rows(markdown, width, height)
    activity_rows = [row for row in rows if ACTIVITY in row]
    assert len(activity_rows) == 1
    assert has_bold_text(screen, "BOLD_STYLE_SENTINEL")
    assert screen.cursor.hidden is False
    assert rows.count("PREVIOUS_TURN_SENTINEL") == 1
    assert rows.count("NEXT_TURN_SENTINEL") == 1
    assert "\x1b[?25l" not in raw_output or "\x1b[?25h" in raw_output


@pytest.mark.parametrize(
    "error", [RuntimeError("stream failed"), asyncio.CancelledError()]
)
async def test_partial_markdown_survives_failure_and_restores_cursor(
    monkeypatch, error
):
    partial = (
        long_markdown(section_count=4)
        + "\n\n**BOLD_STYLE_SENTINEL**\n\nPARTIAL_SENTINEL"
    )
    screen, caught, _raw_output = await render_in_terminal(
        monkeypatch, partial, 40, 8, error=error
    )

    assert type(caught) is type(error)
    rows = terminal_rows(screen)
    flattened = "\n".join(rows)
    assert flattened.count("PARTIAL_SENTINEL") == 1
    assert flattened.count("PREVIOUS_TURN_SENTINEL") == 1
    assert flattened.count("NEXT_TURN_SENTINEL") == 1
    assert has_bold_text(screen, "BOLD_STYLE_SENTINEL")
    assert screen.cursor.hidden is False


async def test_redirected_markdown_is_written_once_without_terminal_controls(
    monkeypatch,
):
    monkeypatch.setattr(terminal, "Live", DeterministicLive)
    output = StringIO()
    console = Console(file=output, force_terminal=False, width=80)
    markdown = "# Redirected\n\n**BOLD_STYLE_SENTINEL**\n\nONCE_SENTINEL"

    await render_response(StreamingChat([markdown[:15], markdown]), "question", console)

    text = output.getvalue()
    assert "\x1b[" not in text
    assert text.count("ONCE_SENTINEL") == 1
    assert text.count("BOLD_STYLE_SENTINEL") == 1
