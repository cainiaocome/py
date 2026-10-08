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

Publication complete: implementation committed/pushed as 2657dc2 on main.
GitHub Actions test/image jobs passed:
https://github.com/cainiaocome/py/actions/runs/37810147851
Published ghcr.io/cainiaocome/py:latest (amd64+arm64), digest
sha256:69eae7a913f147be9094b88a3e99aec774e7c048539838b5b57fe4be739bef8d.
Pulls passed anonymously and with existing Docker credentials (not modified).
Final PTY check through scripts/py passed GHCR pull, startup from /tmp with the
checkout's private .env, live /model discovery, /clear and /exit. No remaining
implementation work or blockers. Original untracked AGENTS.md and docs/spec.md
remain untouched. .env is private and Git-ignored; never stage it.
