# Stage 1: Build
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o otel-sampler ./cmd/server

# Create data directory for WAL and set permissions
RUN mkdir -p /app/data/wal && chmod -R 777 /app/data

# Stage 2: Final Image
FROM gcr.io/distroless/static-debian12

WORKDIR /

# Copy binary from builder
COPY --from=builder /app/otel-sampler /otel-sampler

# Copy data directory with permissions
COPY --from=builder /app/data /data

# Default ports
# OTLP gRPC: 4317
# OTLP HTTP: 4318
# Gossip: 7946
EXPOSE 4317 4318 7946

ENTRYPOINT ["/otel-sampler"]
