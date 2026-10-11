# gochat (Go port)

A single-binary, full-screen terminal chat client for [Ollama
Cloud](https://ollama.com), written in Go. This is a faithful port of the
Python `ai-chat` project and lives on its own `golang` branch, as an independent
project — it is not intended to be merged into `main`.

The Python original renders the transcript with [Rich](https://github.com/Textualize/rich)
and drives input with [prompt-toolkit](https://github.com/prompt-toolkit/python-prompt-toolkit).
This port uses the Go equivalents: [Bubble Tea](https://github.com/charmbracelet/bubbletea)
for the application, [Glamour](https://github.com/charmbracelet/glamour) for
Markdown, and a hand-written modal (vim) editor and command palette. The model
layer talks to Ollama's OpenAI-compatible API with `net/http`, with no
third-party LLM SDK.

## Features

- Full-screen UI: scrollable Markdown transcript with a fixed input area at the
  bottom.
- Streaming answers rendered as Markdown, with a throttled live repaint.
- Tool activity lines ("Searching the web: …", "Fetching page: …") taken from the
  real tool arguments.
- Vim-style modal editing (insert, normal, replace) with the key bindings below.
- A live command palette: type `/` to see commands with descriptions.
- Session commands: `/start`, `/clear`, `/model`, `/enable-web-search-and-web-fetch`,
  `/exit`.
- Web tools: `web_search` and `web_fetch` through Ollama's web endpoints.
- Configuration from the environment or `.env` files, with the same lookup order
  as the Python launcher.
- A line-oriented fallback interface when stdin or stdout is not a terminal.

## Build and run

Requires Go 1.27+.

```sh
make build          # writes ./gochat
make test           # offline Go tests
make lint           # go vet
make run            # run from source

OLLAMA_API_KEY=... OLLAMA_MODEL=... ./gochat
```

`gochat --version` prints the version; `gochat --help` prints usage. Unknown
arguments are ignored.

### Tests

```sh
make test           # go test ./...
make lint           # go vet ./...
go test -race ./...
```

The suite runs entirely offline. HTTP is exercised with `httptest` servers, and
the full-screen controller is driven through the `Session` interface by a fake
conversation that scripts streaming deltas, tool activity, failures, and
cancellation. `go test -cover ./...` reports roughly 85-95% per package (`main`
is the exception, since it needs a real terminal).

### Docker

```sh
make image
docker run --rm -it -e OLLAMA_API_KEY -e OLLAMA_MODEL gochat:local
```

The image is a static binary on `scratch` with CA certificates, so TLS to
Ollama works.

## Releases

Prebuilt binaries are attached to [GitHub
releases](https://github.com/cainiaocome/py/releases) as `gochat-linux-x86_64`,
`gochat-linux-arm64`, and `gochat-macos-arm64`, plus `SHA256SUMS`. Because Git
tags are repository-wide and `v*` belongs to the Python project, the Go
releases use their own `go-v*` namespace. Cut one with:

```sh
git tag -a go-v0.1.1 -m "gochat v0.1.1"
git push origin go-v0.1.1
```

The tag runs `.github/workflows/release.yml`, which builds each target natively,
smoke-tests `--version`, and publishes the assets. `go-v*` tags must point at a
commit without a skip-CI marker, or GitHub silently skips the run.

## Configuration

Credentials are resolved the same way as the Python launcher:

1. `OLLAMA_API_KEY` and `OLLAMA_MODEL` already set in the environment win.
2. Otherwise the current directory's `.env` is loaded.
3. Then `$HOME/.env`.
4. `AI_CHAT_ENV_FILE` selects one explicit file instead of all of the above.

The native parser supports a safe subset of shell syntax: blank lines, `#`
comments, an optional leading `export`, `KEY=value` pairs, and single- or
double-quoted values. It never executes the file or expands variables. Blank
values fall through to later files.

## Key bindings

Vim editing is enabled by default. Press `i` to enter insert mode and Esc for
normal mode.

### Editing and submission

| Key | Action |
| --- | --- |
| `Enter` | Send the message (or command) |
| `Alt+Enter` | Insert a newline |
| `Esc` | Normal mode |
| `i` / `a` / `I` / `A` | Insert before / after the cursor, or at line start / end |
| `o` / `O` | Open a line below / above |
| `R` | Replace mode |
| `r` + char | Replace the character under the cursor |
| `h` `l` `w` `b` `e` `0` `$` `^` | Motions |
| `x` / `D` / `C` | Delete char / to end of line / change to end of line |
| `Tab` | Complete the command; cycle candidates |
| `Up` / `Down` | Previous / next candidate when the palette is open, else move lines |
| `Left` / `Right` / `Home` / `End` | Move the cursor |

### Mode-aware shortcuts

| Key | Insert | Normal |
| --- | --- | --- |
| `Ctrl+U` | Clear to line start (scroll half a page up when empty) | Half page up |
| `Ctrl+D` | Half page down | Half page down |
| `Ctrl+E` | End of line | (nothing) |
| `Ctrl+B` / `Ctrl+F` | Page up / page down | Page up / page down |

> **Ctrl+D never exits.** It only scrolls half a page down, in both modes, so a
> user who keeps scrolling cannot quit the program by accident. Exit with
> `/exit` or `Ctrl+C`.

### Transcript scrolling

| Key | Action |
| --- | --- |
| `Ctrl+B` / `PgUp` | Page up |
| `Ctrl+F` / `PgDn` | Page down |
| `Ctrl+D` | Half page down |
| Mouse wheel | Scroll three lines |
| `End` (paused) | Follow new output again |

### Normal-mode navigation

| Key | Action |
| --- | --- |
| `j` / `k` | Scroll one line down / up |
| `u` / `d` | Half page up / down |
| `Space` | Page down |
| `gg` / `p` | Previous question (repeats go further back) |
| `n` | Next question |
| `G` | Jump to the real end |

## Commands

| Command | Description |
| --- | --- |
| `/start` | Scroll to the start of the last response |
| `/clear` | Clear the conversation and the transcript |
| `/model [name]` | Show the model list, or switch to a model |
| `/enable-web-search-and-web-fetch` | Enable the web tools for this session |
| `/exit` | Exit the chat |

## Layout and behavior parity

- The layout is the same: transcript, a divider, a stable three-line input box
  (four when the draft wraps), and a reverse-video footer showing the mode and
  status.
- The transcript keeps a per-entry render cache keyed by width and revision, so
  a redraw only re-renders entries whose content changed.
- The live answer is re-rendered at most every 50 ms while streaming; the final
  text is always painted.
- On exit the finished transcript is printed to standard output, so it remains
  in the terminal's scrollback.

### Known differences from the Python original

- Vim support is a focused subset. Because this app claims `j`, `k`, `u`, `d`,
  `p`, `n`, `G`, `g`, `Space` for scrolling and message navigation in normal
  mode, those editing motions are intentionally unavailable, exactly as in the
  Python UI.
- Markdown is rendered by Glamour rather than Rich; styling is close but not
  byte-identical.
- Cursor shapes are painted rather than rendered by the terminal, so insert
  mode shows a thin beam (`▏`), normal mode a reverse-video block, and replace
  mode an underline, matching prompt_toolkit's `ModalCursorShapeConfig`. The
  real cursor is still parked on the caret on every frame (hidden), because
  terminals anchor the IME candidate window for Chinese, Japanese and Korean
  input at the hardware cursor; otherwise that popup appears in the bottom-left
  corner.
- The line-oriented fallback prints plain text (no live Markdown repaint), which
  keeps piped output clean.
