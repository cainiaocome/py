# AI CLI Chat — opt-in web, scrollback and configuration lookup

Goal: web tools disabled until /enable-web-search-and-web-fetch; fix long
streaming Markdown scrollback with regression tests; launcher searches CWD
.env then $HOME/.env, stopping once required environment values are available.

Design: preserve web opt-in across /clear and /model, fresh sessions disabled;
factory excludes tools and browsing instructions until enabled. Earlier valid
configuration values win; later files fill missing/blank fields; retain explicit
AI_CHAT_ENV_FILE override. Python remains environment-only, Docker gets env
variables only. Do not modify actual home files or private checkout .env.

Root cause found: Rich Live(vertical_overflow="visible") redraws content beyond
terminal height, but cursor-up cannot erase content in scrollback. Fix uses a
bounded, transient Markdown tail preview and prints full Markdown once on
completion/interruption. Preview must be bounded even on Live.stop() (Rich
forces visible overflow on exit). Add pyte only as dev dependency for real
terminal-state/scrollback regression tests, including wrapped/styled Markdown.

Implemented: web opt-in command and Tab completion, capability persistence across
/clear and /model; CWD/home lookup with nonblank precedence and relative explicit
file override; bounded preview with full/partial Markdown printed once. README
updated. pyte is a dev-only dependency; no production dependency upgrades.

Validation: baseline terminal tests reproduced four failures with old renderer.
New tests compare ANSI output in a terminal emulator to static Markdown, retaining
internal blank lines and bold style, at 40x8 and 80x12, with headings/code/lists/
tables/wrapping, activity, multi-page failure/cancellation, and redirected output.
make lint test passed72 tests; sh -n scripts/py and git diff --check passed. Own
source/diff review complete. Latest disabled-switch test passed focused9 cases.
Live Cloud: default session made zero tool calls; enabling retained context and
then executed search+fetch with activity and a citation.

Implementation pushed as 106e2bb on main. GitHub Actions tests and the
amd64/arm64 image build passed:
https://github.com/cainiaocome/py/actions/runs/37823546952
Published ghcr.io/cainiaocome/py:latest at digest
sha256:2903e9c47624ca90f78897ae02d5c031a8d7e2ab09edfb534a0339d2e15a48d9.

Validation passed: 72 tests, lint, shell syntax, diff check, and local Docker
build. Baseline scrollback tests reproduced four failures before the renderer
fix. The production image passed a 40x8 pyte scrollback probe with unique early
and final Markdown markers and bold styling; pyte is absent from the image and
MarkdownPreview imports successfully. Local PTY verified web disabled by
default, Tab completion/enabling, idempotence, and enabled tools surviving
/model and /clear, followed by successful search/fetch with activity and a
cited response. Published scripts/py passed from the repository directory with
host Ollama variables unset, loading CWD .env; it showed disabled startup,
enabled by Tab-completed command, and successful search/fetch after /model and
/clear. The live container had zero mounts, UID/GID 10001:10001, both required
environment variables present without exposing values, and no /app/.env.
No remaining implementation work or blockers. Preserve original untracked
AGENTS.md and docs/spec.md. Do not modify actual home files or private .env.
No system packages installed.
