# Multi-stage build for a small production image.
# Multi-arch aware: `docker buildx build --platform linux/amd64,linux/arm64 .`
# cross-compiles on the build host (no QEMU-emulated compile).
FROM --platform=$BUILDPLATFORM golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Allow configurable non-root user and dialout group
ARG APP_USER_UID=1000
ARG APP_USER_GID=1000
ARG APP_USERNAME=appuser
ARG APP_DIALOUT_GID=20

# Create the dialout group (if not present) and the app user
RUN groupadd -g ${APP_DIALOUT_GID} -f dialout \
	&& groupadd -g ${APP_USER_GID} ${APP_USERNAME} \
	&& useradd -u ${APP_USER_UID} -g ${APP_USER_GID} -M -N -s /usr/sbin/nologin ${APP_USERNAME} \
	&& usermod -aG dialout ${APP_USERNAME}

# Copy sources and build the binary (after sources are present)
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
	go build -ldflags "-s -w -X main.version=${VERSION}" -o /out/antenna-rotator-server . \
	&& chown ${APP_USER_UID}:${APP_USER_GID} /out/antenna-rotator-server

FROM scratch
ARG APP_USER_UID=1000
ARG APP_USER_GID=1000

# Copy CA certificates and the statically-linked binary from the builder stage.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# Copy passwd/group so the configured user and the 'dialout' group exist inside the scratch image
COPY --from=builder /etc/passwd /etc/group /etc/
COPY --from=builder --chown=${APP_USER_UID}:${APP_USER_GID} /out/antenna-rotator-server /usr/local/bin/antenna-rotator-server

# Drop privileges by using the configured non-root UID:GID
USER ${APP_USER_UID}:${APP_USER_GID}
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/antenna-rotator-server"]
