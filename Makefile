.PHONY: all build test lint clean run-tui run-web install help

# Build variables
BINARY_NAME := goru
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"
CGO_ENABLED := 0

# Default target
all: lint test build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@CGO_ENABLED=$(CGO_ENABLED) go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/goru

# Run tests
test:
	@echo "Running tests..."
	@go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...

# Run linter
lint:
	@echo "Running linter..."
	@golangci-lint run

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f $(BINARY_NAME) coverage.txt
	@rm -rf dist/

# Run in TUI mode
run-tui: build
	@./$(BINARY_NAME) --mode=tui

# Run in web mode
run-web: build
	@./$(BINARY_NAME) --mode=web

# Install binary to GOPATH/bin
install: build
	@echo "Installing $(BINARY_NAME)..."
	@go install ./cmd/goru

# Build for multiple platforms
release:
	@echo "Building releases..."
	@mkdir -p dist
	@for os in linux darwin windows; do \
		for arch in amd64 arm64; do \
			if [ "$$os" = "windows" ] && [ "$$arch" = "arm64" ]; then \
				continue; \
			fi; \
			echo "Building $$os/$$arch..."; \
			GOOS=$$os GOARCH=$$arch CGO_ENABLED=$(CGO_ENABLED) \
				go build $(LDFLAGS) \
				-o dist/$(BINARY_NAME)-$$os-$$arch$$( [ "$$os" = "windows" ] && echo ".exe" ) \
				./cmd/goru; \
		done; \
	done
	@cd dist && for f in *; do \
		if [ -f "$$f" ]; then \
			tar czf "$$f.tar.gz" "$$f"; \
			rm "$$f"; \
		fi; \
	done

# Development dependencies
deps:
	@echo "Installing development dependencies..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.57.0

# Generate mocks (if needed)
generate:
	@echo "Generating code..."
	@go generate ./...

# Benchmark
bench:
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...

# Help
help:
	@echo "Available targets:"
	@echo "  make all      - Run lint, test, and build"
	@echo "  make build    - Build the binary"
	@echo "  make test     - Run tests"
	@echo "  make lint     - Run linter"
	@echo "  make clean    - Clean build artifacts"
	@echo "  make run-tui  - Build and run in TUI mode"
	@echo "  make run-web  - Build and run in web mode"
	@echo "  make install  - Install binary to GOPATH/bin"
	@echo "  make release  - Build for multiple platforms"
	@echo "  make deps     - Install development dependencies"
	@echo "  make bench    - Run benchmarks"
	@echo "  make help     - Show this help message"