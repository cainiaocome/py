"""Interactive input and streamed Markdown output."""

import asyncio

from loguru import logger
from prompt_toolkit import PromptSession
from prompt_toolkit.completion import Completer, Completion
from prompt_toolkit.cursor_shapes import ModalCursorShapeConfig
from prompt_toolkit.enums import EditingMode
from prompt_toolkit.history import InMemoryHistory
from prompt_toolkit.key_binding import KeyBindings
from pydantic_ai.exceptions import UsageLimitExceeded
from rich.console import Console, ConsoleOptions, RenderResult
from rich.live import Live
from rich.markdown import Markdown

from ai_chat.chat import Chat

ENABLE_WEB_COMMAND = "/enable-web-search-and-web-fetch"
COMMANDS = ("/clear", "/exit", "/model", ENABLE_WEB_COMMAND)


class CommandCompleter(Completer):
    def get_completions(self, document, complete_event):
        text = document.text_before_cursor
        if document.cursor_position != len(document.text) or not text.startswith("/"):
            return
        for command in COMMANDS:
            if command.startswith(text):
                yield Completion(command, start_position=-len(text))


def key_bindings() -> KeyBindings:
    bindings = KeyBindings()

    @bindings.add("enter")
    def submit(event):
        event.current_buffer.validate_and_handle()

    @bindings.add("escape", "enter")
    def newline(event):
        event.current_buffer.insert_text("\n")

    @bindings.add("tab")
    def complete(event):
        buffer = event.current_buffer
        if buffer.complete_state:
            buffer.complete_next()
        else:
            buffer.start_completion(select_first=True)

    return bindings


def create_session() -> PromptSession:
    return PromptSession(
        editing_mode=EditingMode.VI,
        cursor=ModalCursorShapeConfig(),
        multiline=True,
        history=InMemoryHistory(),
        completer=CommandCompleter(),
        complete_while_typing=False,
        key_bindings=key_bindings(),
    )


async def run_chat(chat: Chat, session: PromptSession, console: Console) -> None:
    console.print("AI Chat · Ollama Cloud · Model: " + chat.model_name, markup=False)
    console.print(
        "Vim editing · Enter: send · Alt+Enter: newline · Tab: complete · /clear · /exit · /model"
    )
    console.print("Web tools: disabled · " + ENABLE_WEB_COMMAND, markup=False)
    while True:
        try:
            prompt = (await session.prompt_async(f"You [{chat.model_name}] > ")).strip()
        except (EOFError, KeyboardInterrupt):
            break
        if not prompt:
            continue
        if prompt == "/exit":
            break
        if prompt == ENABLE_WEB_COMMAND:
            enabled = chat.enable_web()
            console.print(
                "[dim]Web search and web fetch enabled for this session.[/dim]"
                if enabled
                else "[dim]Web search and web fetch are already enabled.[/dim]"
            )
            continue
        if prompt == "/clear":
            chat.clear()
            console.print("[dim]Conversation cleared.[/dim]")
            continue
        if prompt.split(maxsplit=1)[0] == "/model":
            argument = prompt.removeprefix("/model").strip()
            try:
                await select_model(chat, session, console, argument)
            except (EOFError, KeyboardInterrupt):
                console.print("[dim]Model selection cancelled.[/dim]")
            except asyncio.CancelledError:
                current = asyncio.current_task()
                if current is not None:
                    current.uncancel()
                console.print("[dim]Model selection cancelled.[/dim]")
            except Exception as exc:  # noqa: BLE001 - interactive command boundary
                logger.warning("Model selection failed ({})", type(exc).__name__)
                console.print(
                    "[red]Could not select a model. Try /model <name> or check your connection and key.[/red]"
                )
            continue
        if prompt.startswith("/"):
            console.print(
                "Unknown command. Available: " + ", ".join(COMMANDS),
                style="yellow",
                markup=False,
            )
            continue
        console.print(f"Assistant [{chat.model_name}]", style="bold cyan", markup=False)
        # A separate task lets Ctrl+C cancel one response without exiting the UI.
        task = asyncio.create_task(render_response(chat, prompt, console))
        try:
            await task
        except asyncio.CancelledError:
            current = asyncio.current_task()
            if current is not None:
                current.uncancel()
            console.print("[yellow]Response interrupted.[/yellow]")
        except UsageLimitExceeded:
            console.print(
                "[yellow]Web research stopped: this turn reached its tool or request limit. Try a narrower question.[/yellow]"
            )
        except Exception as exc:  # noqa: BLE001 - recover at the interactive UI boundary
            # Do not log provider exception bodies: they can contain private input.
            logger.warning("Chat request failed ({})", type(exc).__name__)
            console.print(
                "[red]Request failed. Check your connection, API key and model; try again.[/red]"
            )
    console.print("[dim]Goodbye.[/dim]")


class MarkdownPreview:
    """Keep live redraws inside the viewport, including Rich's final refresh."""

    def __init__(self, text: str) -> None:
        self.text = text

    def __rich_console__(
        self, console: Console, options: ConsoleOptions
    ) -> RenderResult:
        lines = console.render_lines(
            Markdown(self.text), options.update(height=None), pad=False, new_lines=True
        )
        limit = max(1, min(12, options.max_height - 2))
        for line in lines[-limit:]:
            yield from line


async def render_response(chat: Chat, prompt: str, console: Console) -> None:
    text = ""
    try:
        with Live(
            MarkdownPreview(""),
            console=console,
            refresh_per_second=12,
            vertical_overflow="crop",
            transient=True,
        ) as live:

            def activity(status: str) -> None:
                live.console.print(status, style="dim", markup=False)

            async for text in chat.stream(prompt, on_activity=activity):
                live.update(MarkdownPreview(text))
    finally:
        # Only this stable render enters scrollback. Also retain partial answers
        # when a request fails or the user interrupts streaming.
        if text:
            console.print(Markdown(text))


async def select_model(
    chat: Chat, session: PromptSession, console: Console, argument: str
) -> None:
    if not argument:
        console.print("Current model: " + chat.model_name, markup=False)
        models = await chat.list_models()
        for index, model in enumerate(models, 1):
            marker = " (current)" if model == chat.model_name else ""
            console.print(f"  {index}. {model}{marker}", markup=False)
        if not models:
            console.print("[dim]No models returned. Enter a model name manually.[/dim]")
        argument = (
            await session.prompt_async("Model number or name (Enter to cancel) > ")
        ).strip()
        if not argument:
            return
        if argument.isdecimal():
            index = int(argument) - 1
            if not 0 <= index < len(models):
                console.print("[yellow]Invalid model number. Model unchanged.[/yellow]")
                return
            argument = models[index]
    chat.switch_model(argument)
    console.print(
        "Model: " + chat.model_name + " · conversation retained", markup=False
    )
