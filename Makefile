APP_NAME := antenna-rotator-server
BINARY := $(APP_NAME)
BIN_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
PREFIX ?= /usr/local

.PHONY: all build build-linux build-windows test vet lint clean run-sim install

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)

build-linux:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-linux-amd64

# Cross-compile a native Windows binary (talks to COM ports directly).
# Run it with: ROTATOR_PORT=COM3 ./bin/antenna-rotator-server-windows-amd64.exe
build-windows:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-windows-amd64.exe

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run

# Run locally against the in-memory simulated rotator
run-sim: build
	SIMULATION=true ./$(BIN_DIR)/$(BINARY)

clean:
	rm -rf $(BIN_DIR) $(BINARY) $(BINARY)-linux-amd64 $(BINARY)-windows-amd64.exe

# Install the binary and systemd unit (run `make build` first, then
# `sudo make install`). An existing /etc/$(APP_NAME).env is never overwritten.
install:
	@test -f $(BIN_DIR)/$(BINARY) || { echo "Run 'make build' first"; exit 1; }
	install -m 0755 $(BIN_DIR)/$(BINARY) $(PREFIX)/bin/$(BINARY)
	install -m 0644 deploy/$(APP_NAME).service /etc/systemd/system/$(APP_NAME).service
	test -f /etc/$(APP_NAME).env || install -m 0644 deploy/$(APP_NAME).env /etc/$(APP_NAME).env
	systemctl daemon-reload
