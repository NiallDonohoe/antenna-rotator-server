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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-s -w" -o /out/antenna-rotator-server . \
	&& chown ${APP_USER_UID}:${APP_USER_GID} /out/antenna-rotator-server

FROM scratch
ARG APP_USER_UID=1000
ARG APP_USER_GID=1000
ARG APP_USERNAME=appuser

# Copy CA certificates and the statically-linked binary from the builder stage.
# The builder stage installs CA certs so we can copy them into this minimal image.
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# Copy passwd/group so the configured user and the 'dialout' group exist inside the scratch image
COPY --from=builder /etc/passwd /etc/group /etc/
COPY --from=builder --chown=${APP_USER_UID}:${APP_USER_GID} /out/antenna-rotator-server /usr/local/bin/antenna-rotator-server

# Drop privileges by using the configured non-root UID:GID
USER ${APP_USER_UID}:${APP_USER_GID}
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/antenna-rotator-server"]
