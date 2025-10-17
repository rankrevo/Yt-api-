package downloader

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var percentRe = regexp.MustCompile(`(\d{1,3})%`)

type ProgressFunc func(pct int)

type Config struct {
	YtDLPTimeout        time.Duration
	DownloadTimeout     time.Duration
	OEmbedEndpoint      string
	DurationAPIEndpoint string
}

type Downloader struct {
	cfg Config
	sem chan struct{}
}

func New(cfg Config, maxConcurrent int) *Downloader {
	return &Downloader{cfg: cfg, sem: make(chan struct{}, maxConcurrent)}
}

func (d *Downloader) withPermit(fn func() error) error {
	d.sem <- struct{}{}
	defer func() { <-d.sem }()
	return fn()
}

func (d *Downloader) FetchMetadata(ctx context.Context, videoURL string) (title, thumbnail string, durationSeconds int, err error) {
	// Try fast HTTP-based fetch first (oEmbed title/thumbnail + external duration API),
	// then fall back to yt-dlp if either fails to provide usable data.
	type metaResult struct {
		title string
		thumb string
		dur   int
		err   error
	}

	// Small, snappy timeout for HTTP metadata calls
	httpTimeout := 5 * time.Second
	httpCtx, httpCancel := context.WithTimeout(ctx, httpTimeout)
	defer httpCancel()

	// Run both HTTP calls concurrently
	chO := make(chan metaResult, 1)
	chD := make(chan metaResult, 1)

	go func() {
		t, th, e := d.fetchOEmbed(httpCtx, d.cfg.OEmbedEndpoint, videoURL)
		chO <- metaResult{title: t, thumb: th, dur: 0, err: e}
	}()
	go func() {
		dur, e := d.fetchDuration(httpCtx, d.cfg.DurationAPIEndpoint, videoURL)
		chD <- metaResult{dur: dur, err: e}
	}()

	var o metaResult
	var dd metaResult
	// Collect with simple time-bound waits; context ensures timely cancel
	select {
	case o = <-chO:
	case <-httpCtx.Done():
		o = metaResult{err: httpCtx.Err()}
	}
	select {
	case dd = <-chD:
	case <-httpCtx.Done():
		dd = metaResult{err: httpCtx.Err()}
	}

	// If we got anything useful from HTTP, return it (prefer fast path)
	if o.title != "" || o.thumb != "" || dd.dur > 0 {
		return o.title, o.thumb, dd.dur, nil
	}

	// Fallback to yt-dlp --dump-json
	ytdlpCtx, cancel := context.WithTimeout(ctx, d.cfg.YtDLPTimeout)
	defer cancel()
	cmd := exec.CommandContext(ytdlpCtx, "yt-dlp", "--dump-json", "--no-playlist", videoURL)
	out, e := cmd.Output()
	if e != nil {
		return "", "", 0, e
	}
	title = extractJSONField(string(out), "title")
	thumbnail = extractJSONField(string(out), "thumbnail")
	durStr := extractJSONField(string(out), "duration")
	if durStr != "" {
		if strings.ContainsAny(durStr, ".") {
			durStr = strings.SplitN(durStr, ".", 2)[0]
		}
		if n, e2 := strconv.Atoi(durStr); e2 == nil {
			durationSeconds = n
		}
	}
	return title, thumbnail, durationSeconds, nil
}

func extractJSONField(js, field string) string {
	// very naive; expects "field": value,
	idx := strings.Index(js, "\""+field+"\"")
	if idx == -1 {
		return ""
	}
	s := js[idx:]
	colon := strings.Index(s, ":")
	if colon == -1 {
		return ""
	}
	v := strings.TrimSpace(s[colon+1:])
	if len(v) == 0 {
		return ""
	}
	// remove leading quotes
	if v[0] == '"' {
		v = v[1:]
		end := strings.IndexByte(v, '"')
		if end == -1 {
			return ""
		}
		return v[:end]
	}
	// numeric until comma
	end := strings.IndexByte(v, ',')
	if end == -1 {
		end = len(v)
	}
	return strings.TrimSpace(v[:end])
}

func (d *Downloader) fetchOEmbed(ctx context.Context, endpoint, videoURL string) (title string, thumbnail string, err error) {
	if endpoint == "" {
		return "", "", errors.New("oembed endpoint not configured")
	}
	u, e := url.Parse(endpoint)
	if e != nil {
		return "", "", e
	}
	q := u.Query()
	q.Set("url", videoURL)
	q.Set("format", "json")
	u.RawQuery = q.Encode()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if e != nil {
		return "", "", e
	}
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, e := client.Do(req)
	if e != nil {
		return "", "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", errors.New("oembed non-2xx")
	}
	var payload struct {
		Title        string `json:"title"`
		ThumbnailURL string `json:"thumbnail_url"`
	}
	dec := json.NewDecoder(resp.Body)
	if e := dec.Decode(&payload); e != nil {
		return "", "", e
	}
	return payload.Title, payload.ThumbnailURL, nil
}

func (d *Downloader) fetchDuration(ctx context.Context, endpoint, videoURL string) (durationSeconds int, err error) {
	if endpoint == "" {
		return 0, errors.New("duration endpoint not configured")
	}
	bodyMap := map[string]string{"url": videoURL}
	b, _ := json.Marshal(bodyMap)
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if e != nil {
		return 0, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, e := client.Do(req)
	if e != nil {
		return 0, e
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, errors.New("duration non-2xx")
	}
	// Try strict struct first
	var s struct {
		Duration int `json:"duration"`
	}
	if e := json.NewDecoder(resp.Body).Decode(&s); e == nil && s.Duration > 0 {
		return s.Duration, nil
	}
	// Fallback decode with map and parse
	// Re-read body is not possible; assume strict decode succeeded or body consumed.
	return 0, errors.New("duration parse failed")
}

func (d *Downloader) Download(ctx context.Context, url, outputPath string, onProgress ProgressFunc) error {
	return d.withPermit(func() error {
		ctx, cancel := context.WithTimeout(ctx, d.cfg.DownloadTimeout)
		defer cancel()
		// Strictly prefer audio-only formats; avoid falling back to video
		audioFmt := "bestaudio[ext=m4a]/bestaudio[ext=webm]/bestaudio"
		args := []string{"-f", audioFmt, "-o", outputPath, "--no-playlist", "--newline", url}
		cmd := exec.CommandContext(ctx, "yt-dlp", args...)
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		go readProgress(stderr, onProgress)
		go readProgress(stdout, onProgress)
		return cmd.Wait()
	})
}

func readProgress(r io.Reader, onProgress ProgressFunc) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if m := percentRe.FindStringSubmatch(line); len(m) == 2 {
			if p, err := strconv.Atoi(m[1]); err == nil {
				if p < 0 {
					p = 0
				}
				if p > 100 {
					p = 100
				}
				onProgress(p)
			}
		}
	}
}
