# syntax=docker/dockerfile:1

FROM golang:1.22-bookworm AS builder
WORKDIR /app
COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go build -o /out/ytmp3api ./cmd/ytmp3

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg python3 python3-pip && rm -rf /var/lib/apt/lists/* \
    && pip3 install --no-cache-dir yt-dlp
WORKDIR /app
COPY --from=builder /out/ytmp3api /usr/local/bin/ytmp3api
ENV CONVERSIONS_DIR=/data \
    WORKER_POOL_SIZE=20 JOB_QUEUE_CAPACITY=1000 MAX_JOB_RETRIES=3 \
    REQUESTS_PER_SECOND=100 BURST_SIZE=200 PER_IP_RPS=10 PER_IP_BURST=20 \
    MAX_CONCURRENT_DOWNLOADS=20 MAX_CONCURRENT_CONVERSIONS=20
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --retries=3 CMD curl -fsS http://127.0.0.1:8080/health || exit 1
CMD ["ytmp3api"]
