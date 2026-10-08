# AI CLI Chat — command palette

Previous request complete: /clear clears the displayed transcript, Escape leaves
insert mode promptly, Ctrl+U clears the input line. 103 tests; commits
46ff481/c6c2b92, Actions https://github.com/cainiaocome/py/actions/runs/37833970128,
published image digest
sha256:35a1f013ab1491fb459e7d65c106772d1a1879be57285c62c20267c9fa44b90c.

Current goal (user request): when input starts with `/`, behave like Claude Code
or Codex — show command candidates and update them as the user types.

Design and changes:

- `CommandCompleter` now attaches a `display_meta` description to each candidate
  (`/clear`, `/exit`, `/model`, `/enable-web-search-and-web-fetch`).
- The full-screen editor enables `complete_while_typing`, so the existing
  `CompletionsMenu` float opens automatically for `/` and re-filters on each
  keystroke. Meta styles were added for readable descriptions.
- prompt_toolkit only auto-completes on insertion, so a text-changed handler
  rebuilds the palette after deletions (applying a completion only grows text,
  so it never re-triggers).
- Up/Down and Tab navigate candidates; a helper cycles with wrap-around instead
  of prompt_toolkit's default `complete_next`, which reverts to the typed prefix
  past the last item. Tab computes candidates synchronously so it cannot be
  raced by the per-keystroke automatic completion.
- Enter submits the current text; selecting a candidate first inserts it, then
  Enter runs it. Normal Vi half-page scrolling (Ctrl+U outside insert/replace)
  is unchanged.

Tests added: palette lists all commands and descriptions, filters while typing,
hides on no match and rebuilds after deletion, Down selects and Enter runs, Tab
completes a prefix. Full suite 105 passed, Ruff and diff checks clean.

Validation pending: Docker/real-PTY check, commit/push, Actions and published
image verification (per AGENTS.md GitHub workflow). Preserve .env, user commit
3d8421e and home files. No system packages installed.
