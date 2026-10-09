# ==========================================
# 1. Build Stage
# ==========================================
FROM golang:alpine AS builder

WORKDIR /src

# Install curl in build stage to fetch the latest yt-dlp binary
RUN apk add --no-cache ca-certificates curl

# YouTube breaks yt-dlp every few weeks, so this layer must NOT be cached
# forever. Bump it explicitly when bot-detection starts failing:
#   docker compose build --build-arg CACHEBUST=$(date +%s)
ARG CACHEBUST=0

# Fetch latest standalone yt-dlp binary into builder
RUN curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /bin/yt-dlp && \
    chmod a+rx /bin/yt-dlp

# Fetch the bgutil PO token provider plugin (activates when YTDLP_POT_BASE_URL
# is set; dormant otherwise). It is a plain zip that yt-dlp loads from its
# plugins folder, no pip required for the standalone binary.
RUN curl -L https://github.com/Brainicism/bgutil-ytdlp-pot-provider/releases/latest/download/bgutil-ytdlp-pot-provider.zip \
    -o /tmp/bgutil-ytdlp-pot-provider.zip

# Cache Go module dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Compile optimized static binary with stripped debug symbols and trimmed paths
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /bin/dc-music-bot \
    ./cmd/bot

# ==========================================
# 2. Production Minimal Runtime Stage
# ==========================================
FROM alpine:3.21

# Install runtime dependencies only (curl omitted - saves image size)
RUN apk add --no-cache \
    ca-certificates \
    ffmpeg \
    python3 \
    nodejs \
    tzdata && \
    rm -rf /var/cache/apk/* /tmp/*

WORKDIR /app

# Copy binaries from builder stage
COPY --from=builder /bin/yt-dlp /usr/local/bin/yt-dlp
COPY --from=builder /bin/dc-music-bot /app/dc-music-bot

# Create non-root user (security hardening)
RUN adduser -D -u 1000 -h /home/botuser botuser && \
    chown -R botuser:botuser /app /home/botuser

# yt-dlp loads plugins from ~/.config/yt-dlp/plugins (zip is imported as-is)
COPY --from=builder --chown=botuser:botuser /tmp/bgutil-ytdlp-pot-provider.zip /home/botuser/.config/yt-dlp/plugins/bgutil-ytdlp-pot-provider.zip

USER botuser
ENV HOME=/home/botuser \
    TMPDIR=/tmp

ENTRYPOINT ["/app/dc-music-bot"]
