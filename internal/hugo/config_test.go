package hugo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readTestConfig(t *testing.T, dir, rel string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	m, err := decodeConfig(data, formatFromExt(rel))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestLoadConfigReadsSplitDirectory(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "config/_default/hugo.toml", `baseURL = "https://example.com/"
title = "Split site"
theme = "ananke"

[markup]
  [markup.goldmark.renderer]
    unsafe = true
`)
	writeTestFile(t, dir, "config/_default/params.toml", `author = "Alice"
subtitle = "Hello"

[colors]
  primary = "#123456"
`)
	writeTestFile(t, dir, "config/_default/menus.toml", `[[main]]
  name = "Home"
  pageRef = "/"
  weight = 10
`)
	writeTestFile(t, dir, "config/_default/module.toml", `[[imports]]
  path = "github.com/example/module"
`)

	site := &Site{Dir: dir}
	cfg, err := site.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigPath != "config/_default/hugo.toml" {
		t.Fatalf("config path: %q", cfg.ConfigPath)
	}
	if len(cfg.ConfigFiles) != 4 {
		t.Fatalf("config files: %#v", cfg.ConfigFiles)
	}
	if cfg.SiteTitle != "Split site" || cfg.BaseURL != "https://example.com/" || cfg.Theme != "ananke" {
		t.Fatalf("basic config: %#v", cfg)
	}
	if cfg.Params["author"] != "Alice" || cfg.Params["subtitle"] != "Hello" {
		t.Fatalf("params: %#v", cfg.Params)
	}
	colors, _ := cfg.Params["colors"].(map[string]any)
	if colors["primary"] != "#123456" {
		t.Fatalf("nested params: %#v", cfg.Params)
	}
	if _, ok := cfg.Extra["markup"]; !ok {
		t.Fatalf("markup should be an extra parameter: %#v", cfg.Extra)
	}
	for _, managed := range []string{"title", "baseURL", "theme", "params", "menus", "module"} {
		if _, ok := cfg.Extra[managed]; ok {
			t.Fatalf("%s should not be an extra parameter: %#v", managed, cfg.Extra)
		}
	}
	if len(cfg.Menus["main"]) != 1 || cfg.Menus["main"][0].Name != "Home" {
		t.Fatalf("menus: %#v", cfg.Menus)
	}
	if len(cfg.Modules) != 1 || cfg.Modules[0].Path != "github.com/example/module" {
		t.Fatalf("modules: %#v", cfg.Modules)
	}
}

func TestSaveConfigWritesSplitFilesBack(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "config/_default/hugo.toml", `baseURL = "https://example.com/"
title = "Split site"

[imaging]
  quality = 80
`)
	writeTestFile(t, dir, "config/_default/params.toml", `author = "Alice"

[colors]
  primary = "#123456"
`)
	writeTestFile(t, dir, "config/_default/menus.toml", `[[main]]
  name = "Home"
  pageRef = "/"
`)

	site := &Site{Dir: dir, DataDir: filepath.Join(dir, "data")}
	title := "Updated site"
	if _, err := site.SaveConfig(ConfigPatch{
		SiteTitle: &title,
		Params: map[string]any{
			"author": "Bob",
			"colors": map[string]any{"primary": "#000000", "accent": "red"},
			"new":    true,
		},
		Extra: map[string]any{
			"outputs":  map[string]any{"home": []any{"HTML"}},
			"siteType": "blog",
		},
	}); err != nil {
		t.Fatal(err)
	}

	hugo := readTestConfig(t, dir, "config/_default/hugo.toml")
	if hugo["title"] != "Updated site" {
		t.Fatalf("title not written: %#v", hugo)
	}
	if _, ok := hugo["params"]; ok {
		t.Fatalf("params should stay in params.toml: %#v", hugo)
	}
	if _, ok := hugo["imaging"]; ok {
		t.Fatalf("deleted extra key should be removed: %#v", hugo)
	}
	if hugo["siteType"] != "blog" {
		t.Fatalf("new extra key missing: %#v", hugo)
	}
	outputs, _ := hugo["outputs"].(map[string]any)
	if outputs == nil {
		t.Fatalf("outputs missing: %#v", hugo)
	}

	params := readTestConfig(t, dir, "config/_default/params.toml")
	if params["author"] != "Bob" || params["new"] != true {
		t.Fatalf("params file: %#v", params)
	}
	colors, _ := params["colors"].(map[string]any)
	if colors["primary"] != "#000000" || colors["accent"] != "red" {
		t.Fatalf("nested params file: %#v", params)
	}

	menus := readTestConfig(t, dir, "config/_default/menus.toml")
	if menus["main"] == nil {
		t.Fatalf("menus file should be preserved: %#v", menus)
	}

	cfg, err := site.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SiteTitle != "Updated site" || cfg.Params["author"] != "Bob" || cfg.Extra["siteType"] != "blog" {
		t.Fatalf("reloaded config: %#v", cfg)
	}
	if _, ok := cfg.Extra["imaging"]; ok {
		t.Fatalf("imaging should have been deleted: %#v", cfg.Extra)
	}
	if _, ok := cfg.Params["colors"].(map[string]any)["accent"]; !ok {
		t.Fatalf("new nested param missing: %#v", cfg.Params)
	}
}

