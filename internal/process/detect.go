package process

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var urlPattern = regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\]|[a-zA-Z0-9.-]+):\d{2,5}(?:/[^\s\]\)>\"']*)?`)

// DetectURL finds a local http(s) listen URL in process output.
// Prefers localhost/127.0.0.1; rewrites 0.0.0.0 / :: to 127.0.0.1 for browsing.
func DetectURL(output string) string {
	matches := urlPattern.FindAllString(output, -1)
	if len(matches) == 0 {
		return ""
	}

	var best string
	bestScore := -1
	for _, raw := range matches {
		cleaned := strings.TrimRight(raw, ".,;:")
		u, err := url.Parse(cleaned)
		if err != nil || u.Port() == "" {
			continue
		}
		score := urlScore(u.Hostname())
		if score > bestScore {
			bestScore = score
			best = normalizeBrowseURL(u)
		}
	}
	return best
}

func urlScore(host string) int {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return 3
	case "0.0.0.0", "::":
		return 1
	default:
		return 2 // LAN hostname / IP (vite Network)
	}
}

func normalizeBrowseURL(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	switch host {
	case "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	case "::1", "[::1]":
		host = "127.0.0.1"
	}
	path := u.EscapedPath()
	if path == "/" {
		path = ""
	}
	return u.Scheme + "://" + host + ":" + port + path
}

// PortFromURL returns the port number from a URL, or 0.
func PortFromURL(raw string) int {
	u, err := url.Parse(raw)
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		return 0
	}
	return p
}
