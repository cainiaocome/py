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

## /start — jump back to the last question

Current goal (user request): add a `/start` command that scrolls the transcript
so the last user message sits at the top, letting the user reread the assistant's
answer from its start.

Changes:

- `src/ai_chat/terminal.py`: added `/start` to `COMMANDS` and
  `COMMAND_DESCRIPTIONS`; `run_chat` now takes `on_start` (invoked for `/start`)
  and `on_message` (invoked only for real questions, skipping commands and their
  echoes). Non-full-screen output reports that `/start` needs the full-screen UI.
- `src/ai_chat/ui.py`: `_accept_input` remembers the echoed line; `on_message`
  promotes it to `_last_user_entry`; `scroll_to_last_message` computes that
  entry's first transcript line (accounting for `split_lines`' trailing empty
  line) and pins it at the top, clamping to the last screenful and resuming
  follow when the question is too close to the bottom. `clear_transcript` resets
  both references and `/start` after `/clear` prints a short note.
- `tests`: new terminal tests for the callbacks/non-UI message and UI tests for
  pinning the last question, idempotent repeat, and the empty-after-`/clear` case.
- `README.md`: added `/start` to the Commands table.

Full suite 114 passed, Ruff and diff checks clean.

Validation: commit 1346f31; full suite 114 passed. Actions
https://github.com/cainiaocome/py/actions/runs/37858830034 succeeded. Published
image ghcr.io/cainiaocome/py:latest digest
sha256:ba129a0544f1641a80bf6b56c4fbc7e6ae918e91ce8027844a9e9137945cdd92. A
real-PTY run of that image confirmed `/start` puts the last question's first line
at the top of the transcript, is idempotent and never exits, prints "No previous
message to scroll to." after `/clear`, and Ctrl+C still exits cleanly (code 0).
