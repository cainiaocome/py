import asyncio
from io import StringIO
from types import SimpleNamespace
from unittest.mock import Mock

from prompt_toolkit.enums import EditingMode
from prompt_toolkit.input import DummyInput
from prompt_toolkit.keys import Keys
from prompt_toolkit.output import DummyOutput
from rich.console import Console

from ai_chat.terminal import create_session, key_bindings, run_chat


class Session:
    def __init__(self, *prompts):
        self.prompts = iter(prompts)

    async def prompt_async(self, label):
        prompt = next(self.prompts)
        if isinstance(prompt, BaseException):
            raise prompt
        return prompt


class Chat:
    def __init__(self):
        self.model_name = "old-model"
        self.prompts = []
        self.cleared = 0

    async def list_models(self):
        return ["old-model", "new-model"]

    def switch_model(self, model):
        if " " in model:
            raise ValueError("bad name")
        self.model_name = model

    def clear(self):
        self.cleared += 1

    async def stream(self, prompt, *, on_activity=None):
        self.prompts.append(prompt)
        if prompt == "fail":
            raise RuntimeError("private detail")
        if prompt == "cancel":
            raise asyncio.CancelledError()
        if prompt == "web" and on_activity:
            on_activity("Searching the web…")
            on_activity("Fetching page…")
        if prompt == "limit":
            from pydantic_ai.exceptions import UsageLimitExceeded

            raise UsageLimitExceeded("limit")
        yield "**hello**"


async def test_commands_and_recovery():
    chat = Chat()
    output = StringIO()
    await run_chat(
        chat,
        Session(" ", "/unknown", "fail", "cancel", "hello", "/clear", "/exit"),
        Console(file=output),
    )
    assert chat.prompts == ["fail", "cancel", "hello"]
    assert chat.cleared == 1
    text = output.getvalue()
    assert "Unknown command" in text
    assert "Request failed" in text
    assert "Response interrupted" in text
    assert "private detail" not in text
    assert "Goodbye" in text


async def test_prompt_interrupts_exit():
    for interrupt in (EOFError(), KeyboardInterrupt()):
        output = StringIO()
        await run_chat(Chat(), Session(interrupt), Console(file=output))
        assert "Goodbye" in output.getvalue()


def test_enter_and_alt_enter_bindings():
    bindings = key_bindings()
    buffer = Mock()
    event = SimpleNamespace(current_buffer=buffer)
    bindings.get_bindings_for_keys((Keys.ControlM,))[0].handler(event)
    buffer.validate_and_handle.assert_called_once()
    bindings.get_bindings_for_keys((Keys.Escape, Keys.ControlM))[0].handler(event)
    buffer.insert_text.assert_called_once_with("\n")


def test_vim_multiline_default(monkeypatch):
    from prompt_toolkit.application import create_app_session

    with create_app_session(input=DummyInput(), output=DummyOutput()):
        session = create_session()
    assert session.editing_mode == EditingMode.VI
    assert session.multiline


async def test_model_picker_direct_switch_and_cancel():
    chat = Chat()
    output = StringIO()
    await run_chat(
        chat,
        Session("/model", "2", "hello", "/model other-model", "/model", "", "/exit"),
        Console(file=output),
    )
    assert chat.model_name == "other-model"
    assert chat.prompts == ["hello"]
    assert chat.cleared == 0
    text = output.getvalue()
    assert "Model: old-model" in text
    assert "2. new-model" in text
    assert "Assistant [new-model]" in text
    assert "conversation retained" in text


async def test_model_invalid_selection_and_failure_recover():
    chat = Chat()
    output = StringIO()
    await run_chat(
        chat,
        Session(
            "/model",
            "99",
            "/model bad name",
            "/model",
            KeyboardInterrupt(),
            "hello",
            "/exit",
        ),
        Console(file=output),
    )
    assert chat.model_name == "old-model"
    assert chat.prompts == ["hello"]
    assert "Invalid model number" in output.getvalue()
    assert "Could not select a model" in output.getvalue()
    assert "selection cancelled" in output.getvalue()


def test_command_completion():
    from prompt_toolkit.document import Document

    from ai_chat.terminal import CommandCompleter

    completer = CommandCompleter()

    def complete(text):
        return list(completer.get_completions(Document(text), None))

    assert [item.text for item in complete("/")] == ["/clear", "/exit", "/model"]
    assert [item.text for item in complete("/mo")] == ["/model"]
    assert complete("/mo")[0].start_position == -3
    assert complete("hello") == []
    assert complete("/model other") == []


async def test_tool_activity_and_limit_recovery():
    output = StringIO()
    chat = Chat()
    await run_chat(
        chat, Session("web", "limit", "hello", "/exit"), Console(file=output)
    )
    assert "Searching the web" in output.getvalue()
    assert "Fetching page" in output.getvalue()
    assert "reached its tool or request limit" in output.getvalue()
    assert chat.prompts == ["web", "limit", "hello"]
