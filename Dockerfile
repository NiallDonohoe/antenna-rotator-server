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

# Final stage (include runtime libs like libusb)
FROM debian:bookworm-slim
# Install the runtime libusb package and certificates
RUN apt-get update \
	&& apt-get install -y --no-install-recommends \
		libusb-1.0-0 \
		ca-certificates \
	&& rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/antenna-rotator-server /usr/local/bin/antenna-rotator-server
# Create non-root user and set ownership
RUN groupadd -r app && useradd -r -g app app && chown app:app /usr/local/bin/antenna-rotator-server
USER app:app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/antenna-rotator-server"]
