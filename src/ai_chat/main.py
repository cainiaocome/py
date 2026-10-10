"""CLI entry point."""

from __future__ import annotations

import argparse
import asyncio
import os
import sys
from collections.abc import Sequence
from importlib.metadata import PackageNotFoundError
from importlib.metadata import version as package_version

from loguru import logger
from rich.console import Console

from ai_chat.chat import create_chat
from ai_chat.config import ConfigurationError, load_configuration
from ai_chat.terminal import create_session, run_chat
from ai_chat.ui import run_terminal


def _parse_arguments(argv: Sequence[str] | None) -> None:
    """Handle ``--version``/``--help`` and ignore anything else.

    Unknown arguments are tolerated so embedding (for example, the test suite
    invoking :func:`main`) never trips over a foreign command line.
    """
    try:
        app_version = package_version("ai-chat")
    except PackageNotFoundError:
        app_version = "unknown"
    parser = argparse.ArgumentParser(
        prog="ai-chat",
        description="Minimal streaming terminal chat with Ollama Cloud.",
    )
    parser.add_argument(
        "--version",
        action="version",
        version=f"%(prog)s {app_version}",
    )
    parser.parse_known_args(argv)


def main(argv: Sequence[str] | None = None) -> None:
    _parse_arguments(argv)
    logger.remove()
    logger.add(
        lambda message: sys.stderr.write(str(message)),
        level="WARNING",
        format="{level}: {message}",
    )
    console = Console()
    try:
        load_configuration(os.environ)
    except ConfigurationError as error:
        logger.error("{}", error)
        raise SystemExit(1) from error
    api_key = os.environ.get("OLLAMA_API_KEY", "").strip()
    model = os.environ.get("OLLAMA_MODEL", "").strip()
    missing = [
        name
        for name, value in (("OLLAMA_API_KEY", api_key), ("OLLAMA_MODEL", model))
        if not value
    ]
    if missing:
        logger.error("Missing required environment variables: {}", ", ".join(missing))
        raise SystemExit(1)
    try:
        chat = create_chat(api_key, model)
        if sys.stdin.isatty() and sys.stdout.isatty():
            asyncio.run(run_terminal(chat, console))
        else:
            asyncio.run(run_chat(chat, create_session(), console))
    except KeyboardInterrupt:
        console.print("[dim]Goodbye.[/dim]")


if __name__ == "__main__":
    main()
