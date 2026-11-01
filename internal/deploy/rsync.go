// Package deploy copies a Hugo build to a remote server with rsync over SSH.
package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxOutput    = 256 * 1024
	defaultPort  = 22
	defaultLimit = 20 * time.Minute
)

var (
	ErrRunning = errors.New("a deployment is already running")

	hostRe = regexp.MustCompile(`^[A-Za-z0-9._:\[\]-]+$`)
	userRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// Config describes one rsync-over-SSH target. Secrets (the private key and the
// known_hosts content) stay on the server and are never sent to clients.
type Config struct {
	Enabled       bool
	Host          string
	Port          int
	User          string
	Path          string
	Source        string
	Delete        bool
	StrictHostKey bool
	SSHKey        string
	KnownHosts    string
	RsyncPath     string
	Timeout       time.Duration
}

// Result mirrors the shape of a Hugo build result so the UI can render both
// with the same status helpers.
type Result struct {
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
	mu      sync.Mutex
	running bool
	last    *Result
}

func NewService() *Service { return &Service{} }

func (s *Service) Last() *Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return nil
	}
	copy := *s.last
	return &copy
}

func (s *Service) SetLast(r *Result) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r == nil {
		s.last = nil
		return
	}
	copy := *r
	s.last = &copy
}

func (s *Service) LoadPersisted(raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	var r Result
	if err := json.Unmarshal([]byte(raw), &r); err == nil && r.Status != "running" {
		s.SetLast(&r)
	}
}

func (s *Service) MarshalLast() string {
	r := s.Last()
	if r == nil {
		return ""
	}
	b, _ := json.Marshal(r)
	return string(b)
}

// Normalized applies defaults without mutating the caller's value.
func (c Config) Normalized() Config {
	c.Host = strings.TrimSpace(c.Host)
	c.User = strings.TrimSpace(c.User)
	c.Path = strings.TrimSpace(c.Path)
	c.RsyncPath = strings.TrimSpace(c.RsyncPath)
	c.Source = strings.TrimSpace(c.Source)
	if c.Port == 0 {
		c.Port = defaultPort
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultLimit
	}
	return c
}

