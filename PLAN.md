# AI CLI Chat — pinned input and detailed tool activity

Previous request complete: per-turn tool/model request caps removed in 4181ddf.
72 tests passed, Actions https://github.com/cainiaocome/py/actions/runs/37826055849
passed; published image digest c393fcbdf6de7f386143059e91c353141c16571a189fd24dc48defd2e1dad5d7.
Offline published probe completed 55 tool calls/56 model requests/112 messages.

Current goal: input always pinned at bottom, full Rich Markdown transcript
scrollable above, tool activity shows actual search query/fetch URL. User
explicitly requests comprehensive tests, then commit and push.

Design: new ui.py TerminalUI using prompt_toolkit full-screen Application,
fixed bottom multiline Vim Buffer, native modal cursors, model/status/footer,
scrollable transcript with PgUp/PgDn/mouse and follow-tail toggle. Rich entries
render to ANSI fragments at current terminal width, with caching. During stream
input stays editable; Enter preserves draft until current run finishes. Ctrl+C
cancels active response or exits idle prompt. Existing terminal.run_chat command
controller reused via optional response_renderer callback and prompt_async
adapter. Preserve legacy output for non-TTY operation. Full-screen session
restores terminal then prints stable complete transcript once into shell
scrollback. No source/config mounts or new dependencies.

Implementation complete: ui.py integrated for interactive stdin/stdout, legacy
fallback retained; actual tool arguments displayed with controls removed; stable
full transcript printed after terminal restoration. Live Markdown shows visible
URLs (Rich hyperlinks disabled in a shallow copy to avoid OSC8 metadata garbage
in prompt_toolkit ANSI parsing); original renderables retain shell hyperlinks.

Validation: final root full run passed 98 tests and Ruff/diff checks; coverage 96%
total statements, 95% ui.py, 100% web.py. New virtual-terminal tests exercise pinned
input, busy drafts, PageUp/Down/mouse, follow-tail, resize/reflow, multiline/Vim
cursor modes, model picker/cancellation, web opt-in/persistence/query/URL safety,
interrupt/error recovery, EOF/task cancellation cleanup, final transcript and
citation links. Coverage-guided no-output failure recovery, mouse down, and literal-paste
cases now pass as well. No new application dependencies.

Remaining: local Docker/real PTY checks, commit/push, Actions and published
image checks. Production source is frozen after the final hyperlink-rendering
fix. Preserve .env, user commit 3d8421e and home files. No system packages
installed. Coverage data stored only in ignored tmp/ui-coverage.
