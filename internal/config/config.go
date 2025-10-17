package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration parsed from environment variables.
type Config struct {
	WorkerPoolSize   int
	JobQueueCapacity int
	MaxJobRetries    int

	RequestsPerSecond float64
	BurstSize         int
	PerIPRPS          float64
	PerIPBurst        int

	RedisAddr     string
	RedisPassword string
	RedisDB       int

	YtDLPTimeout     time.Duration
	FFmpegMinTimeout time.Duration
	FFmpegMaxTimeout time.Duration
	FFmpegMode       string // CBR or VBR
	FFmpegCBRBitrate string // e.g. 192k
	FFmpegVBRQ       int
	FFmpegThreads    int

	AlwaysDownload           bool
	DownloadThreshold        time.Duration
	YtDLPDownloadConcurrency int
	YtDLPDownloadTimeout     time.Duration

	ConversionsDir     string
	UnconvertedFileTTL time.Duration
	ConvertedFileTTL   time.Duration

	RequireAPIKey  bool
	APIKeys        []string
	AllowedOrigins []string
	AdminUser      string
	AdminPass      string

	OEmbedEndpoint      string
	DurationAPIEndpoint string

	MaxConcurrentDownloads   int
	MaxConcurrentConversions int
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
