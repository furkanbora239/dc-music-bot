# ==========================================
# Build Stage
# ==========================================
FROM golang:alpine AS builder

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Compile optimized static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/dc-music-bot ./cmd/bot

# ==========================================
# Production Runtime Stage
# ==========================================
FROM alpine:3.21

# Install audio processing and runtime utilities
RUN apk add --no-cache \
    ca-certificates \
    ffmpeg \
    python3 \
    nodejs \
    curl \
    tzdata && \
    curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp && \
    chmod a+rx /usr/local/bin/yt-dlp

WORKDIR /app

# Copy binary from build stage
COPY --from=builder /bin/dc-music-bot /app/dc-music-bot

# Create non-root user
RUN adduser -D -u 1000 botuser && chown -R botuser:botuser /app
USER botuser

ENTRYPOINT ["/app/dc-music-bot"]
