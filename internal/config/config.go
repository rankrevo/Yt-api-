package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration parsed from environment variables.
//
// For each field below, the corresponding environment variable is indicated
// in parentheses with its default. Values are read once at startup.
type Config struct {
    // WorkerPoolSize controls the number of goroutines in each worker pool
    // for download and conversion. Higher values increase concurrency at the
    // cost of CPU/IO. (WORKER_POOL_SIZE, default 20)
    WorkerPoolSize int

    // JobQueueCapacity is the maximum number of pending jobs allowed in each
    // in-memory priority queue. When full, new requests get HTTP 503. (JOB_QUEUE_CAPACITY, default 1000)
    JobQueueCapacity int

    // MaxJobRetries is the maximum automatic retry attempts per job with
    // exponential backoff before the job is marked failed. (MAX_JOB_RETRIES, default 3)
    MaxJobRetries int

    // RequestsPerSecond and BurstSize define a global token bucket limiter
    // across all requests. (REQUESTS_PER_SECOND default 100, BURST_SIZE default 200)
    RequestsPerSecond float64
    BurstSize         int

    // PerIPRPS and PerIPBurst limit the rate per client IP address.
    // (PER_IP_RPS default 10, PER_IP_BURST default 20)
    PerIPRPS   float64
    PerIPBurst int

    // Redis connection settings for the optional Redis-backed session store.
    // If RedisAddr is non-empty and reachable, Redis will be used. (REDIS_ADDR, REDIS_PASSWORD, REDIS_DB)
    RedisAddr     string
    RedisPassword string
    RedisDB       int

    // YtDLPTimeout caps metadata fallback execution time. FFmpegMin/MaxTimeout
    // bound conversion timeouts. (YTDLP_TIMEOUT default 90s; FFMPEG_MIN_TIMEOUT default 15m; FFMPEG_MAX_TIMEOUT default 60m)
    YtDLPTimeout     time.Duration
    FFmpegMinTimeout time.Duration
    FFmpegMaxTimeout time.Duration

    // FFmpegMode selects constant bitrate (CBR) or variable bitrate (VBR) encoding.
    // FFmpegCBRBitrate sets the bitrate like "192k" when in CBR; FFmpegVBRQ sets
    // the VBR quality (lower is higher quality for LAME). FFmpegThreads sets
    // the thread count (0 lets ffmpeg decide). (FFMPEG_MODE, FFMPEG_CBR_BITRATE, FFMPEG_VBR_Q, FFMPEG_THREADS)
    FFmpegMode       string
    FFmpegCBRBitrate string
    FFmpegVBRQ       int
    FFmpegThreads    int

    // AlwaysDownload forces a fresh download even if a cached asset exists.
    // DownloadThreshold can be used by future logic to decide re-download
    // after a certain age. YtDLPDownloadConcurrency is reserved for future
    // parallel segment download strategies. YtDLPDownloadTimeout limits the
    // end-to-end download time. (ALWAYS_DOWNLOAD, DOWNLOAD_THRESHOLD, YTDLP_DOWNLOAD_CONCURRENCY, YTDLP_DOWNLOAD_TIMEOUT)
    AlwaysDownload           bool
    DownloadThreshold        time.Duration
    YtDLPDownloadConcurrency int
    YtDLPDownloadTimeout     time.Duration

    // ConversionsDir is the root directory for temporary streams/ and output
    // files. File TTLs control cleanup of old artifacts. (CONVERSIONS_DIR,
    // UNCONVERTED_FILE_TTL, CONVERTED_FILE_TTL)
    ConversionsDir     string
    UnconvertedFileTTL time.Duration
    ConvertedFileTTL   time.Duration

    // API-key and CORS controls. If RequireAPIKey is true, only requests with
    // X-API-Key matching APIKeys are allowed. AllowedOrigins feeds CORS. Admin
    // credentials are reserved for future admin endpoints. (REQUIRE_API_KEY,
    // API_KEYS, ALLOWED_ORIGINS, ADMIN_USER, ADMIN_PASS)
    RequireAPIKey  bool
    APIKeys        []string
    AllowedOrigins []string
    AdminUser      string
    AdminPass      string

    // External HTTP endpoints used for fast metadata fetch. (OEMBED_ENDPOINT,
    // DURATION_API_ENDPOINT)
    OEmbedEndpoint      string
    DurationAPIEndpoint string

    // MaxConcurrentDownloads and MaxConcurrentConversions bound the permits in
    // the downloader and converter semaphores. (MAX_CONCURRENT_DOWNLOADS,
    // MAX_CONCURRENT_CONVERSIONS)
    MaxConcurrentDownloads   int
    MaxConcurrentConversions int

    // AllowedDomains restricts which hostnames are accepted in incoming URLs
    // (e.g., "youtube.com,youtu.be"). (ALLOWED_DOMAINS)
    AllowedDomains []string

    // MaxClipSeconds caps the clip duration either (end-start) or (total-start)
    // if end is empty. Requests exceeding this limit are rejected. (MAX_CLIP_SECONDS, default 900)
    MaxClipSeconds int

    // MaxVideoDurationSeconds caps the total video duration. Videos longer than this are rejected. (MAX_VIDEO_DURATION_SECONDS, default 2400 = 40 minutes)
    MaxVideoDurationSeconds int

    // IPAllowlist restricts API access to specific client IPs when configured.
    // Leave empty to allow all. (IP_ALLOWLIST)
    IPAllowlist []string

    // ShedQueueThreshold sheds traffic (readiness returns 503) when combined
    // queued jobs exceed this number. 0 disables shedding. (SHED_QUEUE_THRESHOLD)
    ShedQueueThreshold int
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}

func getEnvFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

func getEnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func Load() *Config {
	cfg := &Config{
		WorkerPoolSize:   getEnvInt("WORKER_POOL_SIZE", 20),
		JobQueueCapacity: getEnvInt("JOB_QUEUE_CAPACITY", 1000),
		MaxJobRetries:    getEnvInt("MAX_JOB_RETRIES", 3),

		RequestsPerSecond: getEnvFloat("REQUESTS_PER_SECOND", 100),
		BurstSize:         getEnvInt("BURST_SIZE", 200),
		PerIPRPS:          getEnvFloat("PER_IP_RPS", 10),
		PerIPBurst:        getEnvInt("PER_IP_BURST", 20),

		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),

		YtDLPTimeout:     getEnvDuration("YTDLP_TIMEOUT", 90*time.Second),
		FFmpegMinTimeout: getEnvDuration("FFMPEG_MIN_TIMEOUT", 15*time.Minute),
		FFmpegMaxTimeout: getEnvDuration("FFMPEG_MAX_TIMEOUT", 60*time.Minute),
		FFmpegMode:       strings.ToUpper(getEnv("FFMPEG_MODE", "CBR")),
		FFmpegCBRBitrate: getEnv("FFMPEG_CBR_BITRATE", "192k"),
		FFmpegVBRQ:       getEnvInt("FFMPEG_VBR_Q", 5),
		FFmpegThreads:    getEnvInt("FFMPEG_THREADS", 0),

		AlwaysDownload:           getEnvBool("ALWAYS_DOWNLOAD", false),
		DownloadThreshold:        getEnvDuration("DOWNLOAD_THRESHOLD", 10*time.Minute),
		YtDLPDownloadConcurrency: getEnvInt("YTDLP_DOWNLOAD_CONCURRENCY", 8),
		YtDLPDownloadTimeout:     getEnvDuration("YTDLP_DOWNLOAD_TIMEOUT", 30*time.Minute),

		ConversionsDir:     getEnv("CONVERSIONS_DIR", "/tmp/conversions"),
		UnconvertedFileTTL: getEnvDuration("UNCONVERTED_FILE_TTL", 5*time.Minute),
		ConvertedFileTTL:   getEnvDuration("CONVERTED_FILE_TTL", 10*time.Minute),

		RequireAPIKey:  getEnvBool("REQUIRE_API_KEY", false),
		APIKeys:        splitAndTrim(getEnv("API_KEYS", "")),
		AllowedOrigins: splitAndTrim(getEnv("ALLOWED_ORIGINS", "*")),
		AdminUser:      getEnv("ADMIN_USER", "admin"),
		AdminPass:      getEnv("ADMIN_PASS", "password"),

		OEmbedEndpoint:      getEnv("OEMBED_ENDPOINT", "https://www.youtube.com/oembed"),
		DurationAPIEndpoint: getEnv("DURATION_API_ENDPOINT", "https://ds2.ezsrv.net/api/getDuration"),

        MaxConcurrentDownloads:   getEnvInt("MAX_CONCURRENT_DOWNLOADS", 20),
        MaxConcurrentConversions: getEnvInt("MAX_CONCURRENT_CONVERSIONS", 20),

        // Validation and security
        AllowedDomains:    splitAndTrim(getEnv("ALLOWED_DOMAINS", "youtube.com,youtu.be")),
        MaxClipSeconds:    getEnvInt("MAX_CLIP_SECONDS", 15*60),
        MaxVideoDurationSeconds: getEnvInt("MAX_VIDEO_DURATION_SECONDS", 40*60), // 40 minutes
        IPAllowlist:       splitAndTrim(getEnv("IP_ALLOWLIST", "")),
        ShedQueueThreshold: getEnvInt("SHED_QUEUE_THRESHOLD", 0),
	}
	return cfg
}

func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		pt := strings.TrimSpace(p)
		if pt != "" {
			res = append(res, pt)
		}
	}
	return res
}
