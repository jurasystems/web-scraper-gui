# Makefile for Web Crawler GUI

# Variables
BINARY_NAME=crawler
BUILD_DIR=dist
MAIN_FILE=main.go
GO=go

# Default target
.PHONY: all
all: build

# Build the application
.PHONY: build
build:
	mkdir -p $(BUILD_DIR)
	$(GO) build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_FILE)

# Run the application
.PHONY: run
run:
	$(GO) run .

# Clean build artifacts
.PHONY: clean
clean:
	rm -rf $(BUILD_DIR)

# Install dependencies
.PHONY: deps
deps:
	$(GO) mod download

# Build for different platforms
.PHONY: build-all
build-all: build-linux build-windows build-mac

.PHONY: build-linux
build-linux:
	mkdir -p $(BUILD_DIR)/linux
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/linux/$(BINARY_NAME) $(MAIN_FILE)

.PHONY: build-windows
build-windows:
	mkdir -p $(BUILD_DIR)/windows
	GOOS=windows GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/windows/$(BINARY_NAME).exe $(MAIN_FILE)

.PHONY: build-mac
build-mac:
	mkdir -p $(BUILD_DIR)/macos
	GOOS=darwin GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/macos/$(BINARY_NAME) $(MAIN_FILE)

# Development helpers
.PHONY: fmt
fmt:
	$(GO) fmt ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: test
test:
	$(GO) test -v ./...

# Help command
.PHONY: help
help:
	@echo "Available commands:"
	@echo "  make run          - Run the application"
	@echo "  make build        - Build the application"
	@echo "  make clean        - Remove build artifacts"
	@echo "  make deps         - Download dependencies"
	@echo "  make fmt          - Format the code"
	@echo "  make test         - Run tests"
	@echo "  make build-all    - Build for Linux, Windows, and macOS"