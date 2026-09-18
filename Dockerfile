# ==========================================
# Stage 1: Build Go Backend Binary
# ==========================================
FROM golang:1.25-bookworm AS builder

WORKDIR /src

# Cache dependencies
COPY go.mod ./
RUN go mod download || true

# Copy source code
COPY . .

# Build statically-linked / optimized server binary
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/nginx-builder ./cmd/server

# ==========================================
# Stage 2: Runtime & Nginx Build Environment
# ==========================================
FROM debian:bookworm-slim

LABEL maintainer="OpenClaw Assistant" \
      description="Web-based Nginx online compiler and builder service with OpenSSL and PCRE source support"

ENV DEBIAN_FRONTEND=noninteractive \
    PORT=8090 \
    DATA_DIR=/app/data \
    MAX_CONCURRENT_JOBS=2 \
    JOB_TIMEOUT_MINUTES=20 \
    LANG=C.UTF-8

# Install official Nginx build dependencies, C toolchain, and Perl (required for OpenSSL Configure)
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    gcc \
    g++ \
    make \
    curl \
    ca-certificates \
    tar \
    gzip \
    perl \
    libpcre2-dev \
    libpcre3-dev \
    libssl-dev \
    zlib1g-dev \
    libxml2-dev \
    libxslt1-dev \
    libgd-dev \
    libgeoip-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary from builder
COPY --from=builder /bin/nginx-builder /app/nginx-builder

# Create data directories
RUN mkdir -p /app/data/builds /app/data/cache

EXPOSE 8090

VOLUME ["/app/data"]

ENTRYPOINT ["/app/nginx-builder"]
