# Multi-stage build for a small production image
FROM golang:1.24 AS builder
WORKDIR /src

# Install build dependencies
RUN apt-get update \
	&& apt-get install -y --no-install-recommends \
		build-essential \
		pkg-config \
		ca-certificates \
	&& rm -rf /var/lib/apt/lists/*

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy sources and build
COPY . .
# Build the main package only — using `./...` with `-o` pointing to a file fails when multiple packages are selected
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o /out/antenna-rotator-server .

FROM scratch
# Copy CA certificates and the statically-linked binary from the builder stage.
# The builder stage installs CA certs so we can copy them into this minimal image.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder --chown=1000:1000 /out/antenna-rotator-server /usr/local/bin/antenna-rotator-server
# Drop privileges by using a numeric non-root UID (no user database in scratch)
USER 1000:1000
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/antenna-rotator-server"]
