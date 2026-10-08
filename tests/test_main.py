import pytest

from ai_chat.main import main


def test_missing_configuration(monkeypatch, tmp_path, capsys):
    monkeypatch.chdir(tmp_path)
    monkeypatch.delenv("OLLAMA_API_KEY", raising=False)
    monkeypatch.delenv("OLLAMA_MODEL", raising=False)
    with pytest.raises(SystemExit) as exc:
        main()
    assert exc.value.code == 1
    assert "OLLAMA_API_KEY and OLLAMA_MODEL" in capsys.readouterr().out
