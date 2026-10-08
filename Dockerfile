# syntax=docker/dockerfile:1
FROM ghcr.io/astral-sh/uv:0.12.23 AS uv
FROM python:3.13-slim AS builder
COPY --from=uv /uv /usr/local/bin/uv
ENV UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never UV_COMPILE_BYTECODE=1
WORKDIR /app
COPY pyproject.toml uv.lock README.md ./
COPY src ./src
RUN uv sync --locked --no-dev --no-editable

FROM python:3.13-slim AS runtime
LABEL org.opencontainers.image.source="https://github.com/cainiaocome/py"
ENV PATH="/app/.venv/bin:$PATH" PYTHONUNBUFFERED=1 PYTHONDONTWRITEBYTECODE=1
WORKDIR /app
COPY --from=builder /app/.venv /app/.venv
USER 10001:10001
ENTRYPOINT ["ai-chat"]
