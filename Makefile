# Makefile for container-runtime-tools.
#
# Targets are thin wrappers around the Go toolchain and shellcheck so that local
# use and CI run the exact same commands.

GO        ?= go
GOFLAGS   ?=
BIN_DIR   ?= bin
SHELLCHECK ?= shellcheck

# Scripts linted by the `lint` target.
SCRIPTS := $(shell git ls-files '*.sh')

.PHONY: all help build test vet fmt lint lint-shell check clean

all: build

help:
	@echo "Targets:"
	@echo "  build       - compile cmd/minirun and cmd/imagelint into $(BIN_DIR)/"
	@echo "  test        - run unit tests"
	@echo "  vet         - run go vet"
	@echo "  fmt         - format Go sources"
	@echo "  lint        - gofmt check + go vet + shellcheck"
	@echo "  lint-shell  - shellcheck every tracked .sh file"
	@echo "  check       - vet + test + lint (what CI runs)"
	@echo "  clean       - remove build output"

build:
	@mkdir -p $(BIN_DIR)
	$(GO) build $(GOFLAGS) -o $(BIN_DIR)/minirun ./cmd/minirun
	$(GO) build $(GOFLAGS) -o $(BIN_DIR)/imagelint ./cmd/imagelint

test:
	$(GO) test $(GOFLAGS) ./...

vet:
	$(GO) vet $(GOFLAGS) ./...

fmt:
	$(GO) fmt ./...

lint: vet lint-shell
	@echo "==> checking gofmt"
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

lint-shell:
	@if [ -z "$(SCRIPTS)" ]; then echo "no shell scripts to lint"; exit 0; fi
	$(SHELLCHECK) --severity=style --external-sources $(SCRIPTS)

check: vet test lint

clean:
	rm -rf $(BIN_DIR)
