# kubectl container-status Makefile

# Variables
BINARY_NAME=kubectl-container_status
VERSION?=v0.1.0
BUILD_DIR=bin
INSTALL_DIR=/usr/local/bin

# Build settings
GOOS?=$(shell go env GOOS)
GOARCH?=$(shell go env GOARCH)
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build clean test install uninstall fmt vet mod-tidy help

# Default target
all: clean test build

## Build the binary
build:
	@echo "Building $(BINARY_NAME) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) cmd/main.go
	@echo "Binary built: $(BUILD_DIR)/$(BINARY_NAME)"

## Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	@GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 cmd/main.go
	@GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 cmd/main.go
	@GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 cmd/main.go
	@GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 cmd/main.go
	@GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe cmd/main.go
	@echo "All binaries built in $(BUILD_DIR)/"

## Run tests
test:
	@echo "Running tests..."
	@go test -v ./pkg/...

## Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	@go test -v -coverprofile=coverage.out ./pkg/...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

## Install the binary to system PATH
install: build
	@echo "Installing $(BINARY_NAME) to $(INSTALL_DIR)..."
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/
	@echo "Installation complete. Run 'kubectl container-status --help' to verify."

## Uninstall the binary from system PATH
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	@sudo rm -f $(INSTALL_DIR)/$(BINARY_NAME)
	@echo "Uninstallation complete."

## Clean build artifacts
clean:
	@echo "Cleaning up..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "Clean complete."

## Format Go code
fmt:
	@echo "Formatting Go code..."
	@go fmt ./...

## Run Go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

## Update Go modules
mod-tidy:
	@echo "Tidying Go modules..."
	@go mod tidy

## Run linter (requires golangci-lint)
lint:
	@echo "Running golangci-lint..."
	@golangci-lint run

## Run development build and test against local cluster
dev-test: build
	@echo "Testing against current kubectl context..."
	@kubectl config current-context
	@echo "Running basic functionality test..."
	@./$(BUILD_DIR)/$(BINARY_NAME) --help

## Create a release package
release: clean test build-all
	@echo "Creating release package..."
	@mkdir -p release
	@tar -czf release/$(BINARY_NAME)-$(VERSION)-linux-amd64.tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-linux-amd64
	@tar -czf release/$(BINARY_NAME)-$(VERSION)-linux-arm64.tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-linux-arm64
	@tar -czf release/$(BINARY_NAME)-$(VERSION)-darwin-amd64.tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-darwin-amd64
	@tar -czf release/$(BINARY_NAME)-$(VERSION)-darwin-arm64.tar.gz -C $(BUILD_DIR) $(BINARY_NAME)-darwin-arm64
	@zip -j release/$(BINARY_NAME)-$(VERSION)-windows-amd64.zip $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe
	@echo "Release packages created in release/"

## Show help
help:
	@echo "Available targets:"
	@echo "  build         Build the binary for current platform"
	@echo "  build-all     Build binaries for all supported platforms"
	@echo "  test          Run tests"
	@echo "  test-coverage Run tests with coverage report"
	@echo "  install       Install binary to system PATH"
	@echo "  uninstall     Remove binary from system PATH"
	@echo "  clean         Clean build artifacts"
	@echo "  fmt           Format Go code"
	@echo "  vet           Run go vet"
	@echo "  mod-tidy      Update Go modules"
	@echo "  lint          Run golangci-lint (requires installation)"
	@echo "  dev-test      Build and test basic functionality"
	@echo "  release       Create release packages"
	@echo "  help          Show this help message" 