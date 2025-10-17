# YTMP3 API (Go)

YouTube → MP3 conversion API with async jobs, audio-only downloads, and queue-backed worker pools.

## Features
- Instant metadata via oEmbed + duration API; yt-dlp fallback
- Background audio-only download (best available stream)
- Asynchronous conversion (POST /convert returns 202 with queue_position)
- Duplicate-friendly: single download per URL (asset hash); conversion dedup per variant (url+quality+trim)
- Status with progress, queue position, and download URL
- Rate limiting (global + per-IP), optional API keys and priority queues
- Redis-backed sessions (docker-compose provided)

## Quick start

### Local (Go)
- Requirements: Go 1.22+, ffmpeg, yt-dlp
- Build & run:
```bash
go build -o bin/ytmp3api ./cmd/ytmp3
./bin/ytmp3api
```
- Defaults: CONVERSIONS_DIR=/tmp/conversions (creates streams/ and outputs/)

### Docker
```bash
docker-compose up -d --build
```
- API on http://localhost:8080
- Redis on redis://localhost:6379

## Configuration (env)

Environment variables configure performance, security, and behavior. Defaults are shown in parentheses.

- WORKER_POOL_SIZE (20): Number of goroutines per worker pool (download/convert). Higher = more concurrency.
- JOB_QUEUE_CAPACITY (1000): Max pending jobs per priority queue before new requests get 503.
- MAX_JOB_RETRIES (3): Automatic retries per job with exponential backoff.

- REQUESTS_PER_SECOND (100), BURST_SIZE (200): Global rate limit token bucket.
- PER_IP_RPS (10), PER_IP_BURST (20): Per-client-IP rate limit.

- REDIS_ADDR, REDIS_PASSWORD, REDIS_DB: If REDIS_ADDR is reachable, sessions/dedup use Redis instead of memory.

- YTDLP_TIMEOUT (90s): Timeout for yt-dlp metadata fallback.
- YTDLP_DOWNLOAD_TIMEOUT (30m): Max time for downloading a single stream.

- FFMPEG_MODE (CBR): Encoding mode CBR or VBR.
- FFMPEG_CBR_BITRATE (192k): Bitrate when using CBR (e.g., 128k/192k/320k).
- FFMPEG_VBR_Q (5): VBR quality (LAME scale; lower number = higher quality).
- FFMPEG_THREADS (0): Threads for ffmpeg; 0 lets ffmpeg decide.

- MAX_CONCURRENT_DOWNLOADS (20): Max concurrent downloads (semaphore size).
- MAX_CONCURRENT_CONVERSIONS (20): Max concurrent conversions.

- CONVERSIONS_DIR (/tmp/conversions): Root dir; contains streams/ and outputs/ subdirs.
- UNCONVERTED_FILE_TTL (5m): Auto-clean old source streams.
- CONVERTED_FILE_TTL (10m): Auto-clean old converted files.

- REQUIRE_API_KEY (false): Enforce API key on all requests.
- API_KEYS (""): Comma-separated list of valid API keys.
- ALLOWED_ORIGINS (*): CORS AllowedOrigins list.

- OEMBED_ENDPOINT (https://www.youtube.com/oembed): Used for fast title/thumbnail.
- DURATION_API_ENDPOINT (https://ds2.ezsrv.net/api/getDuration): Used for fast duration.

- ALLOWED_DOMAINS (youtube.com,youtu.be): Only accept URLs from these hosts.
- MAX_CLIP_SECONDS (900): Reject clips longer than this (based on start/end/duration).
- IP_ALLOWLIST (""): Optional comma-separated client IPs to allow; empty = allow all.
- SHED_QUEUE_THRESHOLD (0): If total queued jobs exceed this, readiness returns 503 to shed load.


## Endpoints

### POST /prepare (202 Accepted)
Request:
```json
{ "url": "https://www.youtube.com/watch?v=VIDEO_ID" }
```
Response:
```json
{
  "conversion_id": "conv_...",
  "status": "created",
  "metadata": {"title":"...","duration":180,"thumbnail":"..."},
  "message": "Metadata fetched successfully. Stream is downloading in background."
}
```

### POST /convert (202 Accepted)
Request:
```json
{ "conversion_id": "conv_...", "quality": "320", "start_time": "00:01:30", "end_time": "00:05:00" }
```
Response (queued):
```json
{ "conversion_id":"conv_...", "status":"queued_for_conversion", "queue_position": 3, "message": "Conversion request accepted and queued." }
```
Response (fast-complete if variant exists):
```json
{ "conversion_id":"conv_...", "status":"completed", "queue_position": 0, "message": "Reused existing converted output." }
```

### GET /status/{id}
```json
{
  "conversion_id": "conv_...",
  "status": "completed|preparing|downloading|converting|failed|queued_for_conversion",
  "download_progress": 85,
  "conversion_progress": 100,
  "download_url": "/download/conv_....mp3",
  "queue_position": 0
}
```

### GET /download/{id}.mp3
Streams the MP3 (Range supported). Use the URL from `download_url` in status.

## Behavior and performance
- Prepare returns immediately with metadata (oEmbed + ds2) and starts background audio download.
- Convert returns 202 and runs when the audio is ready; FIFO inside priority tiers.
- With defaults: ~20 concurrent downloads and ~20 concurrent conversions. Tune via env.
- For bulk traffic: enable Redis and scale horizontally; move queues to Redis Streams/RabbitMQ for multi-worker distribution; use CDN for downloads (optionally S3 if allowed).

## Development
- Format and tidy:
```bash
gofmt -w .
go mod tidy
```
- Playground UI: open `web/playground.html` and set base URL.

## Testing
- Single flow:
```bash
./scripts/flow_report.sh "https://www.youtube.com/watch?v=..." immediate 128
```
- Load sample:
```bash
/workspace/bin/bench -base http://127.0.0.1:8080 -n 100 -q 128 -delay 200ms -url "https://www.youtube.com/watch?v=..."
```
