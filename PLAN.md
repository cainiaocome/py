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

Validation: commit f2e012f; full suite 109 passed, Ruff and diff checks clean.
Actions https://github.com/cainiaocome/py/actions/runs/37856211760 succeeded.
Published image ghcr.io/cainiaocome/py:latest digest
sha256:ce9d996ae5278338475bcbce2995db1c5e6a8f86449e7c36fb98e0900a09b9db. A
real-PTY run of that image confirmed Ctrl+D at an empty prompt exits cleanly
(exit code 0), Ctrl+D with a draft leaves the app running and the draft
unchanged, and Ctrl+D exits once the draft is cleared. Preserve .env, user commit
3d8421e and home files. No system packages installed.
