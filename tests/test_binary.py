"""Smoke tests for the standalone binary built by ``make binary``.

These are skipped unless ``dist/ai-chat`` exists, so the normal test run does
not require PyInstaller. The release workflow builds the binary first, then
runs this file on every target platform.
"""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path

import pytest

BINARY = Path(__file__).resolve().parents[1] / "dist" / "ai-chat"
if sys.platform == "win32":
    BINARY = BINARY.with_suffix(".exe")

pytestmark = pytest.mark.skipif(
    not BINARY.exists(), reason="build the binary first: make binary"
)

CONFIG_NAMES = ("OLLAMA_API_KEY", "OLLAMA_MODEL", "AI_CHAT_ENV_FILE")


@pytest.fixture
def home(tmp_path):
    directory = tmp_path / "home"
    directory.mkdir()
    return directory


def run_binary(tmp_path, home, *args, extra_env=None):
    environment = {k: v for k, v in os.environ.items() if k not in CONFIG_NAMES}
    environment["HOME"] = str(home)
    environment.update(extra_env or {})
    return subprocess.run(
        [str(BINARY), *args],
        cwd=tmp_path,
        input="",
        capture_output=True,
        text=True,
        env=environment,
        timeout=60,
        check=False,
    )


def test_version_flag(tmp_path, home):
    result = run_binary(tmp_path, home, "--version")
    assert result.returncode == 0
    assert result.stdout.strip().startswith("ai-chat ")


def test_missing_configuration_exits(tmp_path, home):
    result = run_binary(tmp_path, home)
    assert result.returncode == 1
    assert "Missing required environment variables" in result.stderr


def test_env_file_is_loaded(tmp_path, home):
    (tmp_path / ".env").write_text("OLLAMA_API_KEY=file-key\nOLLAMA_MODEL=file-model\n")

    result = run_binary(tmp_path, home)

    assert result.returncode == 0
    assert "Model: file-model" in result.stdout


def test_explicit_env_file_is_loaded(tmp_path, home):
    config = tmp_path / "private config.env"
    config.write_text("OLLAMA_API_KEY=explicit-key\nOLLAMA_MODEL=explicit-model\n")

    result = run_binary(tmp_path, home, extra_env={"AI_CHAT_ENV_FILE": str(config)})

    assert result.returncode == 0
    assert "Model: explicit-model" in result.stdout
