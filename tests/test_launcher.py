"""Exercise the launcher without pulling images or requiring a Docker daemon."""

import json
import os
import subprocess
import sys
from pathlib import Path

import pytest

LAUNCHER = Path(__file__).resolve().parents[1] / "scripts" / "py"


@pytest.fixture
def launcher(tmp_path):
    # Isolate the default .env location from the real checkout's credentials.
    script = tmp_path / "scripts" / "py"
    script.parent.mkdir()
    script.write_bytes(LAUNCHER.read_bytes())
    script.chmod(0o755)
    return script


@pytest.fixture
def env(tmp_path):
    docker = tmp_path / "docker"
    docker.write_text(
        f"#!{sys.executable}\n"
        "import json, os, sys\n"
        "data = {'args': sys.argv[1:], 'key': os.environ.get('OLLAMA_API_KEY'), "
        "'model': os.environ.get('OLLAMA_MODEL')}\n"
        "with open(os.environ['CAPTURE'], 'w') as f: json.dump(data, f)\n"
    )
    docker.chmod(0o755)
    values = dict(
        os.environ,
        PATH=f"{tmp_path}:{os.environ['PATH']}",
        CAPTURE=str(tmp_path / "args.json"),
        OLLAMA_API_KEY="environment-key",
        OLLAMA_MODEL="environment-model",
    )
    for name in ("AI_CHAT_IMAGE", "AI_CHAT_ENV_FILE"):
        values.pop(name, None)
    return values


def captured(tmp_path):
    return json.loads((tmp_path / "args.json").read_text())


def test_launcher_environment_only(tmp_path, launcher, env):
    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)
    data = captured(tmp_path)
    assert data["key"] == "environment-key"
    assert data["model"] == "environment-model"
    assert data["args"] == [
        "run",
        "--rm",
        "--pull",
        "always",
        "-it",
        "--env",
        f"TERM={env.get('TERM') or 'xterm-256color'}",
        "--env",
        "OLLAMA_API_KEY",
        "--env",
        "OLLAMA_MODEL",
        "ghcr.io/cainiaocome/py:latest",
    ]


def test_launcher_loads_default_file_from_other_directory(tmp_path, launcher, env):
    (tmp_path / ".env").write_text(
        "# Shell-format configuration\nOLLAMA_API_KEY='file key'\nOLLAMA_MODEL=cloud-model\n"
    )
    env.pop("OLLAMA_API_KEY")
    env.pop("OLLAMA_MODEL")
    subprocess.run([str(launcher)], cwd=launcher.parent, env=env, check=True)
    data = captured(tmp_path)
    assert data["key"] == "file key"
    assert data["model"] == "cloud-model"
    assert "--mount" not in data["args"]
    assert "--env-file" not in data["args"]
    assert "file key" not in " ".join(data["args"])


def test_launcher_file_and_environment_precedence(tmp_path, launcher, env):
    config = tmp_path / "private config.env"
    config.write_text("export OLLAMA_API_KEY='file-key'\nOLLAMA_MODEL=file-model\n")
    env["AI_CHAT_ENV_FILE"] = str(config)
    env.pop("OLLAMA_MODEL")
    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)
    data = captured(tmp_path)
    assert data["key"] == "environment-key"
    assert data["model"] == "file-model"


@pytest.mark.parametrize("name", ["OLLAMA_API_KEY", "OLLAMA_MODEL"])
@pytest.mark.parametrize("value", [None, "", " \t\n"])
def test_launcher_rejects_missing_configuration(tmp_path, launcher, env, name, value):
    if value is None:
        env.pop(name)
    else:
        env[name] = value
    result = subprocess.run(
        [str(launcher)], env=env, capture_output=True, text=True, check=False
    )
    assert result.returncode == 1
    assert f"ERROR: Missing required environment variable: {name}" in result.stderr
    assert not (tmp_path / "args.json").exists()
    assert "environment-key" not in result.stderr


def test_launcher_image_override(tmp_path, launcher, env):
    env["AI_CHAT_IMAGE"] = "ghcr.io/cainiaocome/py:v1.2.3"
    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)
    assert captured(tmp_path)["args"][-1] == env["AI_CHAT_IMAGE"]


def test_launcher_missing_explicit_config(tmp_path, launcher, env):
    env["AI_CHAT_ENV_FILE"] = str(tmp_path / "missing.env")
    result = subprocess.run(
        [str(launcher)], env=env, capture_output=True, text=True, check=False
    )
    assert result.returncode == 1
    assert "Configuration file not found" in result.stderr
    assert not (tmp_path / "args.json").exists()
