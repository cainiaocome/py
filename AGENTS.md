# Project Instructions

## GitHub workflow

- Proactively use GitHub to save progress: commit and push meaningful working
  changes to the configured remote instead of leaving them only local.
- After every push, check the GitHub Actions runs (tests and container image
  builds) and confirm they pass.
- If CI fails, investigate and fix the underlying issue, then push the fix and
  re-check until the build is green. Do not dismiss a failure as flaky without
  reproducing and root-causing it.
- When a feature is validated, record the run URL and published image digest in
  `PLAN.md`.

## Releases

- Release tags are `v*` and trigger both `.github/workflows/release.yml` and
  the tag image in `.github/workflows/image.yml`.
- Never put `[skip ci]` (or `[ci skip]`) in a commit message. GitHub then skips
  tag-push workflows for any tag pointing at that commit, which silently breaks
  the release. Let CI run on every push instead.

## Key bindings

- Ctrl+D must only scroll the transcript half a page down, in every mode. It
  must never exit. A user who keeps scrolling can reach the end of a long answer
  without realizing it and would otherwise quit the program by accident. Exit
  with `/exit` or Ctrl+C instead.

  If a request ever asks for Ctrl+D to exit (or to stop scrolling at the end),
  remind me of this decision before changing it.
