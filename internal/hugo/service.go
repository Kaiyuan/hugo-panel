package hugo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type BuildResult struct {
	Status     string `json:"status"` // running, success, failed
	Success    bool   `json:"success"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output"`
	ExitCode   int    `json:"exit_code"`
	Command    string `json:"command"`
}

type Service struct {
	mu       sync.Mutex
	building bool
	last     *BuildResult

	versionMu    sync.Mutex
	versionCache map[string]versionEntry
}

type versionEntry struct {
	version string
	ok      bool
	at      time.Time
}

var ErrBuildRunning = errors.New("build is already running")

func NewService() *Service {
	return &Service{versionCache: map[string]versionEntry{}}
}

func (svc *Service) Site(dir, binary, dataDir string) *Site {
	return &Site{Dir: dir, Binary: binary, DataDir: dataDir}
}

func (svc *Service) LastBuild() *BuildResult {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if svc.last == nil {
		return nil
	}
	copy := *svc.last
	return &copy
}

func (svc *Service) SetLastBuild(r *BuildResult) {
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if r == nil {
		svc.last = nil
		return
	}
	copy := *r
	svc.last = &copy
}

func (svc *Service) LoadPersistedBuild(raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	var r BuildResult
	if err := json.Unmarshal([]byte(raw), &r); err == nil && r.Status != "running" {
		svc.SetLastBuild(&r)
	}
}

func (svc *Service) MarshalLastBuild() string {
	r := svc.LastBuild()
	if r == nil {
		return ""
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func (svc *Service) Build(ctx context.Context, site *Site) (*BuildResult, error) {
	svc.mu.Lock()
	if svc.building {
		svc.mu.Unlock()
		return nil, ErrBuildRunning
	}
	svc.building = true
	start := time.Now().UTC()
	running := &BuildResult{
		Status:    "running",
		StartedAt: start.Format(time.RFC3339),
		Command:   "hugo --gc --minify --cleanDestinationDir",
	}
	svc.last = running
	svc.mu.Unlock()

	finish := func(res *BuildResult) *BuildResult {
		res.StartedAt = running.StartedAt
		res.Command = running.Command
		res.DurationMS = time.Since(start).Milliseconds()
		res.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		svc.mu.Lock()
		svc.building = false
		copy := *res
		svc.last = &copy
		svc.mu.Unlock()
		return res
	}

	if !site.ConfigExists() {
		return finish(&BuildResult{Status: "failed", ExitCode: -1, Output: "Hugo configuration not found. Initialize the site first."}), nil
	}
	if _, err := exec.LookPath(site.Binary); err != nil {
		return finish(&BuildResult{Status: "failed", ExitCode: -1, Output: "Hugo binary not found: " + site.Binary}), nil
	}
	res := runCommand(ctx, site.Dir, 15*time.Minute, []string{"HUGO_ENV=production"}, site.Binary, "--gc", "--minify", "--cleanDestinationDir")
	result := &BuildResult{
		Status:   "success",
		Success:  res.Err == nil,
		Output:   res.Output,
		ExitCode: res.ExitCode,
	}
	if res.Err != nil {
		result.Status = "failed"
		if result.Output == "" {
			result.Output = res.Err.Error()
		} else {
			result.Output += "\n" + res.Err.Error()
		}
	}
	if result.Output == "" {
		result.Output = "Build completed successfully."
	}
	return finish(result), nil
}

func (svc *Service) HugoVersion(ctx context.Context, site *Site) (string, bool) {
	key := site.Binary
	svc.versionMu.Lock()
	if e, ok := svc.versionCache[key]; ok && time.Since(e.at) < 5*time.Minute {
		svc.versionMu.Unlock()
		return e.version, e.ok
	}
	svc.versionMu.Unlock()

	version, ok := "", false
	dir := site.Dir
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir = ""
	}
	if _, err := exec.LookPath(site.Binary); err == nil {
		res := runCommand(ctx, dir, 15*time.Second, nil, site.Binary, "version")
		if res.Err == nil {
			if parsed, parsedOK := ParseHugoVersion(res.Output); parsedOK {
				version = parsed
			} else {
				version = firstLine(res.Output)
			}
			ok = true
		}
	}
	svc.versionMu.Lock()
	svc.versionCache[key] = versionEntry{version: version, ok: ok, at: time.Now()}
	svc.versionMu.Unlock()
	return version, ok
}
