"""CLI entry point."""

import asyncio
import os
import sys

from loguru import logger
from rich.console import Console

from ai_chat.chat import create_chat
from ai_chat.terminal import create_session, run_chat
from ai_chat.ui import run_terminal


def main() -> None:
    logger.remove()
    logger.add(
        lambda message: sys.stderr.write(str(message)),
        level="WARNING",
        format="{level}: {message}",
    )
    console = Console()
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
