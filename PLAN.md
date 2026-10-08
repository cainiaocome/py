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

Remaining: local Docker/PTY checks, commit/push, Actions publication and published
launcher check. Preserve original untracked AGENTS.md and docs/spec.md. Do not
modify actual home files or private .env. No system packages installed.
