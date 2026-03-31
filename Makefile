BINARY := editor
MODULE  := github.com/anthonybrice/editor

.PHONY: all build test test-unit vet lint clean

all: build

build:
	go build -o $(BINARY) .

## test / test-unit — run all unit tests with race detector and verbose output
test: test-unit

test-unit:
	go test -race -v ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)
