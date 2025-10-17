package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

type ipLimiter struct {
	rate    float64
	burst   int
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

type tokenBucket struct {
	capacity int
	tokens   float64
	rate     float64 // tokens per second
	last     time.Time
}

func newIPLimiter(rate float64, burst int) *ipLimiter {
	return &ipLimiter{rate: rate, burst: burst, buckets: make(map[string]*tokenBucket)}
}

func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[ip]
	if !ok {
		b = &tokenBucket{capacity: l.burst, tokens: float64(l.burst), rate: l.rate, last: time.Now()}
		l.buckets[ip] = b
	}
	now := time.Now()
	delta := now.Sub(b.last).Seconds()
	b.tokens += delta * b.rate
	if b.tokens > float64(b.capacity) {
		b.tokens = float64(b.capacity)
	}
	b.last = now
	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

func GlobalRateLimiter(rps float64, burst int) func(http.Handler) http.Handler {
	bucket := &tokenBucket{capacity: burst, tokens: float64(burst), rate: rps, last: time.Now()}
	var mu sync.Mutex
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			now := time.Now()
			delta := now.Sub(bucket.last).Seconds()
			bucket.tokens += delta * bucket.rate
			if bucket.tokens > float64(bucket.capacity) {
				bucket.tokens = float64(bucket.capacity)
			}
			bucket.last = now
			allowed := bucket.tokens >= 1
			if allowed {
				bucket.tokens -= 1
			}
			mu.Unlock()
			if !allowed {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("rate limit exceeded"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func PerIPRateLimiter(rps float64, burst int) func(http.Handler) http.Handler {
	lim := newIPLimiter(rps, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			if idx := strings.LastIndex(ip, ":"); idx != -1 {
				ip = ip[:idx]
			}
			if !lim.allow(ip) {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("per-ip rate limit exceeded"))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func APIKey(required bool, keys map[string]struct{}) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !required {
				next.ServeHTTP(w, r)
				return
			}
			k := r.Header.Get("X-API-Key")
			if _, ok := keys[k]; !ok {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("invalid api key"))
				return
			}
			// Optionally, priority per API key via header (used by queues if implemented)
			// r.Header.Set("X-API-Priority", keyPriority(k))
			next.ServeHTTP(w, r)
		})
	}
}

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		next.ServeHTTP(w, r)
	})
}

// IPAllowlistMiddleware blocks requests not in the allowlist when the list is non-empty.
func IPAllowlistMiddleware(allow []string) func(http.Handler) http.Handler {
    // Normalize allowlist
    allowed := map[string]struct{}{}
    for _, ip := range allow {
        ip = strings.TrimSpace(ip)
        if ip != "" {
            allowed[ip] = struct{}{}
        }
    }
    return func(next http.Handler) http.Handler {
        // If no allowlist configured, pass-through
        if len(allowed) == 0 {
            return next
        }
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ip := r.RemoteAddr
            if idx := strings.LastIndex(ip, ":"); idx != -1 {
                ip = ip[:idx]
            }
            if _, ok := allowed[ip]; !ok {
                w.WriteHeader(http.StatusForbidden)
                _, _ = w.Write([]byte("ip not allowed"))
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
