# AI CLI Chat — web tools

Goal: approved Ollama Cloud search/fetch capability, retaining existing CLI,
environment-only Python configuration and Docker-only launcher distribution.

Implemented: async WebTools in src/ai_chat/web.py, existing API key, fixed
Ollama endpoints, safe structured errors, 30s total HTTP deadline. Search
5 results by default/max10, 20k total snippet chars; fetch20k chars/max20 links;
truncation flags. httpx declared directly without package version changes.

Agent factory registers tools at startup and after /model switches. Agent.iter
streams every model step and executes mixed text/tool responses to completion.
Per-turn budgets:10 tool calls/15 model requests. Terminal shows tool activity
and limit errors; successful tool messages retained in native history, failed
or cancelled turns leave history unchanged. Model instructed to cite actual
sources and treat retrieved text as untrusted data. README updated.

Validation passed: make lint test (56 tests), git diff --check, own source/diff
review. Tests cover HTTP/auth/body/schema/errors/deadline/content bounds,
search→fetch→answer, mixed text/tools, context, cancellation, loop limits,
model switching and terminal activity/recovery. Live Cloud search returned2
results; fetch returned official docs; configured model used both tools and
streamed a cited answer, then recalled verified endpoints on a follow-up.

Remaining: local Docker build/PTY tool smoke, commit/push, watch Actions and
verify published scripts/py image, then record completion. Keep .env private
and ignored; no source mounts, new configuration or system packages. Preserve
original untracked AGENTS.md and docs/spec.md.
