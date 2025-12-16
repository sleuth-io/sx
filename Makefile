.PHONY: help build install test lint format clean release

# Default target
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

# Build variables
BINARY_NAME=sx
MAIN_PATH=./cmd/sx
BUILD_DIR=./dist
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.Date=$(DATE)"

build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	@go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)"

install: build ## Install binary to ~/.local/bin
	@echo "Installing $(BINARY_NAME)..."
	@mkdir -p $$HOME/.local/bin
	@cp $(BUILD_DIR)/$(BINARY_NAME) $$HOME/.local/bin/
	@echo "✓ $(BINARY_NAME) installed to $$HOME/.local/bin/$(BINARY_NAME)"
	@case ":$$PATH:" in \
		*":$$HOME/.local/bin:"*) ;; \
		*) echo ""; \
		   echo "⚠ Warning: $$HOME/.local/bin is not in your PATH"; \
		   echo "Add this to your ~/.bashrc or ~/.zshrc:"; \
		   echo "  export PATH=\"\$$PATH:$$HOME/.local/bin\"" ;; \
	esac

test: ## Run tests
	@echo "Running tests..."
	@OUTPUT=$$(go test -race -cover ./... 2>&1); \
	if [ $$? -eq 0 ]; then \
		PASSED=$$(echo "$$OUTPUT" | grep -c "^ok"); \
		echo "✓ All $$PASSED packages passed"; \
	else \
		echo "$$OUTPUT"; \
		exit 1; \
	fi

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

update-deps: ## Update all dependencies to latest versions
	@echo "Updating all dependencies..."
	@go get -u ./...
	@go mod tidy

init: ## Initialize development environment (install tools, download deps)
	@echo "Initializing development environment..."
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Downloading dependencies..."
	@go mod download
	@if ! which golangci-lint > /dev/null 2>&1; then \
		echo ""; \
		echo "WARNING: golangci-lint was installed but is not in your PATH."; \
		echo "Add this to your ~/.bashrc or ~/.zshrc:"; \
		echo "  export PATH=\"\$$PATH:\$$(go env GOPATH)/bin\""; \
		echo "Then run: source ~/.bashrc (or source ~/.zshrc)"; \
		echo ""; \
	fi
	@echo ""
	@echo "✓ Development environment initialized"

prepush: format lint test build ## Run before pushing (format, lint, test, build)

postpull: deps ## Run after pulling (download dependencies)
