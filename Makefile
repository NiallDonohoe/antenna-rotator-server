APP_NAME := antenna-rotator-server
BINARY := $(APP_NAME)
BIN_DIR := bin
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"
DOCKER_IMAGE ?= $(APP_NAME):latest

.PHONY: all build build-linux test vet lint clean run-sim docker-build docker-buildx docker-push

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)

build-linux:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BIN_DIR)/$(BINARY)-linux-amd64

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
	rm -rf $(BIN_DIR) $(BINARY) $(BINARY)-linux-amd64

# Docker targets
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE) .

# Multi-arch build (amd64 + arm64, e.g. for Raspberry Pi). Requires buildx.
# Add --push and set DOCKER_IMAGE to a registry tag to publish.
docker-buildx:
	docker buildx build --platform linux/amd64,linux/arm64 \
		--build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE) .

# Example usage: make docker-push DOCKER_REPO=myrepo/antenna-rotator-server:1.0.0
docker-push:
	@echo "About to push $(DOCKER_IMAGE) to $(DOCKER_REPO)"
	if [ -z "$(DOCKER_REPO)" ]; then echo "Set DOCKER_REPO to push the image"; exit 1; fi
	docker tag $(DOCKER_IMAGE) $(DOCKER_REPO)
	docker push $(DOCKER_REPO)
