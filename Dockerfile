# Multi-stage build for a small production image
# Build stage (with libusb and build tools for gousb/cgo)
FROM golang:1.24 AS builder
WORKDIR /src

# Install build dependencies needed for cgo (libusb, pkg-config and build tools)
RUN apt-get update \
	&& apt-get install -y --no-install-recommends \
		build-essential \
		libusb-1.0-0-dev \
		pkg-config \
		ca-certificates \
	&& rm -rf /var/lib/apt/lists/*

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy sources and build (enable CGO for gousb)
COPY . .
# Build the main package only — using `./...` with `-o` pointing to a file fails when multiple packages are selected
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o /out/antenna-rotator-server .

# Final stage (minimal runtime)
FROM gcr.io/distroless/static
COPY --from=builder /out/antenna-rotator-server /usr/local/bin/antenna-rotator-server
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/antenna-rotator-server"]
