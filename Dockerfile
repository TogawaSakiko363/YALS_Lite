# Multi-stage build for YALS Lite
# Stage 1: Build the Go application
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o yals \
    ./cmd/main.go

# Stage 2: Create minimal runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    iputils \
    bind-tools \
    traceroute \
    mtr \
    curl \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1000 yals && \
    adduser -D -u 1000 -G yals yals

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/yals /app/yals

# Copy web files
COPY --from=builder /build/web /app/web

# Copy default config
COPY --from=builder /build/config.yaml /app/config.yaml.example

# Create config directory
RUN mkdir -p /app/config && \
    chown -R yals:yals /app

# Switch to non-root user
USER yals

# Expose port
EXPOSE 8080

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/ || exit 1

# Run the application
ENTRYPOINT ["/app/yals"]
