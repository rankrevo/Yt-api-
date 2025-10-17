package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/rs/cors"

	"ytmp3api/internal/config"
	"ytmp3api/internal/converter"
	"ytmp3api/internal/downloader"
	"ytmp3api/internal/metrics"
	"ytmp3api/internal/middleware"
	"ytmp3api/internal/models"
	"ytmp3api/internal/queue"
	"ytmp3api/internal/store"
	"ytmp3api/internal/util"
    "os/exec"
)

type API struct {
	cfg      *config.Config
	sessions store.SessionStore
	dl       *downloader.Downloader
	conv     *converter.Converter
	dlQueue  *queue.Queue
	cvQueue  *queue.Queue
	metrics  *metrics.Registry
}

func NewAPI(cfg *config.Config) (*API, error) {
	var sess store.SessionStore
	if cfg.RedisAddr != "" {
		rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
		if err := rdb.Ping(context.Background()).Err(); err == nil {
			sess = store.NewRedisStore(rdb)
		}
	}
	if sess == nil {
		sess = store.NewMemoryStore()
	}

	_ = os.MkdirAll(cfg.ConversionsDir, 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.ConversionsDir, "streams"), 0o755)
	_ = os.MkdirAll(filepath.Join(cfg.ConversionsDir, "outputs"), 0o755)

	dl := downloader.New(downloader.Config{
		YtDLPTimeout:        cfg.YtDLPTimeout,
		DownloadTimeout:     cfg.YtDLPDownloadTimeout,
		OEmbedEndpoint:      cfg.OEmbedEndpoint,
		DurationAPIEndpoint: cfg.DurationAPIEndpoint,
	}, cfg.MaxConcurrentDownloads)
	cv := converter.New(converter.Config{MinTimeout: cfg.FFmpegMinTimeout, MaxTimeout: cfg.FFmpegMaxTimeout, Mode: converter.Mode(strings.ToUpper(cfg.FFmpegMode)), CBRBitrate: cfg.FFmpegCBRBitrate, VBRQ: cfg.FFmpegVBRQ, Threads: cfg.FFmpegThreads}, cfg.MaxConcurrentConversions)

	dlQ := queue.NewQueue(cfg.JobQueueCapacity)
	cvQ := queue.NewQueue(cfg.JobQueueCapacity)

	m := metrics.NewRegistry()
	m.Workers.Store(int64(cfg.WorkerPoolSize))
	m.QueueCapacity.Store(int64(cfg.JobQueueCapacity))
	m.RateLimit.Store(int64(cfg.BurstSize))

	api := &API{cfg: cfg, sessions: sess, dl: dl, conv: cv, dlQueue: dlQ, cvQueue: cvQ, metrics: m}
	api.startWorkers()
	api.startCleanup()
	return api, nil
}

func (a *API) startWorkers() {
	dlPool := queue.NewWorkerPool(a.cfg.WorkerPoolSize, a.dlQueue, a.handleDownload)
	dlPool.Start()
	cvPool := queue.NewWorkerPool(a.cfg.WorkerPoolSize, a.cvQueue, a.handleConvert)
	cvPool.Start()
}

func (a *API) startCleanup() {
	go func() {
		ticker := time.NewTicker(time.Minute)
		for range ticker.C {
			now := time.Now()
			// Clean outputs (converted files)
			outDir := filepath.Join(a.cfg.ConversionsDir, "outputs")
			if outs, _ := os.ReadDir(outDir); outs != nil {
				for _, e := range outs {
					if e.IsDir() {
						continue
					}
					p := filepath.Join(outDir, e.Name())
					info, err := os.Stat(p)
					if err != nil {
						continue
					}
					if now.Sub(info.ModTime()) > a.cfg.ConvertedFileTTL {
						_ = os.Remove(p)
					}
				}
			}
			// Clean streams (unconverted source files)
			stDir := filepath.Join(a.cfg.ConversionsDir, "streams")
			if sts, _ := os.ReadDir(stDir); sts != nil {
				for _, e := range sts {
					if e.IsDir() {
						continue
					}
					p := filepath.Join(stDir, e.Name())
					info, err := os.Stat(p)
					if err != nil {
						continue
					}
					if now.Sub(info.ModTime()) > a.cfg.UnconvertedFileTTL {
						_ = os.Remove(p)
					}
				}
			}
		}
	}()
}

