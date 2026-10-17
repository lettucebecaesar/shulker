BINARY := shulker
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test lint fmt vet tidy bench clean

all: build test lint

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY)$(shell go env GOEXE) ./cmd/shulker

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint: vet
	golangci-lint run

tidy:
	go mod tidy

bench:
	go test ./internal/pipeline -run TestBenchmarkReport -v

clean:
	rm -f $(BINARY) $(BINARY).exe
