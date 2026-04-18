BINARY    := cfg-format
BUILD_DIR := build
MODULE    := cfg-format
GOBIN     := $(shell go env GOPATH)/bin

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
	go build -o $(BUILD_DIR)/$(BINARY) .

install:
	go install .

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
