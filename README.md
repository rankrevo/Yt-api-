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
- WORKER_POOL_SIZE (default 20)
- JOB_QUEUE_CAPACITY (default 1000)
- REQUESTS_PER_SECOND, BURST_SIZE (global rate limit)
- PER_IP_RPS, PER_IP_BURST (per-IP limit)
- REDIS_ADDR, REDIS_PASSWORD, REDIS_DB
- YTDLP_TIMEOUT, YTDLP_DOWNLOAD_TIMEOUT
- FFMPEG_* (MODE, CBR/VBR, THREADS)
- MAX_CONCURRENT_DOWNLOADS, MAX_CONCURRENT_CONVERSIONS
- CONVERSIONS_DIR (will contain streams/ and outputs/)
- REQUIRE_API_KEY, API_KEYS (comma separated)
- OEMBED_ENDPOINT, DURATION_API_ENDPOINT

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
