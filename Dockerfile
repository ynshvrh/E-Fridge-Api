# ==========================================
# Stage 1: Build binary
# ==========================================
FROM golang:alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Cache go modules
COPY go.mod go.sum ./
RUN go mod download

# Copy application sources
COPY . .

# Build static binary without CGO
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/bin/api ./cmd/api

# ==========================================
# Stage 2: Minimal runtime image
# ==========================================
FROM alpine:3.20

WORKDIR /app

# Install runtime dependencies and create non-root user
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S appgroup && adduser -S appuser -G appgroup

# Copy compiled binary from builder
COPY --from=builder /app/bin/api /app/api

# Run as non-root user
USER appuser:appgroup

EXPOSE 8080

ENV APP_ENV=production \
    PORT=8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/api"]
