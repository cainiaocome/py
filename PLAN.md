# AI CLI Chat — remove research round limits

Goal: remove per-turn tool/model request caps, commit and push.

Implemented: explicitly set UsageLimits(tool_calls_limit=None,
request_limit=None), including disabling Pydantic AI's default 50-request cap.
Removed the obsolete limit warning and updated README. Regression test runs
55 tool calls and 56 model requests, completes, and retains native history.
Existing opt-in, scrollback, environment lookup and interruption behavior remain.

Validation passed: make lint test (72 tests), including a completed 55-tool-call,
56-model-request turn; final source/diff review and git diff --check.
Remaining: commit/push, watch Actions image publication and verify a completed
long turn in the published image using a local test model (no Cloud quota).
Preserve user commit 3d8421e and existing tracked files; keep .env private and
ignored. No system packages installed.
