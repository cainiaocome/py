# AI CLI Chat — web tools

Goal: approved Ollama Cloud search/fetch capability, retaining existing CLI,
environment-only Python configuration and Docker-only launcher distribution.

Implemented: async WebTools in src/ai_chat/web.py, existing API key, fixed
Ollama endpoints, safe structured errors, 30s total HTTP deadline. Search
5 results by default/max10, 20k total snippet chars; fetch20k chars/max20 links;
truncation flags. httpx declared directly without package version changes.

Agent factory registers tools at startup and after /model switches. Agent.iter
streams every model step and executes mixed text/tool responses to completion.
Per-turn budgets:10 tool calls/15 model requests. Terminal shows tool activity
and limit errors; successful tool messages retained in native history, failed
or cancelled turns leave history unchanged. Model instructed to cite actual
sources and treat retrieved text as untrusted data. README updated.

Implementation pushed as b442b46 on main. GitHub Actions tests and the
amd64/arm64 image build passed:
https://github.com/cainiaocome/py/actions/runs/37818993707
Published ghcr.io/cainiaocome/py:latest at digest
sha256:af1721dd33277c3e3aece6a62d0c2fd919f8ba44f93f8b7c6353895870397c47.

Validation passed: make lint test (56 tests), git diff --check, source/diff
review, and local Docker build. Local PTY search/fetch showed both tool
activities and returned both endpoint names with the official source link;
/exit returned 0. Tests cover HTTP/auth/body/schema/errors/deadline/content bounds,
search→fetch→answer, mixed text/tools, context, cancellation, loop limits,
model switching and terminal activity/recovery. Live Cloud search returned 2
results; fetch returned official docs; configured model used both tools and
streamed a cited answer, then recalled verified endpoints on a follow-up.

Published scripts/py passed from /tmp with host Ollama variables unset, loading
the checkout .env. It showed Searching and Fetching activity, returned both
endpoint names with the docs citation, and exited cleanly. Live container
inspection confirmed user 10001:10001, zero mounts, both environment variables
present without exposing values, /app/.env absent, web/httpx modules available,
and dotenv absent. No remaining implementation work or blockers. Keep .env
private and ignored; preserve original untracked AGENTS.md and docs/spec.md.

Vim cursor shapes use prompt_toolkit's ModalCursorShapeConfig: beam in Insert,
block in Normal, underline in Replace. README documents terminal support.
Latest implementation commit 3859609 passed Actions test and image jobs:
https://github.com/cainiaocome/py/actions/runs/37820820892
Published ghcr.io/cainiaocome/py:latest at digest
sha256:05995e7b0ee3cf00caafea15de54ce83543c7f829c8610b045205141f86d6d46.

Validation passed: make lint test (56 tests), shell syntax, diff review,
git diff --check, and local Docker build. Local PTY web-search/fetch invoked
both tools and returned both endpoint names with the docs citation. Published
scripts/py from /tmp passed raw PTY checks for Insert beam → Normal block →
Replace underline → Normal block → Insert beam; /exit returned 0. Launcher
loaded the checkout .env with host Ollama variables unset. No remaining work.
Keep .env private and ignored; preserve original untracked AGENTS.md and
docs/spec.md. No system packages installed.
