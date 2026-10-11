# Project Instructions (golang branch)

This branch is an independent Go port of the Python `ai-chat` project. Treat it
as its own project: do not merge it into `main`, and keep the Python project on
`main` untouched.

## GitHub workflow

- Proactively commit and push meaningful working changes.
- After every push, check the `Go` Actions workflow and confirm vet, tests, the
  binary build, and the container image build pass.
- If CI fails, investigate and fix the underlying issue before moving on.

## Key bindings

- Ctrl+D must only scroll the transcript half a page down, in every mode. It
  must never exit. A user who keeps scrolling can reach the end of a long answer
  without realizing it and would otherwise quit the program by accident. Exit
  with `/exit` or Ctrl+C instead.

  If a request ever asks for Ctrl+D to exit (or to stop scrolling at the end),
  remind me of this decision before changing it.

## Testing the UI

- The model layer is unit-tested with Bubble Tea messages directly
  (`internal/tui/app_test.go`); no terminal or network is required.
- `internal/config`, `internal/web`, and `internal/llm` tests use `httptest`
  servers and never call the real Ollama Cloud.
