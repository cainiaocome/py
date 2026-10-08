# AI CLI Chat — Ctrl+D exits in insert mode

Previous request complete: mode-aware scroll/edit key bindings. 109 tests; commit
aa6668f/5b9bab0, Actions
https://github.com/cainiaocome/py/actions/runs/37854497232, published image digest
sha256:f57bd070fa722a91c6749f00e76f653971630b59a17f6374575335f3c8173b8b.

Current goal (user request): restore Ctrl+D exiting in insert mode.

Change in `src/ai_chat/ui.py`:

- The insert/replace-mode Ctrl+D binding is now shell-style: at an empty prompt it
  exits (raises `EOFError` on the pending prompt), and with text in the draft it
  stays a no-op so it cannot delete or unindent input. Normal-mode Ctrl+D still
  scrolls half a page down. `/exit` and Ctrl+C continue to exit.

Tests: the idle-exit test is parametrized over Ctrl+C and Ctrl+D again, and the
draft test now checks that Ctrl+D does not exit or edit a non-empty insert draft
plus the normal-mode half page down. Full suite 109 passed, Ruff and diff checks
clean.

Validation pending: Docker/real-PTY check, commit/push, Actions and published
image verification (per AGENTS.md GitHub workflow). Preserve .env, user commit
3d8421e and home files. No system packages installed.
