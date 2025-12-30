APP_NAME := antenna-rotator-server
BINARY := $(APP_NAME)
BIN_DIR := bin
DOCKER_IMAGE ?= $(APP_NAME):latest

.PHONY: all build build-linux test clean docker-build docker-push

all: build

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(BINARY)

build-linux:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o $(BIN_DIR)/$(BINARY)-linux-amd64

test:
	go test ./...

clean:
	rm -rf $(BIN_DIR) $(BINARY) $(BINARY)-linux-amd64

# Docker targets
docker-build:
	@echo "Building docker image (build will run in container)"
	docker build -t $(DOCKER_IMAGE) .

# Example usage: make docker-push DOCKER_REPO=myrepo/antenna-rotator-server:1.0.0
docker-push:
	@echo "About to push $(DOCKER_IMAGE) to $(DOCKER_REPO)"
	if [ -z "$(DOCKER_REPO)" ]; then echo "Set DOCKER_REPO to push the image"; exit 1; fi
	docker tag $(DOCKER_IMAGE) $(DOCKER_REPO)
	docker push $(DOCKER_REPO)
