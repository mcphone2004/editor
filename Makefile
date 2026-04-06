BINARY := bin/editor
MODULE  := github.com/anthonybrice/editor

.PHONY: all build test test-unit test-e2e vet lint clean

all: build

build:
	go build -o $(BINARY) .

## test-unit — unit tests only (no E2E); fast, no build tag required
test-unit:
	go test -race -v ./...

## test-e2e — full UI stack tests; requires -tags e2e
test-e2e:
	go test -race -v -tags e2e ./ui/...

## test — run all tests (unit + E2E)
test: test-unit test-e2e

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)
