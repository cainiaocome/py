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
