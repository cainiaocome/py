.DEFAULT_GOAL := help
BINARY := pychat
VERSION ?= 0.1.0
GO ?= go

.PHONY: help build test lint fmt run image clean
help:
	@echo 'build   Build ./pychat'
	@echo 'test    Run offline Go tests'
	@echo 'lint    Run go vet'
	@echo 'fmt     Format Go sources with gofmt'
	@echo 'run     Run from source'
	@echo 'image   Build the local pychat Docker image'
	@echo 'clean   Remove build output'
build:
	$(GO) build -ldflags "-X main.Version=$(VERSION)" -o $(BINARY) ./cmd/pychat
test:
	$(GO) test ./...
lint:
	$(GO) vet ./...
fmt:
	gofmt -w cmd internal
run:
	$(GO) run ./cmd/pychat
image:
	docker build -t pychat:local .
clean:
	rm -f $(BINARY)
