# AI CLI Chat — Ctrl+D only scrolls

Previous request complete: empty-prompt scrolling. 111 tests; commit
81b7f14/de462b2, Actions
https://github.com/cainiaocome/py/actions/runs/37856921678, published image digest
sha256:6cd067c1bef45fc5db7c7f2b76705841feb5dc1b3e469538f0069d2c7af2a1ab.

Current goal (user request): Ctrl+D must never exit; it always scrolls the
transcript half a page down in both insert and normal mode, so a user scrolling
cannot accidentally quit at the end. This supersedes the earlier "Ctrl+D exits"
request. The decision is now recorded in `AGENTS.md` so it is not reversed again.

Changes:

- `src/ai_chat/ui.py`: replaced the insert/replace exit-or-scroll and separate
  normal-mode bindings with a single eager Ctrl+D binding that only calls
  `_scroll_forward(_half_page_lines())`. `/exit` and Ctrl+C remain the exit paths.
- `tests/test_ui.py`: renamed the Ctrl+D test to assert it scrolls down and never
  exits (including at the bottom); the idle-exit test covers Ctrl+C only. The
  `VirtualTerminal.close()` teardown now calls `app.exit()` instead of sending
  Ctrl+D, so tests no longer depend on a key binding (suite runtime dropped from
  ~30s to ~8s).
- `AGENTS.md`: added a "Key bindings" section stating Ctrl+D must only scroll and
  must never exit, and to remind the user if this is ever requested again.
- `README.md`: documented the behavior.

Full suite 110 passed, Ruff and diff checks clean.

Validation: commit 5a85401; full suite 110 passed. Actions
https://github.com/cainiaocome/py/actions/runs/37857380433 succeeded. Published
image ghcr.io/cainiaocome/py:latest digest
sha256:e40e95c7934417532575eea23e8f63bfa88cdea012f7ce0e1bbb41d5c90b4607. A
real-PTY run of that image confirmed Ctrl+D scrolls half a page down in insert
and normal mode, is a no-op at the bottom without exiting, preserves a typed
draft, and Ctrl+C remains the clean exit path (exit code 0).

Follow-up docs: README now documents the key bindings in dedicated tables
(`## Key bindings` with editing/mode-aware/scrolling tables, plus a `## Commands`
table). Preserve .env, user commit 3d8421e and home files. No system packages
installed.
