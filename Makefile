.PHONY: all help build run test test-race coverage lint modernize fmt clean install deps check ci release release-snapshot mod-update bench privacy-terminal ux-terminal design-terminal design-capture privacy-fuzz vuln test-release

# Variables
BINARY_NAME=sp
BINARY_PATH=./$(BINARY_NAME)
MAIN_PATH=./cmd/sp
COVERAGE_FILE=coverage.out
COVERAGE_HTML=coverage.html
GOLANGCI_LINT_VERSION=v2.5.0

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
	@go test -v github.com/muesli/cancelreader

## test-race: Run tests with race condition detection
test-race:
	@echo "Running tests with race detection..."
	@go test -race -v -coverprofile=$(COVERAGE_FILE) ./...
	@go test -race -v github.com/muesli/cancelreader

## test-release: Validate the release helper without changing refs or contacting remotes
test-release:
	@bash -n scripts/release.sh
	@python3 -m unittest discover -s scripts -p 'test_release.py'

## coverage: Generate test coverage report
coverage:
	@echo "Generating coverage report..."
	@go test -coverprofile=$(COVERAGE_FILE) ./internal/...
	@go tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"
	@go tool cover -func=$(COVERAGE_FILE)

## lint: Run Go linters
lint:
	@echo "Running CI-pinned golangci-lint $(GOLANGCI_LINT_VERSION)..."
	@GOTOOLCHAIN=$$(go env GOVERSION) go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run --timeout=5m ./...

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

## privacy-terminal: Exercise private enable/edit/recover in a POSIX terminal using synthetic stores only
privacy-terminal:
	@mkdir -p .privacy-adversarial
	@go build -o .privacy-adversarial/sp-validation $(MAIN_PATH)
	@python3 scripts/privacy-terminal/main.py --binary .privacy-adversarial/sp-validation

## ux-terminal: Exercise integrated reader/editor/template/privacy UX in synthetic POSIX terminals
ux-terminal:
	@mkdir -p .privacy-adversarial
	@go build -o .privacy-adversarial/sp-ux-validation $(MAIN_PATH)
	@python3 scripts/ux-terminal/main.py --binary .privacy-adversarial/sp-ux-validation

## design-terminal: Exercise chronological/read-only/encrypted redesign flows in synthetic POSIX terminals
design-terminal:
	@mkdir -p .privacy-adversarial
	@go build -o .privacy-adversarial/sp-design-validation $(MAIN_PATH)
	@python3 -B scripts/design-terminal/main.py --binary .privacy-adversarial/sp-design-validation

## design-capture: Render synthetic TUI frames and separate static design concepts (ImageMagick required)
design-capture:
	@mkdir -p .privacy-adversarial/design-review/current
	@SP_DESIGN_CAPTURE_DIR="$(CURDIR)/.privacy-adversarial/design-review/current" go test ./internal/tui -run '^TestDesignCapture$$' -count=1
	@python3 scripts/design-capture/render.py --frames .privacy-adversarial/design-review/current/frames.json --output .privacy-adversarial/design-review/current
	@python3 scripts/design-capture/concepts.py --output .privacy-adversarial/design-review/concepts/frames.json
	@python3 scripts/design-capture/render.py --frames .privacy-adversarial/design-review/concepts/frames.json --output .privacy-adversarial/design-review/concepts --kind concept

## privacy-fuzz: Run five bounded record/private/append fuzz campaigns (20 seconds each)
privacy-fuzz:
	@set -e; for target in FuzzDecodeRecord FuzzStrictJSONCanonicalRoundtrip FuzzAppendBullets FuzzValidateAgeEnvelope FuzzDecodePrivateDocumentFollowup; do \
		GOMAXPROCS=2 go test ./internal/scratchpad -run='^$$' -fuzz="^$${target}$$" -fuzztime=20s -parallel=2; \
	done

## vuln: Check reachable dependency advisories using the selected project toolchain
vuln:
	@GOTOOLCHAIN=$$(go env GOVERSION) go run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

## check: Run all checks (lint, test, build)
check: lint test build
	@echo "All checks passed!"

## ci: Run CI pipeline
ci: deps lint test-race test-release vuln build
	@echo "CI pipeline complete"

## release: Create a new release using GoReleaser
release:
	@echo "Creating release with GoReleaser..."
	@goreleaser release --clean

## release-snapshot: Create a snapshot release using GoReleaser
release-snapshot:
	@echo "Creating snapshot release with GoReleaser..."
	@goreleaser release --snapshot --clean