func (a *API) Router() http.Handler {
	r := chi.NewRouter()
	// CORS and security headers
	corsMw := cors.New(cors.Options{AllowedOrigins: a.cfg.AllowedOrigins, AllowedMethods: []string{"GET", "POST", "DELETE", "OPTIONS"}, AllowedHeaders: []string{"*"}, ExposedHeaders: []string{"Content-Length", "Content-Range"}, AllowCredentials: false})
	r.Use(corsMw.Handler)
	r.Use(middleware.SecurityHeaders)
    // Optional IP allowlist
    r.Use(middleware.IPAllowlistMiddleware(a.cfg.IPAllowlist))
	// Rate limiting
	r.Use(middleware.GlobalRateLimiter(a.cfg.RequestsPerSecond, a.cfg.BurstSize))
	r.Use(middleware.PerIPRateLimiter(a.cfg.PerIPRPS, a.cfg.PerIPBurst))
	// API key middleware
	keys := map[string]struct{}{}
	for _, k := range a.cfg.APIKeys {
		keys[k] = struct{}{}
	}
	r.Use(middleware.APIKey(a.cfg.RequireAPIKey, keys))

	r.Post("/prepare", a.handlePrepare)
	r.Post("/convert", a.handleConvertReq)
	r.Get("/status/{id}", a.handleStatus)
	r.Get("/download/{id}.mp3", a.handleDownloadFile)
	r.Delete("/delete/{id}", a.handleDelete)

    r.Get("/health", a.handleHealth)
    r.Get("/ready", a.handleReady)
	r.Get("/metrics", a.handleMetricsJSON)
	r.Get("/stats", a.handleStats)
	// prometheus metrics at /metrics/prom if client_golang is added later

    // Simple docs and admin placeholders
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, docsHTML)
	})
	r.Get("/admin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, adminHTML)
	})

    // Tool self-test endpoint
    r.Get("/selftest", a.handleSelfTest)

	return r
}

func (a *API) handlePrepare(w http.ResponseWriter, r *http.Request) {
	var req models.PrepareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
    // Validation: allowed domains
    if !util.IsAllowedDomain(req.URL, a.cfg.AllowedDomains) {
        writeErr(w, http.StatusBadRequest, "unsupported url domain")
        return
    }
	// Always create a new session; dedupe at asset/variant layer instead of reusing sessions
	id := newID()
	s := &models.ConversionSession{ID: id, URL: req.URL, State: models.StatePreparing}
	if err := a.sessions.CreateSession(r.Context(), s); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	_ = a.sessions.SetURLMap(r.Context(), req.URL, id)

	// fetch metadata fast using yt-dlp --dump-json (fallback design)
	title, thumb, dur, _ := a.dl.FetchMetadata(r.Context(), req.URL)
	s.Meta = models.MetaLite{Title: title, Thumbnail: thumb, Duration: dur}
	s.State = models.StateCreated
	_ = a.sessions.UpdateSession(r.Context(), s)

	// enqueue background download
	assetHash := util.HashString(util.CanonicalVideoID(req.URL))
	s.AssetHash = assetHash
	_ = a.sessions.UpdateSession(r.Context(), s)
	if _, state, ok, _ := a.sessions.GetAsset(r.Context(), assetHash); !ok || state == "" || state == string(models.StateFailed) {
		_ = a.sessions.SetAsset(r.Context(), assetHash, "", string(models.StatePreparing))
		job := queue.Job{ID: newID(), Type: queue.JobDownload, SessionID: id, EnqueuedAt: time.Now(), Priority: 10}
		if !a.dlQueue.Enqueue(job) {
			writeErr(w, http.StatusServiceUnavailable, "queue full")
			return
		}
	}
	resp := models.PrepareResponse{ConversionID: id, Status: string(s.State), Metadata: s.Meta, Message: "Metadata fetched successfully. Stream is downloading in background."}
	writeJSON(w, http.StatusAccepted, resp)
}

