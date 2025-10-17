package util

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"path"
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