// Validate checks the target and the SSH material. It deliberately rejects
// whitespace and control characters where the values could otherwise reach the
// remote shell.
func (c Config) Validate() error {
	if c.Host == "" || len(c.Host) > 255 || !hostRe.MatchString(c.Host) {
		return errors.New("a valid remote host is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("SSH port must be between 1 and 65535")
	}
	if c.User == "" || len(c.User) > 64 || !userRe.MatchString(c.User) {
		return errors.New("a valid SSH user is required")
	}
	if c.Path == "" || len(c.Path) > 4096 || strings.ContainsAny(c.Path, "\x00\r\n") {
		return errors.New("a valid remote directory is required")
	}
	if c.Source == "" || len(c.Source) > 4096 || strings.ContainsAny(c.Source, "\x00\r\n") {
		return errors.New("a valid local source directory is required")
	}
	if c.RsyncPath != "" && (len(c.RsyncPath) > 500 || strings.ContainsAny(c.RsyncPath, "\x00\r\n\t ")) {
		return errors.New("invalid remote rsync path")
	}
	if c.StrictHostKey && strings.TrimSpace(c.KnownHosts) == "" {
		return errors.New("known_hosts is required when host key verification is enabled")
	}
	if c.SSHKey != "" && !ValidPrivateKey(c.SSHKey) {
		return errors.New("the SSH private key does not look valid")
	}
	return nil
}

func ValidHost(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && len(v) <= 255 && hostRe.MatchString(v)
}

func ValidUser(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && len(v) <= 64 && userRe.MatchString(v)
}

func ValidRemotePath(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && len(v) <= 4096 && !strings.ContainsAny(v, "\x00\r\n")
}

func ValidSource(v string) bool {
	v = strings.TrimSpace(v)
	return v != "" && len(v) <= 4096 && !strings.ContainsAny(v, "\x00\r\n")
}

func ValidPrivateKey(v string) bool {
	if v == "" || len(v) > 64*1024 || strings.ContainsRune(v, '\x00') {
		return false
	}
	return strings.Contains(v, "PRIVATE KEY")
}

func ValidKnownHosts(v string) bool {
	return len(v) <= 1024*1024 && !strings.ContainsRune(v, '\x00')
}

// Run performs one rsync invocation. Expected failures (missing binary,
// unreachable host, non-zero exit) are reported in the Result; only a
// concurrent invocation returns an error.
func (s *Service) Run(ctx context.Context, cfg Config) (*Result, error) {
	cfg = cfg.Normalized()

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil, ErrRunning
	}
	s.running = true
	start := time.Now().UTC()
	running := &Result{
		Status:    "running",
		StartedAt: start.Format(time.RFC3339),
		Command:   displayCommand(cfg),
	}
	s.last = running
	s.mu.Unlock()

	finish := func(res *Result) *Result {
		res.StartedAt = running.StartedAt
		res.Command = running.Command
		res.DurationMS = time.Since(start).Milliseconds()
		res.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		s.mu.Lock()
		s.running = false
		copy := *res
		s.last = &copy
		s.mu.Unlock()
		return res
	}

	if err := cfg.Validate(); err != nil {
		return finish(&Result{Status: "failed", ExitCode: -1, Output: err.Error()}), nil
	}
	info, err := os.Stat(cfg.Source)
	if err != nil || !info.IsDir() {
		return finish(&Result{Status: "failed", ExitCode: -1, Output: "Local source directory not found: " + cfg.Source}), nil
	}
	rsyncBin, err := exec.LookPath("rsync")
	if err != nil {
		return finish(&Result{Status: "failed", ExitCode: -1, Output: "rsync is not installed in the panel container."}), nil
	}

	tempDir, err := os.MkdirTemp("", "hugo-panel-rsync-")
	if err != nil {
		return finish(&Result{Status: "failed", ExitCode: -1, Output: "Could not prepare the SSH credentials: " + err.Error()}), nil
	}
	defer os.RemoveAll(tempDir)

	keyPath := ""
	if cfg.SSHKey != "" {
		keyPath = filepath.Join(tempDir, "id_deploy")
		if err := os.WriteFile(keyPath, []byte(normalizeKey(cfg.SSHKey)), 0o600); err != nil {
			return finish(&Result{Status: "failed", ExitCode: -1, Output: "Could not write the SSH private key: " + err.Error()}), nil
		}
	}
	knownPath := filepath.Join(tempDir, "known_hosts")
	if err := os.WriteFile(knownPath, []byte(normalizeText(cfg.KnownHosts)), 0o600); err != nil {
		return finish(&Result{Status: "failed", ExitCode: -1, Output: "Could not write known_hosts: " + err.Error()}), nil
	}

	sshArgs := []string{"ssh"}
	if keyPath != "" {
		sshArgs = append(sshArgs, "-i", keyPath, "-o", "IdentitiesOnly=yes")
	}
	if cfg.Port != defaultPort {
		sshArgs = append(sshArgs, "-p", strconv.Itoa(cfg.Port))
	}
	sshArgs = append(sshArgs,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=20",
		"-o", "UserKnownHostsFile="+knownPath,
	)
	if cfg.StrictHostKey {
		sshArgs = append(sshArgs, "-o", "StrictHostKeyChecking=yes")
	} else {
		sshArgs = append(sshArgs, "-o", "StrictHostKeyChecking=accept-new")
	}

	args := []string{"-az", "--human-readable", "--protect-args"}
	if cfg.Delete {
		args = append(args, "--delete")
	}
	if cfg.RsyncPath != "" {
		args = append(args, "--rsync-path="+cfg.RsyncPath)
	}
	args = append(args, "-e", strings.Join(sshArgs, " "))
	source := strings.TrimRight(cfg.Source, "/") + "/"
	args = append(args, source, destination(cfg))

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultLimit
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, rsyncBin, args...)
	out := &limitedBuffer{max: maxOutput}
	cmd.Stdout = out
	cmd.Stderr = out
	runErr := cmd.Run()

	result := &Result{Status: "success", Success: runErr == nil, Output: out.String(), ExitCode: 0}
	if runErr != nil {
		result.Status = "failed"
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		if cctx.Err() == context.DeadlineExceeded {
			runErr = fmt.Errorf("deployment timed out after %s", timeout)
		}
		if result.Output == "" {
			result.Output = runErr.Error()
		} else {
			result.Output += "\n" + runErr.Error()
		}
	}
	if result.Output == "" {
		result.Output = "Deployment completed successfully."
	}
	return finish(result), nil
}

func destination(cfg Config) string {
	path := cfg.Path
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	return cfg.User + "@" + cfg.Host + ":" + path
}

func displayCommand(cfg Config) string {
	parts := []string{"rsync", "-az", "--human-readable", "--protect-args"}
	if cfg.Delete {
		parts = append(parts, "--delete")
	}
	if cfg.RsyncPath != "" {
		parts = append(parts, "--rsync-path="+cfg.RsyncPath)
	}
	ssh := []string{"ssh"}
	if cfg.SSHKey != "" {
		ssh = append(ssh, "-i", "<private-key>", "-o", "IdentitiesOnly=yes")
	}
	if cfg.Port != defaultPort {
		ssh = append(ssh, "-p", strconv.Itoa(cfg.Port))
	}
	ssh = append(ssh, "-o", "BatchMode=yes", "-o", "UserKnownHostsFile=<known_hosts>")
	if cfg.StrictHostKey {
		ssh = append(ssh, "-o", "StrictHostKeyChecking=yes")
	} else {
		ssh = append(ssh, "-o", "StrictHostKeyChecking=accept-new")
	}
	parts = append(parts, "-e", fmt.Sprintf("%q", strings.Join(ssh, " ")))
	source := strings.TrimRight(cfg.Source, "/") + "/"
	parts = append(parts, source, destination(cfg))
	return strings.Join(parts, " ")
}

func normalizeKey(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasSuffix(v, "\n") {
		v += "\n"
	}
	return v
}

func normalizeText(v string) string {
	v = strings.TrimRight(v, "\r\n")
	if v == "" {
		return ""
	}
	return v + "\n"
}

type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if b.max <= 0 {
		return n, nil
	}
	remain := b.max - b.buf.Len()
	if remain > 0 {
		if len(p) > remain {
			b.buf.Write(p[:remain])
		} else {
			b.buf.Write(p)
		}
	}
	return n, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buf.String())
}