func (a *API) handleConvertReq(w http.ResponseWriter, r *http.Request) {
	var req models.ConvertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConversionID == "" {
		writeErr(w, http.StatusBadRequest, "invalid request")
		return
	}
	s, err := a.sessions.GetSession(r.Context(), req.ConversionID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "session not found")
		return
	}
    // Validation: sanity check start/end and max clip length
    // Try using known video duration from metadata if present
    total := s.Meta.Duration
    if total < 0 { total = 0 }
    if _, _, ok := util.ParseClipBounds(req.StartTime, req.EndTime, a.cfg.MaxClipSeconds, total); !ok {
        writeErr(w, http.StatusBadRequest, "invalid start/end or clip too long")
        return
    }
	// Always accept and enqueue conversion asynchronously. If source not ready,
	// workers will re-enqueue after a short delay until download completes.
	// Variant hash (url + quality + range)
	s.AssetHash = util.HashString(util.CanonicalVideoID(s.URL))
	s.VariantHash = util.HashString(s.AssetHash + "|" + string(req.Quality) + "|" + req.StartTime + "|" + req.EndTime)
	_ = a.sessions.UpdateSession(r.Context(), s)
	// Fast-complete if variant already exists
	if out, ok, _ := a.sessions.GetVariant(r.Context(), s.VariantHash); ok && out != "" {
		s.OutputPath = out
		s.State = models.StateCompleted
		s.DownloadProgress = 100
		s.ConversionProgress = 100
		_ = a.sessions.UpdateSession(r.Context(), s)
		writeJSON(w, http.StatusAccepted, models.ConvertAcceptedResponse{ConversionID: s.ID, Status: string(s.State), QueuePosition: 0, Message: "Reused existing converted output."})
		return
	}
    // Determine if source is already ready to avoid unnecessary 'queued' bounce
    sourceReady := false
    if s.SourcePath != "" {
        sourceReady = true
    } else {
        if src, state, ok, _ := a.sessions.GetAsset(r.Context(), s.AssetHash); ok && src != "" && state == string(models.StateDownloaded) {
            s.SourcePath = src
            sourceReady = true
        }
    }

    // Map API key to job priority (simple heuristic: premium > default)
	apiKey := r.Header.Get("X-API-Key")
	priority := 5
	lk := strings.ToLower(apiKey)
	if strings.HasPrefix(lk, "premium") || strings.HasPrefix(lk, "pro") || strings.HasPrefix(lk, "vip") {
		priority = 50
	}
	job := queue.Job{ID: newID(), Type: queue.JobConvert, SessionID: s.ID, Quality: string(req.Quality), StartTime: req.StartTime, EndTime: req.EndTime, EnqueuedAt: time.Now(), Priority: priority, ApiKey: apiKey}
	if !a.cvQueue.Enqueue(job) {
		writeErr(w, http.StatusServiceUnavailable, "queue full")
		return
	}
    // If the source is ready, reflect a more immediate state; otherwise mark queued
    if sourceReady {
        s.State = models.StateConverting
    } else {
        s.State = models.StateQueued
    }
    _ = a.sessions.UpdateSession(r.Context(), s)
	// Report position in the convert queue and current download state
	position := a.cvQueue.PositionForSession(queue.JobConvert, s.ID)
    msg := "Conversion request accepted."
    if sourceReady {
        msg += " Starting conversion shortly."
    } else {
        msg += " Waiting for download to finish."
    }
    // Report more accurate status in response to reduce UI flicker
    respStatus := string(s.State)
    writeJSON(w, http.StatusAccepted, models.ConvertAcceptedResponse{
		ConversionID:  s.ID,
        Status:        respStatus,
		QueuePosition: position,
		Message:       msg,
	})
}

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := a.sessions.GetSession(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	downloadURL := ""
	if s.State == models.StateCompleted && s.OutputPath != "" {
		// Prefer stable session-based download URL
		downloadURL = "/download/" + s.ID + ".mp3"
	}
	resp := models.StatusResponse{ConversionID: s.ID, Status: string(s.State), DownloadProgress: s.DownloadProgress, ConversionProgress: s.ConversionProgress, DownloadURL: downloadURL}
	if s.State == models.StateQueued {
		resp.QueuePosition = a.cvQueue.PositionForSession(queue.JobConvert, s.ID)
	}
	if s.Error != "" {
		resp.Error = s.Error
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, _ := a.sessions.GetSession(r.Context(), id)
	_ = a.sessions.DeleteSession(r.Context(), id)
	if s != nil {
		if s.OutputPath != "" {
			_ = os.Remove(s.OutputPath)
		}
		if s.SourcePath != "" {
			_ = os.Remove(s.SourcePath)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "message": "Conversion data removed successfully."})
}

func (a *API) handleDownload(job queue.Job) {
	ctx := context.Background()
	s, err := a.sessions.GetSession(ctx, job.SessionID)
	if err != nil {
		return
	}
	s.State = models.StateDownloading
	_ = a.sessions.UpdateSession(ctx, s)
    start := time.Now()
	// store by asset hash under streams/ so future sessions reuse it
	if s.AssetHash == "" {
		s.AssetHash = util.HashString(util.CanonicalVideoID(s.URL))
	}
	out := filepath.Join(a.cfg.ConversionsDir, "streams", s.AssetHash+".source")
	err = a.dl.Download(ctx, s.URL, out, func(p int) {
		s.DownloadProgress = p
		_ = a.sessions.UpdateSession(ctx, s)
	})
    if err != nil {
        job.Attempts++
        if job.Attempts < a.cfg.MaxJobRetries {
            backoff := time.Duration(1<<job.Attempts) * time.Second
            if backoff > 60*time.Second { backoff = 60 * time.Second }
            go func(j queue.Job) {
                time.Sleep(backoff)
                a.dlQueue.Enqueue(j)
            }(job)
        } else {
            s.State = models.StateFailed
            s.Error = err.Error()
            _ = a.sessions.UpdateSession(ctx, s)
            _ = a.sessions.SetAsset(ctx, s.AssetHash, "", string(models.StateFailed))
            a.metrics.ErrorCount.Add(1)
        }
        return
    }
    a.metrics.SuccessCount.Add(1)
    a.metrics.ObserveDuration(time.Since(start).Seconds(), false)
	s.SourcePath = out
	s.State = models.StateDownloaded
	s.DownloadProgress = 100
	_ = a.sessions.UpdateSession(ctx, s)
	_ = a.sessions.SetAsset(ctx, s.AssetHash, out, string(models.StateDownloaded))
}

func (a *API) handleConvert(job queue.Job) {
	ctx := context.Background()
	s, err := a.sessions.GetSession(ctx, job.SessionID)
	if err != nil {
		return
	}
    start := time.Now()
    // Attempt to hydrate missing SourcePath from the shared asset cache.
    // This allows new sessions for the same URL to convert immediately
    // without waiting for a redundant download or re-enqueue loops.
    if s.AssetHash == "" {
        s.AssetHash = util.HashString(util.CanonicalVideoID(s.URL))
    }
    if s.SourcePath == "" {
        if src, state, ok, _ := a.sessions.GetAsset(ctx, s.AssetHash); ok && src != "" && state == string(models.StateDownloaded) {
            s.SourcePath = src
            s.State = models.StateDownloaded
            s.DownloadProgress = 100
            _ = a.sessions.UpdateSession(ctx, s)
        }
    }
	// Wait until download finishes; if not ready, re-enqueue shortly
	if s.SourcePath == "" || s.State == models.StateDownloading || s.State == models.StatePreparing || s.State == models.StateCreated {
		go func(j queue.Job) {
			// Re-enqueue without mutating the session to avoid overwriting newer fields
			time.Sleep(5 * time.Second)
			a.cvQueue.Enqueue(j)
		}(job)
		return
	}
	s.State = models.StateConverting
	_ = a.sessions.UpdateSession(ctx, s)
	if s.AssetHash == "" {
		s.AssetHash = util.HashString(util.CanonicalVideoID(s.URL))
	}
	if s.VariantHash == "" {
		s.VariantHash = util.HashString(s.AssetHash + "|" + job.Quality + "|" + job.StartTime + "|" + job.EndTime)
	}
	out := filepath.Join(a.cfg.ConversionsDir, "outputs", s.VariantHash+".mp3")
	dur := s.Meta.Duration
    err = a.conv.Convert(ctx, s.SourcePath, out, job.Quality, job.StartTime, job.EndTime, dur, func(p int) {
		s.ConversionProgress = p
		_ = a.sessions.UpdateSession(ctx, s)
	})
	if err != nil {
        job.Attempts++
        if job.Attempts < a.cfg.MaxJobRetries {
            // Exponential backoff: 2^attempt seconds up to 60s
            backoff := time.Duration(1<<job.Attempts) * time.Second
            if backoff > 60*time.Second { backoff = 60 * time.Second }
            go func(j queue.Job) {
                time.Sleep(backoff)
                a.cvQueue.Enqueue(j)
            }(job)
        } else {
            s.State = models.StateFailed
            s.Error = err.Error()
            _ = a.sessions.UpdateSession(ctx, s)
            a.metrics.ErrorCount.Add(1)
        }
        return
	}
    a.metrics.SuccessCount.Add(1)
    a.metrics.ObserveDuration(time.Since(start).Seconds(), true)
	s.OutputPath = out
	s.ConversionProgress = 100
	s.State = models.StateCompleted
	_ = a.sessions.UpdateSession(ctx, s)
	_ = a.sessions.SetVariant(ctx, s.VariantHash, out)
}

func (a *API) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, err := a.sessions.GetSession(r.Context(), id)
	if err != nil || s.OutputPath == "" {
		writeErr(w, http.StatusNotFound, "file not ready")
		return
	}
	f, err := os.Open(s.OutputPath)
	if err != nil {
		writeErr(w, http.StatusNotFound, "missing")
		return
	}
	defer f.Close()
	fi, _ := f.Stat()
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+safeFilename(s.Meta.Title)+".mp3\"")
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"status":         "healthy",
		"active_jobs":    a.metrics.ActiveJobs.Load(),
		"queued_jobs":    a.metrics.QueuedJobs.Load(),
		"completed_jobs": a.metrics.CompletedJobs.Load(),
		"failed_jobs":    a.metrics.FailedJobs.Load(),
		"workers":        a.metrics.Workers.Load(),
		"uptime":         time.Since(a.metrics.UptimeStart).String(),
		"memory_usage":   "",
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleReady(w http.ResponseWriter, r *http.Request) {
    // Consider ready if queues below capacity and tools installed
    // Cheap checks; deeper checks available via /selftest
    if a.cfg.ShedQueueThreshold > 0 {
        totalQ := a.dlQueue.Len() + a.cvQueue.Len()
        if totalQ > a.cfg.ShedQueueThreshold {
            writeErr(w, http.StatusServiceUnavailable, "shedding: too many queued jobs")
            return
        }
    }
    writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (a *API) handleMetricsJSON(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"active_jobs":      a.metrics.ActiveJobs.Load(),
		"queued_jobs":      a.metrics.QueuedJobs.Load(),
		"completed_jobs":   a.metrics.CompletedJobs.Load(),
		"failed_jobs":      a.metrics.FailedJobs.Load(),
		"workers":          a.metrics.Workers.Load(),
		"queue_capacity":   a.cfg.JobQueueCapacity,
		"rate_limit":       a.cfg.RequestsPerSecond,
		"uptime_seconds":   a.metrics.UptimeSeconds(),
		"success_rate":     a.metrics.SuccessRate(),
		"avg_processing_s": 0.0,
		"sessions_active":  a.metrics.SessionsActive.Load(),
        "convert_latency_buckets": a.metrics.ConvertLatencyBuckets,
        "download_latency_buckets": a.metrics.DownloadLatencyBuckets,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"queue_download_len": a.dlQueue.Len(),
		"queue_convert_len":  a.cvQueue.Len(),
	})
}

func (a *API) handleSelfTest(w http.ResponseWriter, r *http.Request) {
    // Check presence of external tools
    type toolInfo struct{ Name, Version, Error string }
    tools := []toolInfo{}
    // ffmpeg -version
    if out, err := exec.Command("ffmpeg", "-version").Output(); err == nil {
        lines := strings.SplitN(string(out), "\n", 2)
        tools = append(tools, toolInfo{Name: "ffmpeg", Version: strings.TrimSpace(lines[0])})
    } else {
        tools = append(tools, toolInfo{Name: "ffmpeg", Error: err.Error()})
    }
    if out, err := exec.Command("yt-dlp", "--version").Output(); err == nil {
        v := strings.TrimSpace(string(out))
        tools = append(tools, toolInfo{Name: "yt-dlp", Version: v})
    } else {
        tools = append(tools, toolInfo{Name: "yt-dlp", Error: err.Error()})
    }
    writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func safeFilename(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	if s == "" {
		return "download"
	}
	return s
}

func newID() string {
	return fmt.Sprintf("conv_%d_%d", time.Now().Unix(), rand.Int63())
}
