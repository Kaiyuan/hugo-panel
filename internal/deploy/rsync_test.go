package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeRsync(t *testing.T, script string) (capture string) {
	t.Helper()
	binDir := t.TempDir()
	capture = filepath.Join(binDir, "args.txt")
	path := filepath.Join(binDir, "rsync")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CAPTURE_FILE", capture)
	return capture
}

func sourceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hello</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func validConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		Host:          "example.com",
		Port:          22,
		User:          "deploy",
		Path:          "/var/www/example.com",
		Source:        sourceDir(t),
		Delete:        true,
		StrictHostKey: true,
		KnownHosts:    "example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest",
		SSHKey:        "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----",
		Timeout:       time.Minute,
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing host", func(c *Config) { c.Host = "" }},
		{"bad host", func(c *Config) { c.Host = "example.com;rm" }},
		{"bad port", func(c *Config) { c.Port = 70000 }},
		{"bad user", func(c *Config) { c.User = "deploy user" }},
		{"missing path", func(c *Config) { c.Path = "" }},
		{"missing source", func(c *Config) { c.Source = "" }},
		{"strict without known hosts", func(c *Config) { c.KnownHosts = "" }},
		{"bad key", func(c *Config) { c.SSHKey = "not-a-key" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validConfig(t)
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if err := validConfig(t).Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	// Host verification can be disabled without known_hosts.
	cfg := validConfig(t)
	cfg.StrictHostKey = false
	cfg.KnownHosts = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config without host verification rejected: %v", err)
	}
}

func TestRunWithFakeRsync(t *testing.T) {
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > \"$CAPTURE_FILE\"\n" +
		"for arg in \"$@\"; do\n" +
		"  case \"$arg\" in\n" +
		"    ssh\\ *)\n" +
		"      for tok in $arg; do\n" +
		"        case \"$tok\" in\n" +
		"          UserKnownHostsFile=*) f=\"${tok#UserKnownHostsFile=}\"; cat \"$f\" > \"$CAPTURE_FILE.known\";;\n" +
		"        esac\n" +
		"      done;;\n" +
		"  esac\n" +
		"done\n" +
		"echo 'sent 1 file'\n"
	capture := fakeRsync(t, script)
	cfg := validConfig(t)

	svc := NewService()
	result, err := svc.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Status != "success" || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if !strings.Contains(result.Output, "sent 1 file") {
		t.Fatalf("output = %q", result.Output)
	}
	if strings.Contains(result.Command, "PRIVATE KEY") {
		t.Fatal("command leaked the private key")
	}

	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	args := string(raw)
	for _, want := range []string{
		"-az", "--human-readable", "--protect-args", "--delete",
		"-e", "StrictHostKeyChecking=yes", "UserKnownHostsFile=",
		"deploy@example.com:/var/www/example.com/",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("rsync args missing %q:\n%s", want, args)
		}
	}
	source := strings.TrimRight(cfg.Source, "/") + "/"
	if !strings.Contains(args, source) {
		t.Errorf("rsync args missing source %q:\n%s", source, args)
	}
	known, err := os.ReadFile(capture + ".known")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(known), cfg.KnownHosts) {
		t.Fatalf("known_hosts content not written: %q", known)
	}
}

func TestRunReportsFailure(t *testing.T) {
	fakeRsync(t, "#!/bin/sh\necho 'connection refused' >&2\nexit 3\n")
	svc := NewService()
	result, err := svc.Run(context.Background(), validConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || result.Status != "failed" || result.ExitCode != 3 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if !strings.Contains(result.Output, "connection refused") {
		t.Fatalf("output = %q", result.Output)
	}
}

func TestRunMissingSource(t *testing.T) {
	fakeRsync(t, "#!/bin/sh\nexit 0\n")
	cfg := validConfig(t)
	cfg.Source = filepath.Join(t.TempDir(), "missing")
	svc := NewService()
	result, err := svc.Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if result.Success || result.ExitCode != -1 || !strings.Contains(result.Output, "not found") {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPersistedResult(t *testing.T) {
	svc := NewService()
	svc.LoadPersisted(`{"status":"success","success":true,"output":"ok"}`)
	last := svc.Last()
	if last == nil || !last.Success || last.Output != "ok" {
		t.Fatalf("last = %#v", last)
	}
	svc.SetLast(nil)
	svc.LoadPersisted(`{"status":"running"}`)
	if svc.Last() != nil {
		t.Fatal("running result should not be restored")
	}
}
