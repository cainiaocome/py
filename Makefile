.DEFAULT_GOAL := help
BINARY := gochat
VERSION ?= 0.2.1
GO ?= go

.PHONY: help build test lint fmt run image clean
help:
	@echo 'build   Build ./gochat'
	@echo 'test    Run offline Go tests'
	@echo 'lint    Run go vet'
	@echo 'fmt     Format Go sources with gofmt'
	@echo 'run     Run from source'
	@echo 'image   Build the local gochat Docker image'
	@echo 'clean   Remove build output'
build:
	$(GO) build -ldflags "-X main.Version=$(VERSION)" -o $(BINARY) ./cmd/gochat
test:
	$(GO) test ./...
lint:
	$(GO) vet ./...
fmt:
	gofmt -w cmd internal
run:
	$(GO) run ./cmd/gochat
image:
	docker build -t gochat:local .
clean:
	rm -f $(BINARY)
