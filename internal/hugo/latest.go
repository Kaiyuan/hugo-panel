package hugo

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// DefaultLatestReleaseURL is the GitHub API endpoint used to look up the
	// newest Hugo release.
	DefaultLatestReleaseURL = "https://api.github.com/repos/gohugoio/hugo/releases/latest"

	// HugoInstallURL points users at the official upgrade instructions.
	HugoInstallURL = "https://gohugo.io/installation/"

	latestCacheTTL   = 6 * time.Hour
	latestFailureTTL = 15 * time.Minute
	latestTimeout    = 5 * time.Second
)

// latestReleaseURL reports the endpoint used for the version check. The
// HUGO_LATEST_URL environment variable can point it at a mirror or a test
// server; an empty value disables the automatic check.
func latestReleaseURL() string {
	if value, ok := os.LookupEnv("HUGO_LATEST_URL"); ok {
		return strings.TrimSpace(value)
	}
	return DefaultLatestReleaseURL
}

// LatestHugoVersion returns the newest Hugo release published upstream. The
// result is cached to avoid hitting the API on every page load. An empty
// string means the check is unavailable (for example offline); callers must
// treat it as "unknown" rather than "up to date".
func (svc *Service) LatestHugoVersion(ctx context.Context) string {
	svc.latestMu.Lock()
	if !svc.latestAt.IsZero() && time.Since(svc.latestAt) < svc.latestTTL {
		version := svc.latestVersion
		svc.latestMu.Unlock()
		return version
	}
	svc.latestMu.Unlock()

	version, ttl := fetchLatestHugoVersion(ctx)

	svc.latestMu.Lock()
	svc.latestVersion = version
	svc.latestTTL = ttl
	svc.latestAt = time.Now()
	svc.latestMu.Unlock()
	return version
}

func fetchLatestHugoVersion(ctx context.Context) (string, time.Duration) {
	endpoint := latestReleaseURL()
	if endpoint == "" {
		return "", latestFailureTTL
	}
	cctx, cancel := context.WithTimeout(ctx, latestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", latestFailureTTL
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "HugoPanel")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", latestFailureTTL
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", latestFailureTTL
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, 512*1024))
	if err != nil {
		return "", latestFailureTTL
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return "", latestFailureTTL
	}
	version, ok := ParseHugoVersion(release.TagName)
	if !ok {
		return "", latestFailureTTL
	}
	return version, latestCacheTTL
}
