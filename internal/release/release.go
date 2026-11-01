// Package release checks the latest published Hugo Panel release.
package release

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultLatestURL is the GitHub API endpoint used to look up the newest
	// Hugo Panel release.
	DefaultLatestURL = "https://api.github.com/repos/Kaiyuan/hugo-panel/releases/latest"

	// PageURL is the human-readable releases page.
	PageURL = "https://github.com/Kaiyuan/hugo-panel/releases/latest"

	cacheTTL       = 6 * time.Hour
	failureTTL     = 15 * time.Minute
	requestTimeout = 5 * time.Second
)

var versionRe = regexp.MustCompile(`\bv?(\d+)\.(\d+)\.(\d+)`)

// Checker looks up the latest release and caches the answer so the API can be
// called on every page load without hitting GitHub each time.
type Checker struct {
	mu     sync.Mutex
	latest string
	ttl    time.Duration
	at     time.Time
}

// NewChecker returns a release checker with an empty cache.
func NewChecker() *Checker { return &Checker{} }

// Latest returns the newest published version, or an empty string when the
// check is unavailable. An empty value must be treated as "unknown".
func (c *Checker) Latest(ctx context.Context) string {
	c.mu.Lock()
	if !c.at.IsZero() && time.Since(c.at) < c.ttl {
		version := c.latest
		c.mu.Unlock()
		return version
	}
	c.mu.Unlock()

	version, ttl := fetchLatest(ctx)

	c.mu.Lock()
	c.latest = version
	c.ttl = ttl
	c.at = time.Now()
	c.mu.Unlock()
	return version
}

// latestURL reports the endpoint used for the version check. The
// PANEL_LATEST_URL environment variable can point it at a mirror or a test
// server; an empty value disables the automatic check.
func latestURL() string {
	if value, ok := os.LookupEnv("PANEL_LATEST_URL"); ok {
		return strings.TrimSpace(value)
	}
	return DefaultLatestURL
}

func fetchLatest(ctx context.Context) (string, time.Duration) {
	endpoint := latestURL()
	if endpoint == "" {
		return "", failureTTL
	}
	cctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", failureTTL
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "HugoPanel")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", failureTTL
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", failureTTL
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 512*1024))
	if err != nil {
		return "", failureTTL
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return "", failureTTL
	}
	version, ok := Normalize(body.TagName)
	if !ok {
		return "", failureTTL
	}
	return version, cacheTTL
}

// Normalize extracts "x.y.z" from a tag or version string such as "v0.0.5".
func Normalize(value string) (string, bool) {
	m := versionRe.FindStringSubmatch(value)
	if m == nil {
		return "", false
	}
	return m[1] + "." + m[2] + "." + m[3], true
}

// IsNewer reports whether latest is strictly newer than current. Unparsable
// versions are treated as unknown, not as newer.
func IsNewer(latest, current string) bool {
	l, ok := parse(latest)
	if !ok {
		return false
	}
	c, ok := parse(current)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parse(value string) ([3]int, bool) {
	normalized, ok := Normalize(value)
	if !ok {
		return [3]int{}, false
	}
	parts := strings.Split(normalized, ".")
	var out [3]int
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}
