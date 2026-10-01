# ---------------------------------------------------
# Stage 1: Build binary
# ---------------------------------------------------
FROM golang:alpine AS builder

WORKDIR /app

# Use fast Alpine mirror and install only required build tools
RUN sed -i 's/dl-cdn.alpinelinux.org\/alpine/mirror.yandex.ru\/mirrors\/alpine/g' /etc/apk/repositories && \
    apk add --no-cache git ca-certificates tzdata gcc musl-dev

# Download dependencies with cache mount
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY . .

# Build binary with CGO enabled (required for github.com/mattn/go-sqlite3) and build cache
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 GOOS=linux go build -ldflags="-w -s" -o /app/bin/bot ./cmd/bot

# ---------------------------------------------------
# Stage 2: Final production image
# ---------------------------------------------------
FROM alpine:3.20

WORKDIR /app

# Certificates for HTTPS requests and tzdata for timezone support with fast mirror
RUN sed -i 's/dl-cdn.alpinelinux.org\/alpine/mirror.yandex.ru\/mirrors\/alpine/g' /etc/apk/repositories && \
    apk --no-cache add ca-certificates tzdata

# Create unprivileged user and group
RUN addgroup -S appgroup && adduser -S appuser -G appgroup

# Create directories for persistent database and assets
RUN mkdir -p /app/data /app/assets && chown -R appuser:appgroup /app

# Copy compiled binary from builder
COPY --from=builder /app/bin/bot /app/bot

# Copy assets
COPY assets/ /app/assets/

USER appuser

# Entrypoint
CMD ["/app/bot"]