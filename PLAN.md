# AI CLI Chat

Implemented: Ollama Cloud terminal chat, streaming Markdown, session history,
Vim/multiline input, active model, /model selection and command Tab completion.
Distribution: Dockerfile, Docker-only scripts/py, GitHub Actions publishing
linux/amd64+linux/arm64 images to ghcr.io/cainiaocome/py:latest.

Environment configuration is complete: Python reads environment variables
only and logs missing/blank configuration via Loguru. python-dotenv was removed.
Launcher sources optional trusted shell-format .env, preserves exported
variable precedence, validates key and model before Docker, and passes values
via --env names only; no .env mount. Runtime uses default non-root image user.
README and configuration/launcher tests updated.

Implementation pushed as 76bc1eb on main. GitHub Actions tests and the
amd64/arm64 image build passed:
https://github.com/cainiaocome/py/actions/runs/37815475658
Published ghcr.io/cainiaocome/py:latest at digest
sha256:01b7e01afd8632bee9bcf48865eccdfe02a40a25aeb24ef53288522283b37573.

Validation passed: make lint test (25 tests), sh -n scripts/py, final source
and diff review, git diff --check, and local Docker build. Local PTY startup
with exported host settings and no mount showed the configured model and
exited cleanly; a no-config container had no /app/.env, reported both missing
settings, and exited 1. Published scripts/py worked from /tmp with host
variables unset, loaded the checkout .env, listed 18 models, cleared context,
and exited cleanly. Live container inspection confirmed zero mounts, both
settings present without exposing values, /app/.env absent, and no dotenv
module installed. No remaining implementation work or blockers.
Keep original untracked AGENTS.md and docs/spec.md untouched. .env is ignored
and private; never stage it. No system packages installed.
