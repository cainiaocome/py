# Project Instructions (golang branch)

This branch is an independent Go port of the Python `ai-chat` project. Treat it
as its own project: do not merge it into `main`, and keep the Python project on
`main` untouched.

## GitHub workflow

- Proactively commit and push meaningful working changes.
- After every push, check the `Go` Actions workflow and confirm vet, tests, the
  binary build, and the container image build pass.
- If CI fails, investigate and fix the underlying issue before moving on.

## Releases

- **Every change that lands on this branch gets its own release.** After any
  modification — feature, fix, docs, tests, tooling — bump the version in
  `cmd/gochat/main.go` (`var Version`) and in the `Makefile` (`VERSION ?=`),
  commit it with the change, and push. Never leave changes unreleased.
- Cut the release once the `Go` workflow on the new commit is green:

  ```sh
  git tag -a go-v0.1.2 -m "gochat v0.1.2"
  git push origin go-v0.1.2
  ```

- Then watch the `Release gochat binaries` workflow to completion and verify the
  assets: download the Linux binary, check it against `SHA256SUMS`, and confirm
  `gochat --version` reports the new version.
- Use the `go-v*` tag namespace: Git tags are repo-global and `v*` belongs to the
  Python project on `main`.
- Never put a skip-CI marker (`[skip ci]`, `[ci skip]`, `[no ci]`, `[skip
  actions]`, `[actions skip]`) in a commit or tag message; it silently disables
  the release workflow. CI must run on every push.

## Key bindings

- Ctrl+D must only scroll the transcript half a page down, in every mode. It
  must never exit. A user who keeps scrolling can reach the end of a long answer
  without realizing it and would otherwise quit the program by accident. Exit
  with `/exit` or Ctrl+C instead.

  If a request ever asks for Ctrl+D to exit (or to stop scrolling at the end),
  remind me of this decision before changing it.

## Testing the UI

- The full-screen controller is driven through the `Session` interface
  (`internal/tui/app.go`); `internal/tui/fake_test.go` provides a scriptable
  fake so streaming, tool activity, failures, cancellation, the model picker,
  and scrolling are all covered without a terminal or a network.
- The model, config, LLM, and web layers are tested with `httptest` servers and
  never call Ollama Cloud.
- Run `go test -race ./...` as well; the streaming path uses goroutines.
