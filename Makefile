SHELL := /bin/bash

MODULES := shared services/cli
PKGS    := ./shared/... ./services/cli/...
BIN     := bin

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
           -X github.com/ClassMesh/classmesh/shared/pkg/version.Version=$(VERSION) \
           -X github.com/ClassMesh/classmesh/shared/pkg/version.Commit=$(COMMIT) \
           -X github.com/ClassMesh/classmesh/shared/pkg/version.Date=$(DATE)

.PHONY: build test coverage vet fmt tidy verify lint clean

## build: compile every service binary into ./bin
build:
	mkdir -p $(BIN)
	go build -ldflags '$(LDFLAGS)' -o $(BIN)/classmesh ./services/cli/cmd/classmesh

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

## clean: remove build artifacts
clean:
	rm -rf $(BIN) coverage.out

## bench: run benchmarks across workspace modules
bench:
	go test -bench=. -benchmem -run=^$$ ./shared/pkg/stage/rules/... ./shared/pkg/engine/...
