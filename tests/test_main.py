import importlib

import pytest

from ai_chat.main import main

module = importlib.import_module("ai_chat.main")


@pytest.fixture(autouse=True)
def isolated_config(monkeypatch, tmp_path):
    """Keep the user's real environment and ``.env`` files out of these tests."""
    home = tmp_path / "home"
    home.mkdir()
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv("HOME", str(home))
    monkeypatch.delenv("AI_CHAT_ENV_FILE", raising=False)


def route_to_legacy(monkeypatch, captured):
    monkeypatch.setattr(module.sys.stdin, "isatty", lambda: False)
    monkeypatch.setattr(module.sys.stdout, "isatty", lambda: False)
    monkeypatch.setattr(
        module,
        "create_chat",
        lambda key, model: captured.update(key=key, model=model) or object(),
    )

    async def legacy(*args):
        captured["legacy"] = True

    monkeypatch.setattr(module, "run_chat", legacy)


@pytest.mark.parametrize("missing", ["OLLAMA_API_KEY", "OLLAMA_MODEL", "both"])
def test_missing_configuration(monkeypatch, capsys, missing):
    monkeypatch.setenv("OLLAMA_API_KEY", "test-key")
    monkeypatch.setenv("OLLAMA_MODEL", "test-model")
    for name in ("OLLAMA_API_KEY", "OLLAMA_MODEL"):
        if missing in (name, "both"):
            monkeypatch.delenv(name)
    with pytest.raises(SystemExit) as exc:
        main()
    assert exc.value.code == 1
    output = capsys.readouterr()
    assert "ERROR: Missing required environment variables:" in output.err
    for name in ("OLLAMA_API_KEY", "OLLAMA_MODEL"):
        assert (name in output.err) == (missing in (name, "both"))
    assert "test-key" not in output.err


def test_whitespace_configuration(monkeypatch, capsys):
    monkeypatch.setenv("OLLAMA_API_KEY", " \t")
    monkeypatch.setenv("OLLAMA_MODEL", "valid-model")
    with pytest.raises(SystemExit):
        main()
    assert "OLLAMA_API_KEY" in capsys.readouterr().err


@pytest.mark.parametrize(
    ("input_tty", "output_tty", "expected"),
    [
        (True, True, "pinned"),
        (False, True, "legacy"),
        (True, False, "legacy"),
        (False, False, "legacy"),
    ],
)
def test_terminal_routing(monkeypatch, input_tty, output_tty, expected):
    monkeypatch.setenv("OLLAMA_API_KEY", "test-key")
    monkeypatch.setenv("OLLAMA_MODEL", "test-model")
    monkeypatch.setattr(module.sys.stdin, "isatty", lambda: input_tty)
    monkeypatch.setattr(module.sys.stdout, "isatty", lambda: output_tty)
    chat = object()
    session = object()
    monkeypatch.setattr(module, "create_chat", lambda *args: chat)
    monkeypatch.setattr(module, "create_session", lambda: session)
    calls = []

    async def pinned(actual_chat, console):
        assert actual_chat is chat
        calls.append("pinned")

    async def legacy(actual_chat, actual_session, console):
        assert actual_chat is chat and actual_session is session
        calls.append("legacy")

    monkeypatch.setattr(module, "run_terminal", pinned)
    monkeypatch.setattr(module, "run_chat", legacy)
    main()
    assert calls == [expected]


def test_env_file_supplies_configuration(monkeypatch, tmp_path):
    monkeypatch.delenv("OLLAMA_API_KEY", raising=False)
    monkeypatch.delenv("OLLAMA_MODEL", raising=False)
    (tmp_path / ".env").write_text(
        "export OLLAMA_API_KEY=file-key\nOLLAMA_MODEL='file-model'\n"
    )
    captured = {}
    route_to_legacy(monkeypatch, captured)

    main()

    assert captured["key"] == "file-key"
    assert captured["model"] == "file-model"
    assert captured["legacy"]


def test_explicit_env_file_overrides_default_locations(monkeypatch, tmp_path):
    monkeypatch.delenv("OLLAMA_API_KEY", raising=False)
    monkeypatch.delenv("OLLAMA_MODEL", raising=False)
    (tmp_path / ".env").write_text("OLLAMA_API_KEY=cwd-key\nOLLAMA_MODEL=cwd-model\n")
    config = tmp_path / "private config.env"
    config.write_text("OLLAMA_API_KEY=explicit-key\nOLLAMA_MODEL=explicit-model\n")
    monkeypatch.setenv("AI_CHAT_ENV_FILE", str(config))
    captured = {}
    route_to_legacy(monkeypatch, captured)

    main()

    assert captured["key"] == "explicit-key"
    assert captured["model"] == "explicit-model"


def test_version_flag(capsys):
    with pytest.raises(SystemExit) as exc:
        main(["--version"])
    assert exc.value.code == 0
    assert "ai-chat" in capsys.readouterr().out


def test_unknown_arguments_are_ignored(monkeypatch):
    monkeypatch.setenv("OLLAMA_API_KEY", "test-key")
    monkeypatch.setenv("OLLAMA_MODEL", "test-model")
    captured = {}
    route_to_legacy(monkeypatch, captured)

    main(["-q", "--capture=no", "tests/test_main.py", "some-positional"])

    assert captured["legacy"]
