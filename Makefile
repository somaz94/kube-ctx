.PHONY: build clean test test-unit cover cover-html lint fmt vet install check-gh branch pr help

APP_NAME=kctx
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_DATE=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X github.com/somaz94/kube-ctx/cmd/cli.Version=$(VERSION) -X github.com/somaz94/kube-ctx/cmd/cli.GitCommit=$(GIT_COMMIT) -X github.com/somaz94/kube-ctx/cmd/cli.BuildDate=$(BUILD_DATE)"

## Build

build: ## Build the binary
	go build $(LDFLAGS) -o bin/$(APP_NAME) ./cmd/

clean: ## Remove build artifacts and coverage files
	rm -rf bin/ coverage.out coverage.html

## Test

test: test-unit ## Run unit tests (alias)

test-unit: ## Run unit tests with coverage
	go test ./... -v -race -cover

## Coverage

cover: ## Generate coverage report
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out

cover-html: cover ## Open coverage report in browser
	go tool cover -html=coverage.out -o coverage.html
	open coverage.html

## Quality

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format code
	go fmt ./...

vet: ## Run go vet
	go vet ./...

## Install

install: build ## Install to /usr/local/bin
	cp bin/$(APP_NAME) /usr/local/bin/$(APP_NAME)

## Workflow

check-gh: ## Check if gh CLI is installed and authenticated
	@command -v gh >/dev/null 2>&1 || { echo "\033[31m✗ gh CLI not installed. Run: brew install gh\033[0m"; exit 1; }
	@gh auth status >/dev/null 2>&1 || { echo "\033[31m✗ gh CLI not authenticated. Run: gh auth login\033[0m"; exit 1; }
	@echo "\033[32m✓ gh CLI ready\033[0m"

branch: ## Create feature branch (usage: make branch name=feature-name)
	@if [ -z "$(name)" ]; then echo "Usage: make branch name=<feature-name>"; exit 1; fi
	git checkout main
	git pull origin main
	git checkout -b feat/$(name)
	@echo "\033[32m✓ Branch feat/$(name) created\033[0m"

pr: check-gh ## Run tests, push, and create PR (usage: make pr title="Add feature")
	@if [ -z "$(title)" ]; then echo "Usage: make pr title=\"PR title\""; exit 1; fi
	go test ./... -race -cover
	go vet ./...
	git push -u origin $$(git branch --show-current)
	@./scripts/create-pr.sh "$(title)"
	@echo "\033[32m✓ PR created\033[0m"

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
