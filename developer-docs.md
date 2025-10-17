# Frontend Integration Guide

This API converts YouTube to MP3 using a two-step, asynchronous flow.

## Typical UX flow
1. User pastes a YouTube URL.
2. Call POST /prepare → get `conversion_id` + metadata instantly.
3. Immediately call POST /convert with the `conversion_id` and selected quality.
4. Poll GET /status/{conversion_id} every 2–5s.
5. When `status=completed`, use `download_url` to download.

## Example calls

### Prepare
```js
const base = 'http://localhost:8080';
const res = await fetch(`${base}/prepare`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', 'X-API-Key': 'free_123' },
  body: JSON.stringify({ url: 'https://www.youtube.com/watch?v=VIDEO_ID' })
});
const data = await res.json();
const id = data.conversion_id;
```

### Convert (queue)
```js
await fetch(`${base}/convert`, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json', 'X-API-Key': 'free_123' },
  body: JSON.stringify({ conversion_id: id, quality: '320', start_time: '', end_time: '' })
});
```

### Status (progress + queue)
```js
const s = await fetch(`${base}/status/${id}`).then(r => r.json());
// s.status: 'queued_for_conversion' | 'downloading' | 'converting' | 'completed' | 'failed'
// s.download_progress, s.conversion_progress, s.queue_position
```

### Download
```js
if (s.status === 'completed' && s.download_url) {
  window.location.href = `${base}${s.download_url}`;
}
```

## Progress UI tips
- Show metadata immediately after /prepare (title/thumbnail/duration).
- While queued: show queue_position and spinner.
- During download/conversion: show progress bars from /status.
- On completion: enable a Download button.

## Error handling
- If /convert returns 202 but the job later fails, /status will show `status=failed` and may set `error`.
- Retry policy: re-queue /convert once after 30–60s if failed due to transient errors.
- Handle invalid/private videos: /prepare may succeed on metadata but download can fail; reflect errors from /status.

## API keys and priorities
- Send `X-API-Key` if your server requires it.
- Premium keys may be prioritized in queue; surface faster ETAs to users.

## CORS
- API sets CORS based on `ALLOWED_ORIGINS`. For local dev, set `*` or your host.

## Notes
- Conversion qualities: 128/192/256/320 (CBR) by default.
- Time range: set `start_time` and/or `end_time` as `HH:MM:SS`.
- Identical requests may complete instantly if the variant was previously converted.
