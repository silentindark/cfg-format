BINARY    := cfg-format
BUILD_DIR := build
GOBIN     := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN     := $(shell go env GOPATH)/bin
endif

# Embed the nearest git tag, or fall back to the short commit hash.
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS   := -ldflags "-X main.version=$(VERSION)"

.DEFAULT_GOAL := help

.PHONY: help build install uninstall test fmt vet tidy check clean

help:
	@echo "Usage: make <target>"
	@echo ""
	@echo "  build      compile the binary to ./$(BUILD_DIR)/$(BINARY)"
	@echo "  install    install $(BINARY) to $(GOBIN)"
	@echo "  uninstall  remove $(BINARY) from $(GOBIN)"
	@echo "  test       run all tests"
	@echo "  fmt        format Go source files"
	@echo "  vet        run go vet"
	@echo "  tidy       tidy go.mod / go.sum"
	@echo "  check      fmt + vet + test (run before committing)"
	@echo "  clean      remove local build artifact"

build:
	mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) .

install: build
	cp $(BUILD_DIR)/$(BINARY) $(GOBIN)/$(BINARY)
	@echo "Installed $(BINARY) to $(GOBIN)"

uninstall:
	rm -f $(GOBIN)/$(BINARY)

test:
	go test ./...

fmt:
	gofmt -w -s .

vet:
	go vet ./...

tidy:
	go mod tidy

check: fmt vet test

clean:
	rm -rf $(BUILD_DIR)
