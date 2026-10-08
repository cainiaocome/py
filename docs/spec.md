# AI CLI Chat — Specification

## Goal

Build a minimal terminal-based AI chat application using Python.

## Tech Stack

- **Python 3.12+**
- **Pydantic AI** — LLM interaction and conversation management
- **Ollama Cloud** — only supported LLM provider for now
- **prompt_toolkit** — interactive terminal input
- **Rich** — formatted terminal output
- **Loguru** — logging
- **uv** — dependency and project management

## Functional Requirements

### 1. LLM Integration

- Use Pydantic AI for all LLM interactions.
- Support Ollama Cloud only.
- Configure API key and model via environment variables (`.env`).
- Maintain conversation history across turns within a session.
- Support streaming responses.

### 2. Terminal Input

- Use `prompt_toolkit` for user input.
- Enable Vim key bindings by default.
- Support multiline input.
- `Enter` submits the message; `Alt+Enter` inserts a newline.
- Maintain input history during the session.
- Support `/exit` and `/clear` commands.

### 3. Terminal Output

- Use Rich to render assistant responses.
- Render Markdown using `rich.markdown.Markdown`.
- Stream responses to the terminal as they arrive.
- Clearly distinguish user messages from assistant messages.
- Display errors without crashing the application.

## Non-Goals

- No tool calling, MCP, or agent skills.
- No web interface.
- No conversation persistence across sessions.
- No multiple providers.
- No complex configuration system.

## Project Structure

```text
ai-chat/
├── pyproject.toml
├── .env.example
├── README.md
└── src/
    └── ai_chat/
        ├── __init__.py
        ├── main.py
        ├── chat.py
        └── terminal.py
```

## Implementation Guidelines

- Prefer simple, readable, async Python code.
- Use Pydantic AI's native conversation history and streaming APIs.
- Avoid unnecessary abstractions and dependencies.
- Keep LLM logic separate from terminal UI.
- Handle Ctrl+C gracefully.
- Provide a CLI entry point: `ai-chat`.
- Include basic tests for conversation history and CLI commands.

## Acceptance Criteria

1. `uv sync` installs all dependencies.
2. `uv run ai-chat` starts the application.
3. Users can chat with an Ollama Cloud model.
4. Conversation context is preserved between messages.
5. Assistant responses stream and render as Markdown.
6. Vim editing works by default.
7. `/clear` resets the conversation; `/exit` quits.
