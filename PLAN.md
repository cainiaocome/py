# AI CLI Chat

Implemented: Ollama Cloud terminal chat, streaming Markdown, session history,
Vim/multiline input, active model, /model selection and command Tab completion.
Distribution: Dockerfile, Docker-only scripts/py, GitHub Actions publishing
linux/amd64+linux/arm64 images to ghcr.io/cainiaocome/py:latest.

In progress: environment-only Python configuration. Python no longer loads
.env and logs missing/blank configuration via Loguru. Removed python-dotenv.
Launcher sources optional trusted shell-format .env, preserves exported
variable precedence, validates key and model before Docker, and passes values
via --env names only; no .env mount. Runtime uses default non-root image user.
README and configuration/launcher tests updated.

Validation passed: make lint test (25 tests), sh -n scripts/py, final source
and diff review, git diff --check. Lockfile change only removes python-dotenv.
Local Docker build succeeded. PTY startup from the local image using exported
host variables (sourced from private .env, with no container mount) displayed
the configured model and exited cleanly with /exit. A no-config container had
no /app/.env, reported both required variables missing, and exited 1.
Remaining: commit/push the eight intended files, watch GitHub publication, and
smoke-test the updated published launcher from /tmp, including its env-only
configuration and no-mount behavior.
Keep original untracked AGENTS.md and docs/spec.md untouched. .env is ignored
and private; never stage it. No system packages installed.
