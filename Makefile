BINARY   := k0sind
PKG      := github.com/k0sproject/k0sind
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -s -w -X $(PKG)/internal/version.Version=$(VERSION)

.PHONY: build install test vet fmt e2e snapshot clean

## build: compile the binary into ./bin
build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) .

## install: install k0sind into $GOBIN (or $GOPATH/bin) for local use
install:
	go install -ldflags '$(LDFLAGS)' .

## test: run unit tests (no docker)
test:
	go test ./... -race

## vet: static analysis
vet:
	go vet ./...

## fmt: check formatting
fmt:
	gofmt -l .

## e2e: run the docker-backed end-to-end tests (requires a running docker)
e2e: build
	K0SIND_BIN=$(CURDIR)/bin/$(BINARY) go test -tags e2e ./test/e2e/... -v -timeout 20m

## snapshot: build release artifacts locally into ./dist without publishing
snapshot:
	goreleaser release --snapshot --clean

## clean: remove build artifacts
clean:
	rm -rf bin dist
