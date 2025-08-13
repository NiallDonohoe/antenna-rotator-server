APP_NAME := antenna-rotator-server

.PHONY: all clean linux

all: linux

linux:
    GOOS=linux GOARCH=amd64 go build -o $(APP_NAME)_linux_amd64

clean:
    rm -