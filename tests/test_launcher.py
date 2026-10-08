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
    home = tmp_path / "home"
    home.mkdir()
    values = dict(
        os.environ,
        PATH=f"{tmp_path}:{os.environ['PATH']}",
        HOME=str(home),
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


def test_complete_environment_skips_malformed_and_side_effect_files(
    tmp_path, launcher, env
):
    (tmp_path / ".env").write_text("if then\n")
    marker = tmp_path / "home-was-sourced"
    (Path(env["HOME"]) / ".env").write_text(f"touch '{marker}'\n")

    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)

    assert not marker.exists()
    assert captured(tmp_path)["key"] == "environment-key"


def test_current_directory_file_satisfies_config_and_skips_home(
    tmp_path, launcher, env
):
    (tmp_path / ".env").write_text("OLLAMA_API_KEY='cwd-key'\nOLLAMA_MODEL=cwd-model\n")
    marker = tmp_path / "home-was-sourced"
    (Path(env["HOME"]) / ".env").write_text(f"touch '{marker}'\n")
    env.pop("OLLAMA_API_KEY")
    env.pop("OLLAMA_MODEL")

    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)

    data = captured(tmp_path)
    assert data["key"] == "cwd-key"
    assert data["model"] == "cwd-model"
    assert not marker.exists()


def test_missing_current_directory_file_falls_back_to_home(tmp_path, launcher, env):
    env.pop("OLLAMA_API_KEY")
    env.pop("OLLAMA_MODEL")
    (Path(env["HOME"]) / ".env").write_text(
        "OLLAMA_API_KEY=home-key\nOLLAMA_MODEL=home-model\n"
    )

    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)

    data = captured(tmp_path)
    assert data["key"] == "home-key"
    assert data["model"] == "home-model"


def test_partial_current_directory_file_precedes_home_fallback(tmp_path, launcher, env):
    (tmp_path / ".env").write_text("OLLAMA_API_KEY=cwd-key\n")
    (Path(env["HOME"]) / ".env").write_text(
        "OLLAMA_API_KEY=home-key\nOLLAMA_MODEL=home-model\n"
    )
    env.pop("OLLAMA_API_KEY")
    env.pop("OLLAMA_MODEL")

    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)

    data = captured(tmp_path)
    assert data["key"] == "cwd-key"
    assert data["model"] == "home-model"


def test_environment_values_precede_files_and_lookup_stops_when_complete(
    tmp_path, launcher, env
):
    (tmp_path / ".env").write_text("OLLAMA_API_KEY=cwd-key\nOLLAMA_MODEL=cwd-model\n")
    (Path(env["HOME"]) / ".env").write_text(
        "OLLAMA_API_KEY=home-key\nOLLAMA_MODEL=home-model\n"
    )
    env["OLLAMA_MODEL"] = "  \t"

    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)

    data = captured(tmp_path)
    assert data["key"] == "environment-key"
    assert data["model"] == "cwd-model"


def test_whitespace_environment_values_are_filled_from_home(tmp_path, launcher, env):
    env["OLLAMA_API_KEY"] = " \t\n"
    env["OLLAMA_MODEL"] = "  "
    (Path(env["HOME"]) / ".env").write_text(
        "OLLAMA_API_KEY=home-key\nOLLAMA_MODEL=home-model\n"
    )

    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)

    data = captured(tmp_path)
    assert data["key"] == "home-key"
    assert data["model"] == "home-model"


def test_launcher_explicit_config_overrides_defaults_but_environment_wins(
    tmp_path, launcher, env
):
    config = tmp_path / "private config.env"
    config.write_text("export OLLAMA_API_KEY=file-key\nOLLAMA_MODEL=file-model\n")
    env["AI_CHAT_ENV_FILE"] = str(config)
    env.pop("OLLAMA_MODEL")
    (tmp_path / ".env").write_text("OLLAMA_MODEL=cwd-model\n")

    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)

    data = captured(tmp_path)
    assert data["key"] == "environment-key"
    assert data["model"] == "file-model"


def test_launcher_resolves_relative_explicit_config_from_working_directory(
    tmp_path, launcher, env
):
    (tmp_path / "relative config.env").write_text("OLLAMA_MODEL=file-model\n")
    env["AI_CHAT_ENV_FILE"] = "relative config.env"
    env.pop("OLLAMA_MODEL")

    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)

    data = captured(tmp_path)
    assert data["key"] == "environment-key"
    assert data["model"] == "file-model"


def test_launcher_missing_explicit_config_does_not_fall_back(tmp_path, launcher, env):
    env.pop("OLLAMA_API_KEY")
    env.pop("OLLAMA_MODEL")
    env["AI_CHAT_ENV_FILE"] = str(tmp_path / "missing.env")
    (tmp_path / ".env").write_text("OLLAMA_API_KEY=cwd-key\nOLLAMA_MODEL=cwd-model\n")
    (Path(env["HOME"]) / ".env").write_text(
        "OLLAMA_API_KEY=home-key\nOLLAMA_MODEL=home-model\n"
    )

    result = subprocess.run(
        [str(launcher)],
        cwd=tmp_path,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )

    assert result.returncode == 1
    assert "Configuration file not found" in result.stderr
    assert not (tmp_path / "args.json").exists()


def test_complete_environment_skips_missing_explicit_config(tmp_path, launcher, env):
    env["AI_CHAT_ENV_FILE"] = str(tmp_path / "missing.env")

    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)

    assert captured(tmp_path)["key"] == "environment-key"


@pytest.mark.parametrize("name", ["OLLAMA_API_KEY", "OLLAMA_MODEL"])
@pytest.mark.parametrize("value", [None, "", " \t\n"])
def test_launcher_rejects_missing_configuration_before_docker(
    tmp_path, launcher, env, name, value
):
    if value is None:
        env.pop(name)
    else:
        env[name] = value

    result = subprocess.run(
        [str(launcher)],
        cwd=tmp_path,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )

    assert result.returncode == 1
    assert f"ERROR: Missing required environment variable: {name}" in result.stderr
    assert not (tmp_path / "args.json").exists()
    assert "environment-key" not in result.stderr


def test_launcher_image_override_and_no_secrets_or_file_mounts(tmp_path, launcher, env):
    env["AI_CHAT_IMAGE"] = "ghcr.io/cainiaocome/py:v1.2.3"

    subprocess.run([str(launcher)], cwd=tmp_path, env=env, check=True)

    data = captured(tmp_path)
    args = data["args"]
    assert args[-1] == env["AI_CHAT_IMAGE"]
    assert "--pull" in args and "always" in args
    assert "--mount" not in args
    assert "--env-file" not in args
    assert "environment-key" not in " ".join(args)
    assert "environment-model" not in " ".join(args)
