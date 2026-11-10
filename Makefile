SHELL := sh

GO ?= go
EXE :=
ifeq ($(OS),Windows_NT)
EXE := .exe
endif

BIN := bin/infradiff$(EXE)
TOOLS_BIN := $(CURDIR)/.bin
GOLANGCI_LINT := $(TOOLS_BIN)/golangci-lint$(EXE)
GOLANGCI_LINT_VERSION ?= v2.15.0

.PHONY: all build test fmt vet lint cross clean

all: build

build:
	@mkdir -p bin
	$(GO) build -trimpath -o $(BIN) ./cmd/infradiff

test:
	$(GO) test ./...

fmt:
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "gofmt needs to run on:"; \
		echo "$$out"; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

lint:
	@mkdir -p .bin
	@if [ ! -x "$(GOLANGCI_LINT)" ]; then \
		echo "installing golangci-lint $(GOLANGCI_LINT_VERSION) into .bin"; \
		GOBIN="$(TOOLS_BIN)" $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION); \
	fi
	$(GOLANGCI_LINT) run ./...

cross:
	@mkdir -p bin/cross
	@for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do \
		os=$${target%/*}; \
		arch=$${target#*/}; \
		suffix=""; \
		if [ "$$os" = "windows" ]; then suffix=".exe"; fi; \
		echo "building $$os/$$arch"; \
		GOOS=$$os GOARCH=$$arch $(GO) build -trimpath \
			-o "bin/cross/infradiff-$$os-$$arch$$suffix" ./cmd/infradiff || exit 1; \
	done

clean:
	rm -rf bin
