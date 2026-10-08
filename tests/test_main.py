import pytest

from ai_chat.main import main


@pytest.mark.parametrize("missing", ["OLLAMA_API_KEY", "OLLAMA_MODEL", "both"])
def test_missing_configuration(monkeypatch, tmp_path, capsys, missing):
    monkeypatch.chdir(tmp_path)
    # The app must ignore .env even when it contains complete configuration.
    (tmp_path / ".env").write_text("OLLAMA_API_KEY=file-key\nOLLAMA_MODEL=file-model\n")
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
    import importlib

    module = importlib.import_module("ai_chat.main")
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
