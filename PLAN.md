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

## Normal-mode message navigation

Current goal (user request): normal-mode keys to move between questions —
`gg`/`p` to go back one question per press (first press goes to the most recent),
`n` to go forward one question when one exists, and `G` to jump to the real end
of the transcript.

Changes:

- `src/ai_chat/ui.py`: replaced the single `_last_user_entry` reference with a
  `_user_entries` list. `_message_anchors` computes each question's first
  transcript line from the rendered entries; `previous_message`/`next_message`
  pick the neighbouring anchor relative to the current top line, so manual
  scrolling still behaves; `_pin_offset` reuses the `/start` pinning (clamping to
  the last screenful). `G` shares the `End`/`Ctrl+End` follow-tail handler.
  Because the 50 ms key timeout can flush a lone `g`, a non-eager `g` handler
  remembers it so a slower second `g` still counts as `gg`; Esc stays immediate,
  and Vi's other `g`-prefixed commands still work when typed together.
- `tests/test_ui.py`: new test drives slow `gg`, fast `gg`, `n`, `p`, a no-op `n`
  at the last question, and `G`.
- `README.md`: added a "Jumping between questions (normal mode)" table.

Full suite 115 passed, Ruff and diff checks clean.

Validation: commit af33932; full suite 115 passed. Actions
https://github.com/cainiaocome/py/actions/runs/37860537647 succeeded. Published
image ghcr.io/cainiaocome/py:latest digest
sha256:ede0777c2ce8ee8c9b6b3acb681452d590c91acde34406c1cb7009eb545c4385. A
real-PTY run of that image confirmed slow `gg`, fast `gg`, `n`, `p`, a clamped
`n` at the last question, and `G` back to the end, all without exiting (Ctrl+C
still exit code 0).

## Normal-mode j/k line scrolling

Current goal (user request): `j`/`k` in normal mode scroll the transcript one
line down/up.

Changes:

- `src/ai_chat/ui.py`: eager normal-mode `j`/`k` bindings call
  `_scroll_forward(1)` / `_scroll_back(1)`, overriding Vim's cursor-line motion
  in the input buffer.
- `tests/test_ui.py`: new test checks `k` pauses and steps one line up, and `j`
  steps one line back down.
- `README.md`: renamed the normal-mode table and added the `j`/`k` row.

Full suite 116 passed, Ruff and diff checks clean.

Validation: commit dd1fb3c; full suite 116 passed. Actions
https://github.com/cainiaocome/py/actions/runs/37861447900 succeeded. Published
image ghcr.io/cainiaocome/py:latest digest
sha256:b75a4a558dc450c89e6a7f21dc8bca73adb4c8b2321eb48e9157834870ad640c. A
real-PTY run of that image confirmed two `k` presses pause the transcript, one
`j` keeps it paused (one-line movement, not a page), the next `j` follows the
tail again, and Ctrl+C still exits cleanly (code 0).

## Smooth scrolling: cache transcript lines and raise the redraw rate

Current goal (user request): make scrolling feel smooth. Profiling showed the
transcript was re-split and re-measured on every frame (O(total transcript)),
capped at ~12 FPS.

Root cause and measurements (test virtual terminal, 100 cols):

- `_transcript_text` + prompt_toolkit's `FormattedTextControl.create_content`
  both ran `split_lines` over the whole transcript each render, and
  `create_content` also hashed a `tuple` of every fragment. `split_lines` was
  ~97% of render time.
- Per-frame `create_content`: 6 ms (1 entry) → 61 ms (10) → 338 ms (50).
  `min_redraw_interval=1/12` capped redraws at ~12 FPS.

Changes:

- `src/ai_chat/ui.py`: `TranscriptEntry` now caches the split `lines` per width
  (`revision` bumps on `invalidate`). `TerminalUI._transcript_lines(width)`
  assembles the transcript from those cached lines and only rebuilds when the
  width or any entry revision changes; `_line_count` is maintained there.
  `TranscriptControl` is now a plain `UIControl` whose `create_content` returns a
  `UIContent` over the cached lines, so prompt_toolkit never re-splits or hashes
  the transcript. This uses only public prompt_toolkit API (no monkey-patching);
  `mouse_handler` keeps wheel scrolling, `is_focusable=False`.
  `min_redraw_interval` raised from `1/12` to `1/60`.
- `tests/test_ui.py`: new test asserts the assembled lines and per-entry split are
  reused across frames/scrolls and rebuilt only on content/width change.
