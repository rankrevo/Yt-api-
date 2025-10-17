package util

import (
    "crypto/sha1"
    "encoding/hex"
    "net/url"
    "path"
    "strconv"
    "strings"
)

// CanonicalVideoID attempts to canonicalize YouTube URLs to a stable video id.
// For non-YouTube URLs, returns the original URL trimmed.
func CanonicalVideoID(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	u, err := url.Parse(s)
	if err != nil {
		return s
	}
	host := strings.ToLower(u.Host)
	if strings.Contains(host, "youtube.com") {
		q := u.Query()
		if v := q.Get("v"); v != "" {
			return "yt:" + v
		}
		// Shorts: /shorts/<id>
		if strings.HasPrefix(strings.ToLower(u.Path), "/shorts/") {
			return "yt:" + path.Base(u.Path)
		}
	}
	if strings.Contains(host, "youtu.be") {
		id := strings.Trim(path.Base(u.Path), "/")
		if id != "" {
			return "yt:" + id
		}
	}
	// Fallback to normalized scheme+host+path without query fragments
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func HashString(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// IsAllowedDomain returns true if the URL belongs to one of allowed domains.
func IsAllowedDomain(raw string, allowed []string) bool {
    s := strings.TrimSpace(raw)
    if s == "" {
        return false
    }
    u, err := url.Parse(s)
    if err != nil {
        return false
    }
    host := strings.ToLower(u.Host)
    for _, d := range allowed {
        d = strings.ToLower(strings.TrimSpace(d))
        if d == "" {
            continue
        }
        if strings.HasSuffix(host, d) || host == d {
            return true
        }
    }
    return false
}

// ParseClipBounds validates HH:MM:SS or MM:SS and returns seconds.
// Returns (start, end, ok). ok=false if invalid or exceeds maxSeconds.
func ParseClipBounds(start, end string, maxSeconds int, totalDuration int) (int, int, bool) {
    toSec := func(t string) (int, bool) {
        t = strings.TrimSpace(t)
        if t == "" {
            return 0, true
        }
        parts := strings.Split(t, ":")
        if len(parts) < 2 || len(parts) > 3 {
            return 0, false
        }
        var h, m, s int
        var err error
        if len(parts) == 3 {
            if h, err = strconv.Atoi(parts[0]); err != nil || h < 0 { return 0, false }
            if m, err = strconv.Atoi(parts[1]); err != nil || m < 0 || m > 59 { return 0, false }
            if s, err = strconv.Atoi(parts[2]); err != nil || s < 0 || s > 59 { return 0, false }
        } else {
            if m, err = strconv.Atoi(parts[0]); err != nil || m < 0 { return 0, false }
            if s, err = strconv.Atoi(parts[1]); err != nil || s < 0 || s > 59 { return 0, false }
        }
        return h*3600 + m*60 + s, true
    }
    ss, ok1 := toSec(start)
    ee, ok2 := toSec(end)
    if !ok1 || !ok2 { return 0, 0, false }
    if ee > 0 && ee <= ss { return 0, 0, false }
    if totalDuration > 0 {
        if ss >= totalDuration { return 0, 0, false }
        if ee > 0 && ee > totalDuration { return 0, 0, false }
    }
    clipLen := 0
    if ee > 0 { clipLen = ee - ss } else if totalDuration > 0 { clipLen = totalDuration - ss }
    if maxSeconds > 0 && clipLen > maxSeconds { return 0, 0, false }
    return ss, ee, true
}
