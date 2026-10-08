.DEFAULT_GOAL := help
.PHONY: help run test lint format build image
help:
	@echo 'run     Start the published Docker chat client'
	@echo 'image   Build a local Docker image'
	@echo 'test    Run offline tests'
	@echo 'lint    Check Python style'
	@echo 'format  Format Python files'
	@echo 'build   Build the Python package'
run:
	./scripts/py
test:
	uv run pytest
lint:
	uv run ruff check .
	uv run ruff format --check .
format:
	uv run ruff format .
build:
	uv build

image:
	docker build -t ai-chat:local .