- `README.md`: unchanged (no user-facing behavior change).

Results (same benchmarks):

- Warm assemble: ~0.001–0.010 ms (was 158 ms for 50 entries).
- One-line scroll: ~0.012 ms (was ~320 ms for 100 entries).
- End-to-end: 56 FPS over 1 s of continuous scrolling with 50 entries
  (~5800 lines), mean transcript frame 2.5 ms, worst 5.0 ms (was effectively
  ~3 FPS). Full suite 117 passed, Ruff and diff checks clean.

Validation: commit 6fc8df4; full suite 117 passed. Actions
https://github.com/cainiaocome/py/actions/runs/37864220694 succeeded. Published
image ghcr.io/cainiaocome/py:latest digest
sha256:45276bdba709403aeb6d541b4953be2b5b45641e0e81ad90356c715bc60d8ffc. A
real-PTY run of that image (distinct wrapped lines) measured 17 ms median scroll
latency per wheel tick (p95 18 ms), a 20-tick burst settling in 24 ms, and all
scrolling/navigation keys (j/k, gg/p/n/G, Ctrl+U/Ctrl+D) still working with a
clean Ctrl+C exit.

## Throttle the live streaming re-render

Current goal (user request, follow-up #3 from the performance review): stop
re-rendering the whole streamed answer on every chunk.

Root cause: `render_response` set `entry.objects = (Markdown(text),)` and
invalidated on every token. Rich Markdown re-rendering grows with the answer, so
each frame cost O(answer so far); at 60 FPS that is noticeable CPU even though
scrolling is now smooth.

Changes:

- `src/ai_chat/ui.py`: added `LIVE_REFRESH_INTERVAL = 0.05` and repaint the live
  entry at most every 50 ms (the first chunk always paints, and
  `finish_live_response` always paints the complete text on success, error, or
  interrupt, so the final answer is never truncated).
- `tests/test_ui.py`: new test streams 400 chunks and asserts the live entry is
  repainted far fewer times than there are chunks while the final Markdown holds
  the whole answer.

Measured (200 chunks over ~1.2 s, 100x40): live repaints 93 -> 24, live render
time 176 ms -> 43 ms (~10.5% -> 3.6% of a core); the stream also completed
faster because the event loop spent less time rendering. Full suite 118 passed.

Validation: commit 1e256ef; full suite 118 passed. Actions
https://github.com/cainiaocome/py/actions/runs/37865245310 succeeded. Published
image ghcr.io/cainiaocome/py:latest digest
sha256:051a460137d7b91e73ff8bf941d17ed9fa721e4375544e1c9d5d6f1afd1bab30. PTY
checks of that image still pass (17 ms median scroll latency, j/k, gg/p/n/G,
Ctrl+U/Ctrl+D, clean Ctrl+C). The throttle itself is covered by the new unit
test; the dummy Cloud key fails before any chunk so the live path cannot stream
in the image.

## Normal-mode u / d / Space scrolling keys

User request: make transcript navigation easy from normal mode.

- `u` scrolls half a page up (same as `Ctrl+U` in normal mode).
- `d` scrolls half a page down (same as `Ctrl+D`).
- `Space` scrolls down one full page.

Implementation: three eager `vi_navigation_mode` bindings in `ui.py` reusing
`_half_page_lines()` / `_page_lines()`, documented in the README normal-mode
navigation table, and covered by a new test.

Validation: commit 62ca6f9; full suite 119 passed. Actions
https://github.com/cainiaocome/py/actions/runs/37866230435 succeeded. Published
image ghcr.io/cainiaocome/py:latest digest
sha256:a9a78ff311df0079f9cde871c9adf19592cdd0eb358b4988934284794b809079. A real-PTY
run of that image scrolled to the top (`k`), then measured distinct line tokens:
top=0, `d` (half page)=82, `u`=0 (exact reverse), `Space` (full page)=194, and a
clean Ctrl+C exit.

## Standalone binaries (Phase 1 and 2)

User request: release the tool as a single binary for Linux and macOS, not only
as a container, without disturbing the Docker workflow they use most. Windows
and Intel macOS are out of scope.

Phase 1 (packaging and parity):

- `src/ai_chat/config.py` resolves credentials like `scripts/py`: exported
  values win, then the working directory's `.env`, then `$HOME/.env`;
  `AI_CHAT_ENV_FILE` selects one explicit file. The native parser is a safe
  subset (comments, `export`, quotes) and never executes the file. `main.py`
  calls it, so the binary is usable without the Docker launcher.
- `main.py` gained `--version` (argparse `parse_known_args`, so foreign argv is
  ignored) and reports a missing explicit config file before the startup check.
- `ai-chat.spec` and `packaging/entry.py` freeze the app with PyInstaller,
  copying the distribution metadata that `genai-prices`, `pydantic-ai-slim` and
  `ai-chat` read at import. `make binary` builds `dist/ai-chat` via the new
  `packaging` dependency group.
- Tests: `tests/test_config.py`, expanded `tests/test_main.py`, and
  `tests/test_binary.py` (skipped unless `dist/ai-chat` exists).

Phase 2 (release pipeline):

- `.github/workflows/image.yml` also builds and smoke-tests the Linux binary.
- `.github/workflows/release.yml` builds Linux x86_64/arm64 and macOS arm64
  binaries on `v*` tags, attaches them plus `SHA256SUMS` to the GitHub release,
  and supports a build-only manual dispatch.

The Docker path is unchanged in behavior: the launcher still loads `.env` and
passes the two variables to the container, where the app sees complete
environment values and skips file loading.

Validation: commit da40c8d; full suite 138 passed (the four binary smoke tests
run when `dist/ai-chat` exists, otherwise they skip). Actions
https://github.com/cainiaocome/py/actions/runs/38093738114 succeeded (test,
binary and image jobs). The release matrix
https://github.com/cainiaocome/py/actions/runs/38093877257 built and smoke-tested
the Linux x86_64, Linux arm64 and macOS arm64 binaries; the release job is
skipped without a `v*` tag. The published image ghcr.io/cainiaocome/py:latest
digest
sha256:8f6b2827363a6479a16821a8745149ea86ac3e265e3510a91df645dfde9ec0d0 still
renders the full-screen UI in a PTY. The local PyInstaller build is a 30 MB
Linux binary and its full-screen UI also runs in a PTY.

