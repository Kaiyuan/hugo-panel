package hugo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeHugoBinary(t *testing.T, dir, version string) string {
	t.Helper()
	path := filepath.Join(dir, "hugo")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"version\" ]; then\n" +
		"  echo \"hugo v" + version + "+extended linux/amd64 BuildDate=unknown\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func testSite(t *testing.T, version, themeMinVersion string) *Site {
	t.Helper()
	root := t.TempDir()
	binary := fakeHugoBinary(t, root, version)
	dir := filepath.Join(root, "site")
	if err := os.MkdirAll(filepath.Join(dir, "themes", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hugo.toml"), []byte("baseURL = \"https://example.com/\"\ntheme = \"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := "name = \"Demo\"\nmin_version = \"" + themeMinVersion + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "themes", "demo", "theme.toml"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Site{Dir: dir, Binary: binary}
}

func TestThemeCompatibilityDetectsMinVersion(t *testing.T) {
	site := testSite(t, "0.100.0", "0.150.0")
	result, err := site.CheckThemeCompatibility(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "incompatible" || result.Code != "min_version" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Compatible {
		t.Fatal("theme must be reported as incompatible")
	}
	if !strings.Contains(result.Reason, "0.150.0") {
		t.Fatalf("reason should mention the required version: %q", result.Reason)
	}
}

func TestThemeCompatibilityBuildsWithCompatibleVersion(t *testing.T) {
	site := testSite(t, "0.100.0", "0.50.0")
	result, err := site.CheckThemeCompatibility(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compatible || result.Status != "compatible" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestPluginCompatibilityWithoutModules(t *testing.T) {
	site := testSite(t, "0.100.0", "0.50.0")
	result, err := site.CheckPluginsCompatibility(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compatible || result.Code != "no_modules" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestUpgradeCompatibilityRejectsOlderHugo(t *testing.T) {
	site := testSite(t, "0.100.0", "0.150.0")
	result, err := site.CheckUpgradeCompatibility(context.Background(), site.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if result.Compatible || result.Status != "incompatible" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(result.Themes) != 1 || result.Themes[0].Code != "min_version" {
		t.Fatalf("theme check missing: %#v", result.Themes)
	}
	if result.Reason == "" {
		t.Fatal("upgrade incompatibility must include a reason")
	}
}

func TestHugoAssetName(t *testing.T) {
	name, err := hugoAssetName("0.169.0", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if name != "hugo_extended_0.169.0_linux-amd64.tar.gz" {
		t.Fatalf("unexpected asset name %q", name)
	}
	name, err = hugoAssetName("0.169.0", "darwin", "arm64")
	if err != nil || name != "hugo_extended_0.169.0_darwin-universal.tar.gz" {
		t.Fatalf("unexpected darwin asset %q (%v)", name, err)
	}
	if _, err := hugoAssetName("0.169.0", "plan9", "amd64"); err == nil {
		t.Fatal("unsupported platform should fail")
	}
}
