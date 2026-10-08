# AI CLI Chat — input layout

Previous requests complete: live slash command palette plus the earlier input UX
fixes. 105 tests; commits 2870236/995a0f3, Actions
https://github.com/cainiaocome/py/actions/runs/37835460727, published image digest
sha256:c61a15f51af2e3e3f47b5d59ec5efe597f9263310c07a5e7a8e924cd1c9bafc1.

Current goal (user request): the input box shrank after a response and the input
started on a new line under `You [model] >`. Make the box a stable size (3-4
lines) and put the input on the same line as the prompt.

Changes in `src/ai_chat/ui.py`:

- The editor height is no longer `min=1`: it is 3 lines for an empty or short
  draft and grows to 4 for longer wrapped drafts
  (`Dimension(min=3, preferred=visible_lines, max=visible_lines)`). Because the
  max equals the preferred size, the layout can no longer inflate the box when
  the transcript is short, and it never shrinks below three lines once the
  transcript grows. Wrapped-line counting subtracts the inline prompt width.
- The prompt is rendered inline with `Window(get_line_prefix=...)` instead of a
  separate window, so `You [model] > message` is one line and wrapped or
  continuation lines align under the first input column. The standalone prompt
  window was removed from the root layout and `_viewport_height` was updated.
- `_line_prefix` renders the prompt on `(lineno, wrap_count) == (0, 0)` and
  spaces of the prompt width on later lines.

Tests: full suite 105 passed, Ruff and diff checks clean (the existing
scroll/wrap/resize tests still pass because the box keeps a three-line floor and
its old growth for long drafts).

Validation pending: Docker/real-PTY check, commit/push, Actions and published
image verification (per AGENTS.md GitHub workflow). Preserve .env, user commit
3d8421e and home files. No system packages installed.
