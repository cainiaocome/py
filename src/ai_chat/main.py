"""CLI entry point."""

import asyncio
import os
import sys

from dotenv import load_dotenv
from loguru import logger
from rich.console import Console

from ai_chat.chat import create_chat
from ai_chat.terminal import create_session, run_chat


def main() -> None:
    load_dotenv(dotenv_path=".env")
    logger.remove()
    logger.add(sys.stderr, level="WARNING", format="{level}: {message}")
    console = Console()
    api_key = os.environ.get("OLLAMA_API_KEY", "").strip()
    model = os.environ.get("OLLAMA_MODEL", "").strip()
    if not api_key or not model:
        console.print(
            "[red]Set OLLAMA_API_KEY and OLLAMA_MODEL in .env or your environment.[/red]"
        )
        raise SystemExit(1)
    try:
        asyncio.run(run_chat(create_chat(api_key, model), create_session(), console))
    except KeyboardInterrupt:
        console.print("[dim]Goodbye.[/dim]")


if __name__ == "__main__":
    main()
