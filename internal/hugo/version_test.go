package hugo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseHugoVersion(t *testing.T) {
	cases := map[string]string{
		"hugo v0.169.0+extended linux/amd64 BuildDate=2026-10-01T00:00:00Z VendorInfo=gohugoio": "0.169.0",
		"hugo v0.150.1+extended+withdeploy darwin/arm64":                                        "0.150.1",
		"hugo v0.92.0-DEV": "0.92.0",
		"0.169.0":          "0.169.0",
	}
	for input, want := range cases {
		got, ok := ParseHugoVersion(input)
		if !ok || got != want {
			t.Fatalf("ParseHugoVersion(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	if _, ok := ParseHugoVersion("hugo unknown build"); ok {
		t.Fatal("expected an unparsable version to be rejected")
	}
}

func TestHugoVersionAtLeast(t *testing.T) {
	cases := []struct {
		version string
		minimum string
		want    bool
	}{
		{"0.169.0", "0.169.0", true},
		{"0.170.0", "0.169.0", true},
		{"0.169.1", "0.169.0", true},
		{"0.168.9", "0.169.0", false},
		{"0.150.1", "0.169.0", false},
		{"1.0.0", "0.169.0", true},
		{"", "0.169.0", false},
	}
	for _, c := range cases {
		if got := HugoVersionAtLeast(c.version, c.minimum); got != c.want {
			t.Fatalf("HugoVersionAtLeast(%q, %q) = %v; want %v", c.version, c.minimum, got, c.want)
		}
	}
}

func fakeHugo(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hugo")
	script := "#!/bin/sh\necho \"" + output + "\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnsureHugoVersion(t *testing.T) {
	site := &Site{Binary: fakeHugo(t, "hugo v0.169.1+extended linux/amd64")}
	version, err := site.EnsureHugoVersion(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if version != "0.169.1" {
		t.Fatalf("version = %q", version)
	}

	site.Binary = fakeHugo(t, "hugo v0.150.1+extended linux/amd64")
	version, err = site.EnsureHugoVersion(t.Context())
	if err == nil {
		t.Fatal("expected an outdated-version error")
	}
	if version != "0.150.1" {
		t.Fatalf("version = %q", version)
	}
	he, ok := err.(*Error)
	if !ok || he.Code != "hugo_outdated" {
		t.Fatalf("unexpected error: %#v", err)
	}
	if !strings.Contains(he.Message, MinHugoVersion) {
		t.Fatalf("message should mention the minimum version: %q", he.Message)
	}

	site.Binary = filepath.Join(t.TempDir(), "missing-hugo")
	if _, err := site.EnsureHugoVersion(t.Context()); err == nil {
		t.Fatal("expected a missing-binary error")
	}
}
