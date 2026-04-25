FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /frontend-build/web

# Install frontend dependencies from lockfile first for better layer caching.
COPY web/package.json web/pnpm-lock.yaml ./
RUN corepack enable && pnpm install --frozen-lockfile

# Copy the rest of the repository so the frontend build can emit assets into
# server/router/frontend/dist as expected by the backend.
WORKDIR /frontend-build
COPY . .
RUN corepack enable && cd web && pnpm release

FROM --platform=$BUILDPLATFORM golang:1.26.1-alpine AS backend
WORKDIR /backend-build

# Install build dependencies.
RUN apk add --no-cache git ca-certificates

# Copy go mod files and download dependencies (cached layer).
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code and the prebuilt frontend assets from the frontend stage.
COPY . .
COPY --from=frontend /frontend-build/server/router/frontend/dist ./server/router/frontend/dist

ARG TARGETOS TARGETARCH VERSION=dev COMMIT=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build \
      -trimpath \
      -ldflags="-s -w -X github.com/usememos/memos/internal/version.Version=${VERSION} -X github.com/usememos/memos/internal/version.Commit=${COMMIT} -extldflags '-static'" \
      -tags netgo,osusergo \
      -o memos \
      ./cmd/memos

# Use minimal Alpine with security updates
FROM alpine:3.21 AS monolithic

# Install runtime dependencies and create non-root user in single layer
RUN apk add --no-cache tzdata ca-certificates su-exec && \
    addgroup -g 10001 -S nonroot && \
    adduser -u 10001 -S -G nonroot -h /var/opt/memos nonroot && \
    mkdir -p /var/opt/memos /usr/local/memos && \
    chown -R nonroot:nonroot /var/opt/memos

# Copy binary and entrypoint to /usr/local/memos
COPY --from=backend /backend-build/memos /usr/local/memos/memos
COPY --from=backend --chmod=755 /backend-build/scripts/entrypoint.sh /usr/local/memos/entrypoint.sh

# Run as root to fix permissions, entrypoint will drop to nonroot
USER root

# Set working directory to the writable volume
WORKDIR /var/opt/memos

# Data directory
VOLUME /var/opt/memos

ENV TZ="UTC" \
    MEMOS_PORT="5230"

EXPOSE 5230

ENTRYPOINT ["/usr/local/memos/entrypoint.sh", "/usr/local/memos/memos"]
