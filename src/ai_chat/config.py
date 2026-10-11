"""Resolve Ollama Cloud credentials from the environment and ``.env`` files.

The Docker launcher (``scripts/py``) and the native builds must find
credentials the same way: values already in the environment win, then the
current directory's ``.env``, then the home directory's ``.env``.
``AI_CHAT_ENV_FILE`` selects one explicit file instead of those defaults.

Only a safe subset of shell syntax is supported: blank lines, ``#`` comments,
an optional leading ``export``, ``KEY=value`` pairs, single- or double-quoted
values, and a comment after a value. It never executes the file and never
expands variables.

The container launcher *sources* the file with a POSIX shell, so this parser reads
that subset the same way rather than literally: ``KEY=secret # note`` is the
credential ``secret`` followed by a comment, not ``secret # note``. Getting that
wrong sends ``secret # note`` as an API key, which is a 401 rather than a typo.
``tests/test_config.py`` asserts the two agree by running the same lines through
``sh``.
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
        value = _parse_value(value)
        values[key] = value
    return values


def _parse_value(raw: str) -> str:
    """Read the right-hand side of an assignment the way a POSIX shell reads it.

    Quotes are removed and joined with the surrounding text, and a ``#`` that
    starts a word begins a comment. The container launcher sources the file, so
    this has to agree with it. Where a shell would reject the line -- unquoted
    spaces, for instance -- this parser is deliberately more forgiving.
    """
    value = raw.strip()
    text = ""
    significant = 0

    def append(segment: str, *, quoted: bool) -> None:
        nonlocal text, significant
        text += segment
        if quoted or segment.strip(" \t"):
            significant = len(text)

    index = 0
    while index < len(value):
        character = value[index]
        if character == "#" and (index == 0 or value[index - 1] in " \t"):
            return text[:significant]
        if character in "\"'":
            closing = _closing_quote(value, index)
            if closing < 0:
                # An unterminated quote is not ours to interpret; keep it as written.
                append(value[index:].rstrip(" \t"), quoted=False)
                return text[:significant]
            append(_unquote(value[index : closing + 1]), quoted=True)
            index = closing + 1
            continue
        append(character, quoted=False)
        index += 1
    return text[:significant]


def _closing_quote(value: str, start: int) -> int:
    """Return the index of the quote closing the one at *start*, or -1.

    Only a double quote takes backslash escapes.
    """
    quote = value[start]
    index = start + 1
    while index < len(value):
        if quote == '"' and value[index] == "\\":
            index += 2
            continue
        if value[index] == quote:
            return index
        index += 1
    return -1


def _unquote(segment: str) -> str:
    """Strip the surrounding quotes and resolve the escapes they allow."""
    inner = segment[1:-1]
    if segment[0] == "'":
        return inner
    text = ""
    index = 0
    while index < len(inner):
        if (
            inner[index] == "\\"
            and index + 1 < len(inner)
            and inner[index + 1] in '"\\$`'
        ):
            index += 1
            text += inner[index]
            index += 1
            continue
        text += inner[index]
        index += 1
    return text


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
