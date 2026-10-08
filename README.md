# AI Chat

A minimal Python 3.12+ terminal chat client for Ollama Cloud. Uses Pydantic AI
for streaming and session history, prompt_toolkit for Vim input, Rich for
Markdown, and Loguru for error logging. No local Ollama server is required.

## Run

Install and start [Docker](https://docs.docker.com/get-started/get-docker/), then
from this checkout:

```sh
cp .env.example .env
# Set your Ollama Cloud API key and model in .env.
./scripts/py
```

No host Python, uv, or Python dependencies are required. The launcher pulls
`ghcr.io/cainiaocome/py:latest` on every start and runs an interactive, temporary
container. It checks exported `OLLAMA_API_KEY` and `OLLAMA_MODEL` first. If
settings are missing or blank, it searches for `.env` in this order:

1. The current working directory.
2. Your home directory (`$HOME/.env`).

Loading stops as soon as both settings are nonblank. Earlier nonblank values
win; later files fill only missing settings. Files use trusted POSIX shell
syntax (`KEY=value`, quotes, comments and `export` are supported). If settings
remain missing, the launcher logs an error and exits before starting Docker.
Only environment variables are passed to the container; `.env` is never mounted.

Optional overrides (`AI_CHAT_ENV_FILE` selects a single file instead of the
normal search when settings are incomplete):

```sh
AI_CHAT_ENV_FILE=/path/to/chat.env ./scripts/py
AI_CHAT_IMAGE=ghcr.io/cainiaocome/py:v1.0.0 ./scripts/py
```

Get a key from https://ollama.com/settings/keys. `OLLAMA_MODEL` is the Cloud
model name (default example: `gpt-oss:20b`). Requests use
`https://ollama.com/v1`. The Python application reads environment variables
only and logs an error if either required value is missing or blank. Keep
`.env` private; it is ignored by Git and loaded only by the launcher.

- Vim editing is enabled by default; press `i` to enter insert mode, Esc for normal mode.
  The cursor is a beam in Insert mode, a block in Normal mode, and an underline
  in Replace mode (requires a terminal that supports cursor shape changes).
- The input stays pinned below the conversation and expands for multiline drafts.
- Enter sends the message; Alt+Enter adds a newline (Esc then Enter also works).
- You can draft the next message while the assistant responds. Enter preserves
  that draft until the response finishes; Ctrl+C interrupts the response.
- PgUp/PgDn or the mouse wheel scroll the conversation without moving input.
  End while scrolled up, or Ctrl+End, returns to following new output. Submitting
  a new message also resumes following; incoming tokens alone do not.
- The active model is shown at startup, in the input prompt, and above each response.
- `/model` lists Cloud models; choose a number or enter a name (Enter cancels).
- `/model <name>` switches directly. Switching retains conversation context and
  applies only to this session; `.env` is unchanged. Model availability is checked
  by Ollama when you send your next message.
- `/enable-web-search-and-web-fetch` enables both web tools for this session.
- Tab completes all commands, including the web-enable command; repeated Tab cycles matches.
- `/clear` resets model context; input history remains available within the session.
- `/exit`, Ctrl+D, or Ctrl+C at the prompt exits.
- Ctrl+C during a response cancels it and returns to the prompt.

Interactive terminals use a full-screen layout with a scrollable Rich Markdown
transcript and a fixed input area. Streaming and interrupted replies remain in
that transcript. On exit the terminal is restored and the complete transcript
is printed once into normal shell scrollback. Redirected output uses the
standard scrolling interface and prints complete responses once.

Failed or interrupted responses are not added to model conversation history.
Messages and input history live only in memory and are not saved between
sessions. Request errors show a short message without logging message content
or credentials.

## Web search and page fetching

Web tools are disabled when a session starts. Enable them with:

```text
/enable-web-search-and-web-fetch
```

The assistant can then search and read pages using Ollama Cloud's
[web search and fetch APIs](https://docs.ollama.com/capabilities/web-search).
Both tools use your existing `OLLAMA_API_KEY`; no additional configuration or
local browser is needed. Use a Cloud model that supports function/tool calling.
Enabling preserves conversation context and survives `/model` and `/clear`.
Each new session starts with the tools disabled again.

Ask naturally, for example:

- “Search for the latest Ollama release and cite the official announcement.”
- “Read https://docs.ollama.com/capabilities/web-search and summarize its APIs.”

During tool execution the terminal shows the actual query or URL, for example:

```text
Searching the web: latest Ollama release
Fetching page: https://docs.ollama.com/capabilities/web-search
```

Tool arguments are displayed as literal text, with terminal control characters
removed. The model is instructed to link sources in its Markdown answers and
to report web errors honestly. Tool execution and returned sources remain in
session history; `/clear` resets them together with the conversation.

Each web request has a 30-second deadline. Search defaults to five results
(maximum ten), and each search/fetch result is bounded to 20,000 content
characters with a truncation flag. Page results also include at most 20 links.
There is no per-turn cap on tool calls or model requests; research continues
until the model finishes or you interrupt it with Ctrl+C. Failed or interrupted
turns do not update history. Authentication errors, rate limits, timeouts,
unavailable pages, and malformed responses are returned to the model as short
error messages.
Credentials and page contents are not logged. Python still reads configuration
only from environment variables; the launcher alone loads the optional `.env`.

## Container publishing

`.github/workflows/image.yml` runs tests and lint, then builds Linux amd64 and
arm64 images. Pushes to `main` publish `latest` and a commit tag; `v*` Git tags
publish a matching version tag. Manual runs are supported. Pull requests build
without publishing. GHCR authentication uses the workflow's `GITHUB_TOKEN`
with `packages: write`; no registry secret is required. The image is linked to
this repository. For anonymous pulls, the GHCR package must have public
visibility (new packages may initially be private). Private packages require
an existing Docker credential with package read access.

Only allowlisted application files enter the Docker build context; `.env`, Git
metadata, tests and local caches are excluded. The runtime contains Python and
the installed application, runs as a non-root user by default, and has no uv.

## Development

For local Python development, install [uv](https://docs.astral.sh/uv/):

```sh
uv sync --locked
export OLLAMA_API_KEY=your-ollama-cloud-api-key
export OLLAMA_MODEL=gpt-oss:20b
uv run ai-chat
```

```sh
make test
make lint
```

`make help` lists commands. Tests use Pydantic AI's test models, mocked HTTP
responses, and a terminal emulator for Markdown scrollback and pinned-layout
regression checks. They cover real keyboard input, resize/scroll behavior,
streaming drafts, Vim cursors, commands, interruption, cleanup, and literal tool
details. They do not need a Cloud API key. `uv.lock` pins resolved dependencies
for reproducible installs.

Build and try the container locally:

```sh
make image
```

The launcher always pulls, so a local-only image is best tested directly:

```sh
# Export OLLAMA_API_KEY and OLLAMA_MODEL first.
docker run --rm -it -e OLLAMA_API_KEY -e OLLAMA_MODEL ai-chat:local
```