### v0.1.0 release

Created and pushed `v0.1.0` on commit f82d305. The release workflow
https://github.com/cainiaocome/py/actions/runs/38094278280 succeeded and
published https://github.com/cainiaocome/py/releases/tag/v0.1.0 with
`ai-chat-linux-x86_64` (31,124,472 bytes), `ai-chat-linux-arm64` (29,131,392
bytes), `ai-chat-macos-arm64` (26,121,696 bytes) and `SHA256SUMS`. The image
workflow https://github.com/cainiaocome/py/actions/runs/38094278225 published
ghcr.io/cainiaocome/py:v0.1.0 (digest
sha256:9e7c203512df26fcd90249693d80a79e421568749dd66a96caf1ecc6cc80626f).

A tag pointing at a commit whose message contains a skip-CI marker is silently
ignored by GitHub, so the first `v0.1.0` tag (on f855d32) triggered nothing. The
fix is to avoid skip-CI markers entirely and let CI run on every push; the
hazard is recorded in `AGENTS.md`. The downloaded Linux binary was re-checked:
its sha256 matches `SHA256SUMS`, `--version` prints `ai-chat 0.1.0`, `.env`
loading works, and a missing configuration exits 1.

### Renamed the standalone binary to pychat

The frozen executable and its release assets are now `pychat` / `pychat-*`
(spec `pychat.spec`, `dist/pychat`), and `--version` reports `pychat 0.1.0`.
The Python distribution, import package, console script and container image
keep the `ai-chat` name. Commit 573d66d; Actions
https://github.com/cainiaocome/py/actions/runs/38094714084 succeeded. A tag-push
workflow runs the file from the tagged commit, so `v0.1.0` was re-pointed at
573d66d and the release republished with the renamed assets: release
https://github.com/cainiaocome/py/actions/runs/38094852784 published
`pychat-linux-x86_64` (31,125,896 bytes), `pychat-linux-arm64` (29,133,144
bytes), `pychat-macos-arm64` (26,122,528 bytes) and `SHA256SUMS`; image
https://github.com/cainiaocome/py/actions/runs/38094852816 published
ghcr.io/cainiaocome/py:v0.1.0 (ghcr.io/cainiaocome/py@sha256:05ef4289448c5b7766516923779bb787c3fa5b083f0fd8e73afe7f0169f80dc7). The downloaded Linux
asset's sha256 matches `SHA256SUMS` (2347ab9f...) and `--version` prints
`pychat 0.1.0`.
