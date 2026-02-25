.PHONY: help build build-darwin build-darwin-amd64 install test lint format clean release demo sx

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
LDFLAGS=-ldflags "-X github.com/sleuth-io/sx/internal/buildinfo.Version=$(VERSION) -X github.com/sleuth-io/sx/internal/buildinfo.Commit=$(COMMIT) -X github.com/sleuth-io/sx/internal/buildinfo.Date=$(DATE)"

build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	@go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)"

build-darwin: ## Build for macOS (arm64)
	@echo "Building $(BINARY_NAME) for macOS (arm64)..."
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64"

build-darwin-amd64: ## Build for macOS (amd64/Intel)
	@echo "Building $(BINARY_NAME) for macOS (amd64)..."
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	@echo "Built: $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64"

install: build ## Install binary to ~/.local/bin
	@echo "Installing $(BINARY_NAME)..."
	@mkdir -p $(HOME)/.local/bin
	@rm -f $(HOME)/.local/bin/$(BINARY_NAME) && cp $(BUILD_DIR)/$(BINARY_NAME) $(HOME)/.local/bin/
	@echo "✓ $(BINARY_NAME) installed to $(HOME)/.local/bin/$(BINARY_NAME)"
	@case ":$$PATH:" in \
		*":$$HOME/.local/bin:"*) ;; \
		*) echo ""; \
		   echo "⚠ Warning: $$HOME/.local/bin is not in your PATH"; \
		   echo "Add this to your ~/.bashrc or ~/.zshrc:"; \
		   echo "  export PATH=\"\$$PATH:\$$HOME/.local/bin\"" ;; \
	esac

test: ## Run tests
	@echo "Running tests..."
	@OUTPUT=$$(go test -race -cover ./... 2>&1 | grep -v 'no such tool "covdata"'); \
	RESULT=$$?; \
	if echo "$$OUTPUT" | grep -q "^FAIL"; then \
		echo "$$OUTPUT"; \
		exit 1; \
	else \
		PASSED=$$(echo "$$OUTPUT" | grep -c "^ok"); \
		echo "✓ All $$PASSED packages passed"; \
	fi

lint: ## Run linters
	@echo "Running linters..."
	@go tool golangci-lint run

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
sx: build ## Build and run sx (usage: make sx install, make sx -- install --repair)
	@$(BUILD_DIR)/$(BINARY_NAME) $(filter-out $@,$(MAKECMDGOALS))

# Catch-all target to allow passing args to sx (eg: make sx install)
%:
	@:

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

init: ## Initialize development environment (download deps)
	@echo "Initializing development environment..."
	@echo "Downloading dependencies..."
	@go mod download
	@echo ""
	@echo "✓ Development environment initialized"
	@echo "  Tools available via 'go tool <name>' (see go.mod tool section)"

prepush: format lint test build ## Run before pushing (format, lint, test, build)

postpull: init ## Run after pulling (install tools and download dependencies)

demo: build ## Generate demo GIF (requires vhs)
	@echo "Generating demo..."
	@which vhs > /dev/null || (echo "vhs not found. Install from https://github.com/charmbracelet/vhs" && exit 1)
	@$(BUILD_DIR)/$(BINARY_NAME) remove test-driven-development --yes 2>/dev/null || true
	@DEMO_HOME=$$(mktemp -d) && \
	mkdir -p "$$DEMO_HOME/.claude" && \
	HOME="$$DEMO_HOME" PATH="$(CURDIR)/$(BUILD_DIR):$$PATH" PS1="$$ " vhs docs/demo.tape && \
	rm -rf "$$DEMO_HOME"
	@echo "Generated: docs/demo.gif"

logs:  ## Tail the sx log file, which is often ~/.cache/sx/sx.log, but not always
	@LOG_FILE="$$(sx config 2>/dev/null | grep 'Log File:' | cut -d: -f2 | tr -d ' ')"; \
	echo "Tailing $$LOG_FILE\n"; \
	tail -f "$$LOG_FILE"
