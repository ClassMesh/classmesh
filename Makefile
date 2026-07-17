SHELL := /bin/bash

PKGS := ./...
BIN  := bin

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
           -X github.com/ClassMesh/classmesh/internal/version.Version=$(VERSION) \
           -X github.com/ClassMesh/classmesh/internal/version.Commit=$(COMMIT) \
           -X github.com/ClassMesh/classmesh/internal/version.Date=$(DATE)

.PHONY: build cgo-check test coverage vet fmt tidy verify lint vuln bench clean

## build: compile the reference CLI into ./bin as one cgo-free static binary
build:
	mkdir -p $(BIN)
	CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o $(BIN)/classmesh ./cmd/classmesh

## cgo-check: assert the shipped binary built cgo-free: statically linked, no C deps.
## (`-race` tests still need cgo, so the guarantee is enforced on the artifact, not the test harness.)
cgo-check: build
	@if file $(BIN)/classmesh | grep -q 'statically linked'; then \
	  echo 'cgo-free OK: classmesh is statically linked (CGO_ENABLED=0)'; \
	else \
	  echo 'NOT cgo-free: classmesh is dynamically linked; a cgo dependency leaked in'; \
	  file $(BIN)/classmesh; exit 1; \
	fi

## test: run every test in the module with the race detector
test:
	go test -race -count=1 $(PKGS)

## coverage: like test but emits coverage.out at the repo root
coverage:
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic $(PKGS)

## vet: static analysis across the module
vet:
	go vet $(PKGS)

## fmt: format every Go file in the module
fmt:
	go fmt ./...

## tidy: synchronize the root module dependency files
tidy:
	go mod tidy

## verify: verify root module checksums
verify:
	go mod verify

## lint: run golangci-lint once from the module root
lint:
	golangci-lint run --config=$(CURDIR)/.golangci.yml ./...

## vuln: scan the root module for known vulnerabilities (go install golang.org/x/vuln/cmd/govulncheck@v1.5.0)
vuln:
	govulncheck ./...

## clean: remove build artifacts
clean:
	rm -rf $(BIN) coverage.out

## bench: run the committed benchmark package set
bench:
	go test -bench=. -benchmem -run=^$$ ./rules ./schema ./stream . ./internal/logfields ./internal/tokenizer/wordpiece ./stream/sink/jsonl ./stream/source/text ./stream/source/jsonl
