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
container. It reads the checkout's `.env` even when launched from another
working directory. The launcher sources this optional file as trusted POSIX
shell configuration (`KEY=value`, quotes, comments and `export` are supported).
Exported `OLLAMA_API_KEY` and `OLLAMA_MODEL` override file values. Both must be
nonblank after loading; otherwise the launcher logs an error and exits before
starting Docker. You can also skip the file and export both variables directly.
Only environment variables are passed to the container; `.env` is never mounted.

Optional overrides:

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
- Enter sends the message; Alt+Enter adds a newline (Esc then Enter also works).
- The active model is shown at startup, in the input prompt, and above each response.
- `/model` lists Cloud models; choose a number or enter a name (Enter cancels).
- `/model <name>` switches directly. Switching retains conversation context and
  applies only to this session; `.env` is unchanged. Model availability is checked
  by Ollama when you send your next message.
- Tab completes `/clear`, `/exit`, and `/model`; repeated Tab cycles matches.
- `/clear` resets model context; input history remains available within the session.
- `/exit`, Ctrl+D, or Ctrl+C at the prompt exits.
- Ctrl+C during a response cancels it and returns to the prompt.

Responses stream as Markdown. Failed or interrupted responses are not added to
conversation history. Messages and input history live only in memory and are
not saved between sessions. Request errors show a short message without logging
message content or credentials.

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

`make help` lists commands. Tests use Pydantic AI's test models and do not need a
Cloud API key. `uv.lock` pins the resolved dependencies for reproducible installs.

Build and try the container locally:

```sh
make image
```

The launcher always pulls, so a local-only image is best tested directly:

```sh
# Export OLLAMA_API_KEY and OLLAMA_MODEL first.
docker run --rm -it -e OLLAMA_API_KEY -e OLLAMA_MODEL ai-chat:local
```
