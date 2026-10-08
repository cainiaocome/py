# AI CLI Chat — input/UX fixes

Previous request complete: pinned input, scrollable Rich transcript and detailed
tool activity. 99 tests; validated commits 3788e9c/c4f5139, Actions
https://github.com/cainiaocome/py/actions/runs/37831896964, published image digest
sha256:3794768f7c20dfd0d73eb846653635fce8c9b007eaff15d81ece24f86b685534.

Current goal (user request): three UX fixes in the full-screen UI:

1. `/clear` must also clear the displayed conversation, not only model history.
2. Escape in insert mode took about a second to enter normal mode; it should be
   immediate.
3. Ctrl+U after typing left the text in place and only moved the cursor to the
   start of the line; it should clear the line.

Root causes and fixes:

- `/clear` only called `chat.clear()`. `run_chat` gained an optional `on_clear`
  callback; the UI passes `TranscriptConsole.clear_transcript`, which drops the
  entries and resets follow-tail/scroll before printing the confirmation.
- Prompt_toolkit defaults `Application.ttimeoutlen = 0.5s` and `timeoutlen = 1.0s`
  to disambiguate a lone Escape from escape sequences and Alt+Enter (which sends
  the same `ESC CR` bytes). The UI now sets both to 0.05s, so a lone Escape takes
  ~0.1s while Alt+Enter is still recognized.
- In full-screen Vi apps, `load_vi_page_navigation_bindings` binds `c-u` to
  half-page scroll, shadowing the insert-mode `unix-line-discard` binding. The UI
  now binds `c-u` in insert/replace mode to delete from the cursor to line start;
  normal-mode half-page scrolling is unchanged.

Tests added: Ctrl+U clears line content; Escape reaches normal mode quickly with
short configured timeouts; `/clear` leaves only the confirmation entry; `on_clear`
callback invoked by `run_chat`. Full suite 103 passed, Ruff and diff checks clean.
Alt+Enter multiline input remains covered by the existing Vim test.

Validation: commit 46ff481; full suite 103 passed, Ruff and diff checks clean;
UI tests stable across 5 CPU-contended runs. Actions
https://github.com/cainiaocome/py/actions/runs/37833970128 passed test and image
jobs. Published image ghcr.io/cainiaocome/py:latest digest
sha256:35a1f013ab1491fb459e7d65c106772d1a1879be57285c62c20267c9fa44b90c. A
real-PTY run of that image confirmed Ctrl+U clears the input line, Escape leaves
insert mode promptly (~0.1-0.3s) while Alt+Enter still works, and `/clear`
removes the displayed conversation. Preserve .env, user commit 3d8421e and home
files. No system packages installed.
