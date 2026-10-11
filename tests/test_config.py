import shutil
import subprocess
from pathlib import Path

import pytest

from ai_chat import config


def test_parse_env_file_supports_common_syntax(tmp_path):
    path = tmp_path / ".env"
    path.write_text(
        "# a comment\n"
        "\n"
        "export OLLAMA_API_KEY=exported\n"
        "OLLAMA_MODEL='quoted model'\n"
        'OLLAMA_MODEL="last wins"\n'
        "not a pair\n"
        "1BAD=value\n"
        "  SPACED  =  trimmed  \n"
    )

    values = config.parse_env_file(path)

    assert values["OLLAMA_API_KEY"] == "exported"
    assert values["OLLAMA_MODEL"] == "last wins"
    assert values["SPACED"] == "trimmed"
    assert "1BAD" not in values
    assert "not a pair" not in values


def test_parse_env_file_missing_is_empty(tmp_path):
    assert config.parse_env_file(tmp_path / "absent.env") == {}


def test_environment_values_are_never_overwritten(tmp_path):
    home = tmp_path / "home"
    home.mkdir()
    (tmp_path / ".env").write_text("OLLAMA_API_KEY=cwd\nOLLAMA_MODEL=cwd\n")
    (home / ".env").write_text("OLLAMA_API_KEY=home\nOLLAMA_MODEL=home\n")
    environ = {"OLLAMA_API_KEY": "environment", "OLLAMA_MODEL": "environment"}

    config.load_configuration(environ, cwd=tmp_path, home=home)

    assert environ["OLLAMA_API_KEY"] == "environment"
    assert environ["OLLAMA_MODEL"] == "environment"


def test_configured_environment_skips_files(monkeypatch, tmp_path):
    calls = []
    monkeypatch.setattr(config, "parse_env_file", lambda path: calls.append(path) or {})
    environ = {"OLLAMA_API_KEY": "key", "OLLAMA_MODEL": "model"}

    config.load_configuration(environ, cwd=tmp_path, home=tmp_path)

    assert calls == []


def test_current_directory_wins_and_home_fills_gaps(tmp_path):
    home = tmp_path / "home"
    home.mkdir()
    (tmp_path / ".env").write_text("OLLAMA_API_KEY=cwd-key\n")
    (home / ".env").write_text("OLLAMA_API_KEY=home-key\nOLLAMA_MODEL=home-model\n")
    environ = {}

    config.load_configuration(environ, cwd=tmp_path, home=home)

    assert environ["OLLAMA_API_KEY"] == "cwd-key"
    assert environ["OLLAMA_MODEL"] == "home-model"


def test_blank_file_value_does_not_block_home(tmp_path):
    home = tmp_path / "home"
    home.mkdir()
    (tmp_path / ".env").write_text("OLLAMA_API_KEY=\nOLLAMA_MODEL= \t\n")
    (home / ".env").write_text("OLLAMA_API_KEY=home-key\nOLLAMA_MODEL=home-model\n")
    environ = {}

    config.load_configuration(environ, cwd=tmp_path, home=home)

    assert environ["OLLAMA_API_KEY"] == "home-key"
    assert environ["OLLAMA_MODEL"] == "home-model"


def test_blank_environment_value_is_filled_from_current_directory(tmp_path):
    (tmp_path / ".env").write_text("OLLAMA_API_KEY=cwd-key\nOLLAMA_MODEL=cwd-model\n")
    environ = {"OLLAMA_API_KEY": " \t", "OLLAMA_MODEL": ""}

    config.load_configuration(environ, cwd=tmp_path, home=tmp_path / "missing")

    assert environ["OLLAMA_API_KEY"] == "cwd-key"
    assert environ["OLLAMA_MODEL"] == "cwd-model"


def test_explicit_env_file_is_read_relative_to_cwd(tmp_path):
    config_file = tmp_path / "private config.env"
    config_file.write_text("OLLAMA_MODEL=explicit\n")
    environ = {"AI_CHAT_ENV_FILE": "private config.env", "OLLAMA_API_KEY": "key"}

    config.load_configuration(environ, cwd=tmp_path, home=tmp_path)

    assert environ["OLLAMA_MODEL"] == "explicit"


