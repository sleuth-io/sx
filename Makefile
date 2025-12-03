.PHONY: help build install test lint format clean release

# Default target
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

# Build variables
BINARY_NAME=skills
MAIN_PATH=./cmd/skills
BUILD_DIR=./dist
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Date=$(DATE)"

build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	@go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)"

install: ## Install binary to $GOPATH/bin
	@echo "Installing $(BINARY_NAME)..."
	@go install $(LDFLAGS) $(MAIN_PATH)
	@echo "Installed to: $$(go env GOPATH)/bin/$(BINARY_NAME)"

test: ## Run tests
	@echo "Running tests..."
	@go test -v -race -cover ./...

lint: ## Run linters (requires golangci-lint)
	@echo "Running linters..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not found. Install from https://golangci-lint.run/usage/install/" && exit 1)
	@golangci-lint run

format: ## Format code
	@echo "Formatting code..."
	@gofmt -s -w .
	@go mod tidy

clean: ## Clean build artifacts
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@go clean

release: ## Create release with goreleaser (requires goreleaser)
	@echo "Creating release..."
	@which goreleaser > /dev/null || (echo "goreleaser not found. Install from https://goreleaser.com/install/" && exit 1)
	@goreleaser release --clean

# Development targets
run: build ## Build and run the binary
	@$(BUILD_DIR)/$(BINARY_NAME)

dev: ## Run in development mode (with hot reload, requires air)
	@which air > /dev/null || go install github.com/cosmtrek/air@latest
	@air

# Module management
deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@go mod download

tidy: ## Tidy go.mod
	@echo "Tidying go.mod..."
	@go mod tidy

verify: ## Verify dependencies
	@echo "Verifying dependencies..."
	@go mod verify
