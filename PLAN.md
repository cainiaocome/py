# AI CLI Chat

Implemented: Python/uv CLI with Pydantic AI and Ollama Cloud, streamed Rich
Markdown, Vim/multiline input, session history, /clear, /exit, /model picker and
direct switching, active model display, command Tab completion and error recovery.

Container distribution implemented: multi-stage Dockerfile (Python 3.13,
locked production dependencies, non-root runtime without uv), allowlisted
.dockerignore, executable scripts/py, and GitHub Actions publishing
linux/amd64 + linux/arm64 to ghcr.io/cainiaocome/py. main publishes latest;
v* tags publish versions; PRs test/build without publishing. Launcher always
pulls and mounts .env read-only, preserving dotenv quoting and exported env
precedence; no credentials in the image or command arguments.

Validation: make lint test (14 tests), sh -n scripts/py, local Docker build,
runtime inspection (non-root, installed application, no uv/.env/.git), local
Docker PTY startup and /clear + /exit. Prior live Cloud streaming/context and
model discovery/picker tests passed. No system packages installed.

Remaining: commit/push required application and distribution files, monitor
Actions publication, pull GHCR image using existing Docker credentials and
smoke-test scripts/py. Check anonymous GHCR access and document if package
visibility needs user action. Original untracked AGENTS.md and docs/spec.md
remain untouched. .env is private and Git-ignored; never stage it.
