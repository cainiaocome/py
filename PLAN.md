# AI CLI Chat — empty-prompt scroll shortcuts

Previous request complete: Ctrl+D exits at an empty insert prompt. 111 tests;
commit f2e012f/b43033c, Actions
https://github.com/cainiaocome/py/actions/runs/37856211760, published image digest
sha256:ce9d996ae5278338475bcbce2995db1c5e6a8f86449e7c36fb98e0900a09b9db.

Current goal (user request): let an empty insert prompt scroll the transcript, so
the user can read a long answer before typing the next question.

Changes in `src/ai_chat/ui.py`:

- Insert/replace Ctrl+U: with nothing typed, scroll half a page up instead of
  doing nothing; with a draft, still discard from the cursor to the line start.
- Insert/replace Ctrl+D: while the transcript is scrolled up, scroll half a page
  down (matching normal mode); once at the bottom it exits at an empty prompt and
  is a no-op when a draft exists. Normal-mode Ctrl+D is unchanged.
- Normal-mode Ctrl+U/Ctrl+D and Ctrl+B/Ctrl+F are unchanged.

Tests: added an empty-insert Ctrl+U half-page test and an insert Ctrl+D test
(draft no-op, half page down while scrolled up, exit at the empty tail). Full
suite 111 passed, Ruff and diff checks clean.

Validation: commit 81b7f14; full suite 111 passed, Ruff and diff checks clean.
Actions https://github.com/cainiaocome/py/actions/runs/37856921678 succeeded.
Published image ghcr.io/cainiaocome/py:latest digest
sha256:6cd067c1bef45fc5db7c7f2b76705841feb5dc1b3e469538f0069d2c7af2a1ab. A
real-PTY run of that image confirmed Ctrl+U on an empty insert prompt scrolled
the transcript up, Ctrl+D scrolled it back to the tail and exited cleanly once
at the empty bottom (exit code 0), and Ctrl+U with typed text cleared the line
without scrolling. Preserve .env, user commit 3d8421e and home files. No system
packages installed.