func TestSaveConfigCreatesRootFileInSplitSite(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "config/_default/params.toml", `author = "Alice"
`)

	site := &Site{Dir: dir}
	cfg, err := site.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigPath != "config/_default/params.toml" || cfg.Params["author"] != "Alice" {
		t.Fatalf("initial config: %#v", cfg)
	}
	title := "New title"
	theme := "ananke"
	if _, err := site.SaveConfig(ConfigPatch{SiteTitle: &title, Theme: &theme}); err != nil {
		t.Fatal(err)
	}

	hugo := readTestConfig(t, dir, "config/_default/hugo.toml")
	if hugo["title"] != "New title" || hugo["theme"] != "ananke" {
		t.Fatalf("root file: %#v", hugo)
	}
	params := readTestConfig(t, dir, "config/_default/params.toml")
	if params["author"] != "Alice" {
		t.Fatalf("params should not be polluted: %#v", params)
	}
	if _, ok := params["title"]; ok {
		t.Fatalf("title leaked into params.toml: %#v", params)
	}
}

func TestSaveConfigRejectsReservedExtraKey(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "hugo.toml", `title = "Before"
`)
	site := &Site{Dir: dir}
	if _, err := site.SaveConfig(ConfigPatch{Extra: map[string]any{"title": "After"}}); err == nil {
		t.Fatal("expected reserved key error")
	}
	cfg, err := site.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SiteTitle != "Before" {
		t.Fatalf("config was modified: %#v", cfg)
	}
}

func TestExtraParametersSingleFile(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "hugo.toml", `baseURL = "https://example.com/"
title = "Single"

[params]
  author = "Alice"

[outputs]
  home = ["HTML", "RSS"]
`)
	site := &Site{Dir: dir}
	cfg, err := site.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Extra["outputs"]; !ok {
		t.Fatalf("outputs should be extra: %#v", cfg.Extra)
	}
	if _, ok := cfg.Extra["params"]; ok {
		t.Fatalf("params should not be extra: %#v", cfg.Extra)
	}
	title := "Single"
	if _, err := site.SaveConfig(ConfigPatch{
		SiteTitle: &title,
		Extra: map[string]any{
			"outputs":    map[string]any{"home": []any{"HTML"}},
			"pagination": map[string]any{"pagerSize": int64(10)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err = site.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Params["author"] != "Alice" {
		t.Fatalf("params lost: %#v", cfg.Params)
	}
	if _, ok := cfg.Extra["outputs"]; !ok {
		t.Fatalf("outputs lost: %#v", cfg.Extra)
	}
	if _, ok := cfg.Extra["pagination"]; !ok {
		t.Fatalf("pagination not saved: %#v", cfg.Extra)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "hugo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "author") == false {
		t.Fatalf("params were not persisted: %s", raw)
	}
}

func TestSplitYAMLConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "config/_default/hugo.yaml", `baseURL: https://example.com/
title: YAML site
`)
	writeTestFile(t, dir, "config/_default/params.yaml", `author: Alice
colors:
  primary: "#123456"
`)

	site := &Site{Dir: dir}
	cfg, err := site.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SiteTitle != "YAML site" || cfg.Params["author"] != "Alice" {
		t.Fatalf("initial config: %#v", cfg)
	}
	if _, err := site.SaveConfig(ConfigPatch{Params: map[string]any{
		"author": "Bob",
		"colors": map[string]any{"primary": "#abcdef"},
	}}); err != nil {
		t.Fatal(err)
	}
	params := readTestConfig(t, dir, "config/_default/params.yaml")
	if params["author"] != "Bob" {
		t.Fatalf("params file: %#v", params)
	}
	colors, _ := params["colors"].(map[string]any)
	if colors["primary"] != "#abcdef" {
		t.Fatalf("nested params file: %#v", params)
	}
	hugo := readTestConfig(t, dir, "config/_default/hugo.yaml")
	if hugo["title"] != "YAML site" {
		t.Fatalf("root file was changed: %#v", hugo)
	}
}

func TestSplitConfigIgnoresLanguageAndEnvironmentFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "config/_default/hugo.toml", `title = "Default"
`)
	writeTestFile(t, dir, "config/_default/params.toml", `author = "Alice"
`)
	writeTestFile(t, dir, "config/_default/params.zh.toml", `author = "小明"
`)
	writeTestFile(t, dir, "config/production/params.toml", `author = "Production"
`)

	site := &Site{Dir: dir}
	cfg, err := site.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Params["author"] != "Alice" {
		t.Fatalf("unexpected params: %#v", cfg.Params)
	}
	if len(cfg.ConfigFiles) != 2 {
		t.Fatalf("language and environment files should be excluded: %#v", cfg.ConfigFiles)
	}
	title := "Changed"
	if _, err := site.SaveConfig(ConfigPatch{SiteTitle: &title}); err != nil {
		t.Fatal(err)
	}
	lang := readTestConfig(t, dir, "config/_default/params.zh.toml")
	if lang["author"] != "小明" {
		t.Fatalf("language file was modified: %#v", lang)
	}
	prod := readTestConfig(t, dir, "config/production/params.toml")
	if prod["author"] != "Production" {
		t.Fatalf("environment file was modified: %#v", prod)
	}
}
