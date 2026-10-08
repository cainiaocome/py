# AI CLI Chat — mode-aware key bindings

Previous requests complete: stable inline input box plus the live slash command
palette. 108 tests; commit 990fb92/abfb454, Actions
https://github.com/cainiaocome/py/actions/runs/37837153963, published image digest
sha256:15477ba555c8d476e6dd8371708632bf74e1ae4594759739cc5732874ea11a99.

Current goal (user request): mode-aware shortcuts, with Ctrl+B/Ctrl+F paging the
transcript in both insert and normal mode.

Changes in `src/ai_chat/ui.py`:

- Ctrl+B and Ctrl+F scroll a full transcript page up/down in both modes (page =
  viewport minus two lines, matching PgUp/PgDn).
- Ctrl+U clears the current line in insert/replace mode and scrolls half a page
  up in normal mode.
- Ctrl+D is a no-op in insert/replace mode and scrolls half a page down in
  normal mode. This intentionally removes the old "Ctrl+D at an empty prompt
  exits" behavior; `/exit` and Ctrl+C still exit.
- Ctrl+E moves to the end of the current line in insert/replace mode and does
  nothing in normal mode.
- Added `_page_lines`/`_half_page_lines` helpers and an `editing_modes` filter
  (`vi_insert_mode | vi_insert_multiple_mode | vi_replace_mode`) so each key can
  dispatch on insert vs navigation mode. All new bindings are eager app-level
  bindings, so they override prompt_toolkit's default Vi page navigation.

Tests: added coverage for Ctrl+B/F in both modes, Ctrl+U half-page in normal
mode, Ctrl+D no-op (including not exiting at an empty prompt) plus half-page in
normal mode, and Ctrl+E end-of-line plus normal-mode no-op. Updated the idle-exit
test to Ctrl+C only and the README key list. Full suite 108 passed, Ruff and diff
checks clean.

Validation: commit aa6668f; full suite 108 passed, Ruff and diff checks clean.
Actions https://github.com/cainiaocome/py/actions/runs/37854497232 succeeded.
Published image ghcr.io/cainiaocome/py:latest digest
sha256:f57bd070fa722a91c6749f00e76f653971630b59a17f6374575335f3c8173b8b. A
real-PTY run of that image confirmed: Ctrl+B/Ctrl+F page the transcript in both
insert and normal mode, normal Ctrl+U scrolls half a page up and normal Ctrl+D
returns half a page down, normal Ctrl+E is a no-op, insert Ctrl+E moves to the
line end (typing after it appended to `hello!`), insert Ctrl+D neither exited nor
edited, and insert Ctrl+U cleared the line. Preserve .env, user commit 3d8421e
and home files. No system packages installed.
