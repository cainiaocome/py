"""Resolve Ollama Cloud credentials from the environment and ``.env`` files.

The Docker launcher (``scripts/py``) and the native builds must find
credentials the same way: values already in the environment win, then the
current directory's ``.env``, then the home directory's ``.env``.
``AI_CHAT_ENV_FILE`` selects one explicit file instead of those defaults.

Only a safe subset of shell syntax is supported: blank lines, ``#`` comments,
an optional leading ``export``, ``KEY=value`` pairs, and single- or
double-quoted values. Unlike the Docker launcher, the native parser never
executes the file and never expands variables.
"""

from __future__ import annotations

import os
import re
from collections.abc import Mapping, MutableMapping
from pathlib import Path

CREDENTIAL_NAMES = ("OLLAMA_API_KEY", "OLLAMA_MODEL")

_KEY = re.compile(r"[A-Za-z_][A-Za-z0-9_]*")


class ConfigurationError(RuntimeError):
    """An explicitly requested configuration file does not exist."""


def is_configured(environ: Mapping[str, str]) -> bool:
    """Return True when both credentials hold a non-blank value."""
    return all(environ.get(name, "").strip() for name in CREDENTIAL_NAMES)


def parse_env_file(path: Path) -> dict[str, str]:
    """Parse the supported ``.env`` subset, with later assignments winning."""
    try:
        text = path.read_text(encoding="utf-8")
    except FileNotFoundError:
        return {}

    values: dict[str, str] = {}
    for raw_line in text.splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith(("export ", "export\t")):
            line = line[len("export") :].strip()
        key, separator, value = line.partition("=")
        if not separator:
            continue
        key = key.strip()
        if not _KEY.fullmatch(key):
            continue
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        values[key] = value
    return values


def load_configuration(
    environ: MutableMapping[str, str] | None = None,
    *,
    cwd: Path | None = None,
    home: Path | None = None,
) -> None:
    """Fill the credential variables in *environ* from the environment/files.

    Values already present and non-blank are never overwritten, matching the
    Docker launcher. A blank value in the current directory's file does not
    stop a later file from filling it. Raises :class:`ConfigurationError` when
    ``AI_CHAT_ENV_FILE`` names a file that does not exist.
    """
    target = os.environ if environ is None else environ
    if is_configured(target):
        return

    working_directory = Path.cwd() if cwd is None else Path(cwd)
    explicit = target.get("AI_CHAT_ENV_FILE", "").strip()
    if explicit:
        explicit_path = Path(explicit)
        if not explicit_path.is_absolute():
            explicit_path = working_directory / explicit_path
        if not explicit_path.is_file():
            raise ConfigurationError(f"Configuration file not found: {explicit_path}")
        _apply(target, explicit_path)
        return

    _apply(target, working_directory / ".env")
    if is_configured(target):
        return

    home_directory = _home_directory(target, home)
    if home_directory is None:
        return
    home_path = home_directory / ".env"
    if home_path != working_directory / ".env":
        _apply(target, home_path)


def _apply(environ: MutableMapping[str, str], path: Path) -> None:
    """Merge *path* into *environ*, keeping existing non-blank values."""
    if not path.is_file():
        return
    for key, value in parse_env_file(path).items():
        if not environ.get(key, "").strip():
            environ[key] = value


def _home_directory(environ: Mapping[str, str], home: Path | None) -> Path | None:
    if home is not None:
        return Path(home)
    configured = environ.get("HOME", "").strip()
    if configured:
        return Path(configured)
    try:
        return Path.home()
    except RuntimeError:
        return None