def test_missing_explicit_env_file_raises(tmp_path):
    environ = {"AI_CHAT_ENV_FILE": "missing.env"}

    with pytest.raises(config.ConfigurationError, match="Configuration file not found"):
        config.load_configuration(environ, cwd=tmp_path, home=tmp_path)


def test_home_is_taken_from_environment_when_not_supplied(tmp_path):
    (tmp_path / ".env").write_text("OLLAMA_MODEL=cwd\n")
    home = tmp_path / "home"
    home.mkdir()
    (home / ".env").write_text("OLLAMA_API_KEY=from-home\n")
    environ = {"HOME": str(home)}

    config.load_configuration(environ, cwd=tmp_path)

    assert environ["OLLAMA_API_KEY"] == "from-home"
    assert environ["OLLAMA_MODEL"] == "cwd"


def test_relative_explicit_file_resolves_from_working_directory(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "config.env").write_text("OLLAMA_MODEL=explicit\n")
    environ = {"AI_CHAT_ENV_FILE": "config.env", "OLLAMA_API_KEY": "key"}

    config.load_configuration(environ, home=Path(tmp_path))

    assert environ["OLLAMA_MODEL"] == "explicit"


@pytest.mark.parametrize(
    ("line", "expected"),
    [
        ("OLLAMA_API_KEY=plain", "plain"),
        ("OLLAMA_API_KEY=plain   ", "plain"),
        ('OLLAMA_API_KEY="quoted value"', "quoted value"),
        ("OLLAMA_API_KEY='quoted value'", "quoted value"),
        ("OLLAMA_API_KEY=secret # ollama cloud key", "secret"),
        ('OLLAMA_API_KEY="secret"  # key', "secret"),
        ("OLLAMA_API_KEY='secret' # key", "secret"),
        ("OLLAMA_API_KEY=sec#ret", "sec#ret"),
        ('OLLAMA_API_KEY="sec#ret"', "sec#ret"),
        ('OLLAMA_API_KEY="  padded  "', "  padded  "),
        ("OLLAMA_API_KEY=", ""),
        ("OLLAMA_API_KEY=# nothing", ""),
        ("OLLAMA_API_KEY=pre\"mid\"'post'", "premidpost"),
        ('OLLAMA_API_KEY="a\\"b"', 'a"b'),
        ("OLLAMA_API_KEY='a\\b'", "a\\b"),
        ('OLLAMA_API_KEY="unclosed', '"unclosed'),
        ("OLLAMA_API_KEY='secret'# key", "secret# key"),
    ],
)
def test_parse_env_file_reads_values_the_way_a_shell_does(tmp_path, line, expected):
    """A comment or quote after a value must not become part of the value."""
    path = tmp_path / ".env"
    path.write_text(line + "\n")

    assert config.parse_env_file(path)["OLLAMA_API_KEY"] == expected


@pytest.mark.parametrize(
    "line",
    [
        "OLLAMA_API_KEY=plain",
        "OLLAMA_API_KEY=secret # ollama cloud key",
        'OLLAMA_API_KEY="secret" # key',
        "OLLAMA_API_KEY='secret' # key",
        'OLLAMA_API_KEY="quoted value"',
        "OLLAMA_API_KEY='quoted value'",
        "OLLAMA_API_KEY=sec#ret",
        'OLLAMA_API_KEY="sec#ret"',
        'OLLAMA_API_KEY="  padded  "',
        "OLLAMA_API_KEY=",
        "OLLAMA_API_KEY=pre\"mid\"'post'",
        'OLLAMA_API_KEY="a\\"b"',
        "export OLLAMA_API_KEY=exported",
    ],
)
def test_parse_env_file_agrees_with_a_shell(tmp_path, line):
    """The container launcher sources the file, so the two readers must agree.

    This is the test that catches a native client sending ``secret # note`` as an
    API key while the container sends ``secret``.
    """
    if shutil.which("sh") is None:
        pytest.skip("no POSIX shell available to compare against")

    path = tmp_path / ".env"
    path.write_text(f"# leading comment\n{line}\nOLLAMA_MODEL=m\n")

    exported = subprocess.run(
        ["sh", "-c", '. ./.env; printf %s "$OLLAMA_API_KEY"'],
        cwd=tmp_path,
        capture_output=True,
        text=True,
        check=True,
    ).stdout

    assert config.parse_env_file(path)["OLLAMA_API_KEY"] == exported
