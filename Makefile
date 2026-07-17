SHELL := /bin/bash

MODULES := shared services/cli
PKGS    := ./shared/... ./services/cli/...
BIN     := bin

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
           -X github.com/ClassMesh/classmesh/internal/version.Version=$(VERSION) \
           -X github.com/ClassMesh/classmesh/internal/version.Commit=$(COMMIT) \
           -X github.com/ClassMesh/classmesh/internal/version.Date=$(DATE)

.PHONY: build cgo-check test coverage vet fmt tidy verify lint vuln clean

## build: compile every service binary into ./bin (cgo-free, single static binary)
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

## test: run every test in every workspace module with the race detector
test:
	go test -race -count=1 $(PKGS)

## coverage: like test but emits coverage.out at the repo root
coverage:
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic $(PKGS)

## vet: static analysis across every workspace module
vet:
	go vet $(PKGS)

## fmt: format every Go file in the workspace
fmt:
	gofmt -w -l shared services

## tidy: run go mod tidy in every module
tidy:
	@for m in $(MODULES); do (cd $$m && go mod tidy); done

## verify: verify module checksums in every module
verify:
	@for m in $(MODULES); do (cd $$m && go mod verify); done

## lint: run golangci-lint in every module
lint:
	@for m in $(MODULES); do (cd $$m && golangci-lint run --config=$(CURDIR)/.golangci.yml ./...); done

## vuln: scan every module for known vulnerabilities (go install golang.org/x/vuln/cmd/govulncheck@v1.5.0)
vuln:
	@for m in $(MODULES); do (cd $$m && govulncheck ./...); done

## clean: remove build artifacts
clean:
	rm -rf $(BIN) coverage.out

## bench: run benchmarks across workspace modules
bench:
	go test -bench=. -benchmem -run=^$$ ./shared/pkg/stage/rules/... ./shared/pkg/stage/schema/... ./shared/pkg/engine/... ./shared/pkg/classifier/... ./shared/pkg/logfields/... ./shared/pkg/tokenizer/wordpiece/... ./shared/pkg/sink/jsonl/... ./shared/pkg/source/textfile/... ./shared/pkg/source/jsonl/...
