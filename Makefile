.PHONY: build test install clean

BINARY=legacy-map
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) .

test:
	go test ./... -v

install:
	go install -ldflags "-X main.version=$(VERSION)" .

clean:
	rm -f $(BINARY)
