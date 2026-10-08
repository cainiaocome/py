"""Exercise the launcher without pulling images or requiring a Docker daemon."""

import json
import os
import subprocess
import sys
from pathlib import Path

LAUNCHER = Path(__file__).resolve().parents[1] / "scripts" / "py"


def environment(tmp_path):
    docker = tmp_path / "docker"
    docker.write_text(
        f"#!{sys.executable}\n"
        "import json, os, sys\n"
        "with open(os.environ['CAPTURE'], 'w') as f: json.dump(sys.argv[1:], f)\n"
    )
    docker.chmod(0o755)
    env = dict(
        os.environ,
        PATH=f"{tmp_path}:{os.environ['PATH']}",
        CAPTURE=str(tmp_path / "args.json"),
    )
    for name in ("AI_CHAT_IMAGE", "AI_CHAT_ENV_FILE", "OLLAMA_API_KEY", "OLLAMA_MODEL"):
        env.pop(name, None)
    return env


def test_launcher_mount_and_runtime_overrides(tmp_path):
    env = environment(tmp_path)
    config = tmp_path / "private config.env"
    config.write_text("OLLAMA_API_KEY='file-key'\n")
    env.update(
        AI_CHAT_ENV_FILE=str(config),
        OLLAMA_API_KEY="private-key",
        OLLAMA_MODEL="model-name",
    )
    subprocess.run([str(LAUNCHER)], cwd=tmp_path, env=env, check=True)
    args = json.loads((tmp_path / "args.json").read_text())
    assert args[:6] == ["run", "--rm", "--pull", "always", "-it", "--user"]
    assert f"type=bind,source={config},target=/app/.env,readonly" in args
    assert "OLLAMA_API_KEY" in args and "OLLAMA_MODEL" in args
    assert "private-key" not in " ".join(args)
    assert args[-1] == "ghcr.io/cainiaocome/py:latest"


def test_launcher_image_override(tmp_path):
    env = environment(tmp_path)
    env["AI_CHAT_IMAGE"] = "ghcr.io/cainiaocome/py:v1.2.3"
    subprocess.run([str(LAUNCHER)], cwd=tmp_path, env=env, check=True)
    args = json.loads((tmp_path / "args.json").read_text())
    assert args[-1] == env["AI_CHAT_IMAGE"]


def test_launcher_missing_explicit_config(tmp_path):
    env = environment(tmp_path)
    env["AI_CHAT_ENV_FILE"] = str(tmp_path / "missing.env")
    result = subprocess.run(
        [str(LAUNCHER)], env=env, capture_output=True, text=True, check=False
    )
    assert result.returncode == 1
    assert "Configuration file not found" in result.stderr
    assert not (tmp_path / "args.json").exists()
