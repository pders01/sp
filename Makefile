.PHONY: all help build run test test-race coverage lint modernize fmt clean install deps check ci release release-snapshot mod-update bench

# Variables
BINARY_NAME=sp
BINARY_PATH=./$(BINARY_NAME)
MAIN_PATH=./cmd/sp
COVERAGE_FILE=coverage.out
COVERAGE_HTML=coverage.html

all: build

## help: Display this help message
help:
	@echo "$(BINARY_NAME)"
	@echo ""
	@echo "Available targets:"
	@awk 'BEGIN {FS = ":.*##"; printf "\n"} /^[a-zA-Z_-]+:.*?##/ { printf "  %-20s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

## build: Build sp binary
build:
	@echo "Building $(BINARY_NAME)..."
	@go build -o $(BINARY_PATH) $(MAIN_PATH)
	@echo "Build complete: $(BINARY_PATH)"

## run: Build and run sp
run: build
	@echo "Running $(BINARY_NAME)..."
	@$(BINARY_PATH)

## test: Run all tests
test:
	@echo "Running tests..."
	@go test -v ./...

## test-race: Run tests with race condition detection
test-race:
	@echo "Running tests with race detection..."
	@go test -race -v ./...

## coverage: Generate test coverage report
coverage:
	@echo "Generating coverage report..."
	@go test -coverprofile=$(COVERAGE_FILE) ./internal/...
	@go tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"
	@go tool cover -func=$(COVERAGE_FILE)

## lint: Run Go linters
lint:
	@echo "Running linters..."
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Running go vet instead..."; \
		go vet ./...; \
	fi
	@echo "Running gofmt..."
	@gofmt -l -s .

## modernize: Apply Go-idiom upgrades (any, min/max, range-over-int, etc.) via gopls.
##            Requires gopls on PATH; install with: go install golang.org/x/tools/gopls@latest
modernize:
	@echo "Running modernize via gopls..."
	@if ! command -v gopls > /dev/null; then \
		echo "gopls not installed. Install with: go install golang.org/x/tools/gopls@latest"; \
		exit 1; \
	fi
	@gopls codeaction -kind=source.fixAll -exec ./... 2>&1 | tee /tmp/sp-modernize.log; \
		echo "Done. Review with: git diff"

## fmt: Format Go code
fmt:
	@echo "Formatting code..."
	@go fmt ./...
	@gofmt -s -w .

## clean: Remove build artifacts and test cache
clean:
	@echo "Cleaning build artifacts..."
	@rm -f $(BINARY_PATH)
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	@go clean -testcache
	@echo "Clean complete"

## install: Install sp to GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME) to GOPATH/bin..."
	@go build -o $(shell go env GOBIN)/$(BINARY_NAME) $(MAIN_PATH) 2>/dev/null || \
		go build -o $(shell go env GOPATH)/bin/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Installation complete"

## deps: Download and tidy dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy
	@echo "Dependencies updated"

## bench: Run benchmarks
bench:
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem ./...

## mod-update: Update all dependencies to latest versions
mod-update:
	@echo "Updating dependencies to latest versions..."
	@go get -u ./...
	@go mod tidy
	@echo "Dependencies updated"

## check: Run all checks (lint, test, build)
check: lint test build
	@echo "All checks passed!"

## ci: Run CI pipeline
ci: deps lint test build
	@echo "CI pipeline complete"

## release: Create a new release using GoReleaser
release:
	@echo "Creating release with GoReleaser..."
	@goreleaser release --clean

## release-snapshot: Create a snapshot release using GoReleaser
release-snapshot:
	@echo "Creating snapshot release with GoReleaser..."
	@goreleaser release --snapshot --clean
