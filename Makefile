BINARY  := streams
CMD     := ./cmd/streams
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.Version=$(VERSION)"

.PHONY: build test run clean

build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD)

test:
	go test ./...

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
