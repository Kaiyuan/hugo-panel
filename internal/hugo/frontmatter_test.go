package hugo

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSplitFrontMatterFormats(t *testing.T) {
	yamlDoc := []byte("---\ntitle: Hello\ndraft: true\ntags:\n  - go\n---\n# Body\n")
	format, fm, body, err := SplitFrontMatter(yamlDoc)
	if err != nil || format != FormatYAML {
		t.Fatalf("yaml: format=%q err=%v", format, err)
	}
	if fm["title"] != "Hello" || fm["draft"] != true || body != "# Body\n" {
		t.Fatalf("yaml parsed wrong: %#v body=%q", fm, body)
	}

	tomlDoc := []byte("+++\ntitle = \"Hello\"\nweight = 10\n+++\nBody\n")
	format, fm, body, err = SplitFrontMatter(tomlDoc)
	if err != nil || format != FormatTOML {
		t.Fatalf("toml: format=%q err=%v", format, err)
	}
	if fm["title"] != "Hello" || IntValue(fm["weight"]) != 10 || body != "Body\n" {
		t.Fatalf("toml parsed wrong: %#v body=%q", fm, body)
	}

	jsonDoc := []byte("{\"title\":\"Hello\",\"draft\":false}\n\nBody\n")
	format, fm, body, err = SplitFrontMatter(jsonDoc)
	if err != nil || format != FormatJSON {
		t.Fatalf("json: format=%q err=%v", format, err)
	}
	if fm["title"] != "Hello" || body != "Body\n" {
		t.Fatalf("json parsed wrong: %#v body=%q", fm, body)
	}

	format, fm, body, err = SplitFrontMatter([]byte("plain body"))
	if err != nil || format != FormatYAML || len(fm) != 0 || body != "plain body" {
		t.Fatalf("plain body: format=%q fm=%#v body=%q err=%v", format, fm, body, err)
	}
}

func TestEncodeFrontMatterRoundTrip(t *testing.T) {
	fm := map[string]any{"title": "Round trip", "draft": false, "weight": int64(5), "tags": []any{"a", "b"}}
	for _, format := range []string{FormatYAML, FormatTOML, FormatJSON} {
		data, err := EncodeFrontMatter(format, fm, "Hello\n")
		if err != nil {
			t.Fatalf("%s encode: %v", format, err)
		}
		gotFormat, gotFM, body, err := SplitFrontMatter(data)
		if err != nil {
			t.Fatalf("%s split: %v", format, err)
		}
		if gotFormat != format || body != "Hello\n" {
			t.Fatalf("%s roundtrip: format=%s body=%q", format, gotFormat, body)
		}
		if gotFM["title"] != "Round trip" || IntValue(gotFM["weight"]) != 5 {
			t.Fatalf("%s front matter: %#v", format, gotFM)
		}
		if got := StringSlice(gotFM["tags"]); len(got) != 2 || got[0] != "a" {
			t.Fatalf("%s tags: %#v", format, got)
		}
	}
}

func TestContentLifecycle(t *testing.T) {
	dir := t.TempDir()
	site := &Site{Dir: dir, Binary: "hugo", DataDir: filepath.Join(dir, "data")}
	item, err := site.CreateContent(SaveInput{
		Path:   "posts/hello.md",
		Format: FormatYAML,
		FrontMatter: map[string]any{
			"title": "Hello",
			"draft": true,
		},
		Body: "# Hello\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.Title != "Hello" || item.Status != "draft" || item.Section != "posts" || item.WordCount != 1 {
		t.Fatalf("unexpected item: %#v", item)
	}
	if _, err := site.CreateContent(SaveInput{Path: "posts/hello.md", Format: FormatYAML, FrontMatter: map[string]any{}}); err == nil {
		t.Fatal("expected duplicate path error")
	}
	updated, err := site.UpdateContent(SaveInput{
		Path:        "posts/hello.md",
		NewPath:     "journal/hello.md",
		Format:      FormatTOML,
		FrontMatter: map[string]any{"title": "Hello again", "draft": false, "date": "2020-01-02"},
		Body:        "Body\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Path != "journal/hello.md" || updated.Title != "Hello again" || updated.Status != "published" {
		t.Fatalf("unexpected update: %#v", updated)
	}
	if _, err := os.Stat(filepath.Join(dir, "content", "posts", "hello.md")); !os.IsNotExist(err) {
		t.Fatalf("old file still exists: %v", err)
	}
	meta, err := site.ContentMeta()
	if err != nil || len(meta.Sections) != 1 || meta.Sections[0].Name != "journal" {
		t.Fatalf("meta: %#v err=%v", meta, err)
	}
	res, err := site.ListContent(ListOptions{Query: "again", Page: 1, Limit: 10})
	if err != nil || res.Total != 1 {
		t.Fatalf("list: %#v err=%v", res, err)
	}
	if err := site.DeleteContent("journal/hello.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := site.ReadContent("journal/hello.md"); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestPathTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	site := &Site{Dir: dir}
	for _, p := range []string{"../escape.md", "/absolute.md", "posts/../../escape.md", "posts\\evil.md"} {
		if _, err := site.CreateContent(SaveInput{Path: p, FrontMatter: map[string]any{"title": "x"}, Body: "x"}); err == nil {
			t.Fatalf("path %q should be rejected", p)
		}
	}
}

func TestConfigMenusAndModulesFromTOML(t *testing.T) {
	dir := t.TempDir()
	content := `baseURL = "https://example.com/"
title = "Test"
theme = "ananke"

[module]
  [[module.imports]]
    path = "github.com/example/module"

[menus]
  [[menus.main]]
    name = "Home"
    pageRef = "/"
    weight = 1
`
	if err := os.WriteFile(filepath.Join(dir, "hugo.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	site := &Site{Dir: dir}
	cfg, err := site.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SiteTitle != "Test" || cfg.Theme != "ananke" || len(cfg.Themes) != 1 {
		t.Fatalf("config: %#v", cfg)
	}
	if len(cfg.Menus["main"]) != 1 || cfg.Menus["main"][0].Name != "Home" || cfg.Menus["main"][0].PageRef != "/" {
		t.Fatalf("menus: %#v", cfg.Menus)
	}
	if len(cfg.Modules) != 1 || cfg.Modules[0].Path != "github.com/example/module" {
		t.Fatalf("modules: %#v", cfg.Modules)
	}
	title := "Updated"
	theme := ""
	if _, err := site.SaveConfig(ConfigPatch{SiteTitle: &title, Theme: &theme}); err != nil {
		t.Fatal(err)
	}
	reload, err := site.LoadConfig()
	if err != nil || reload.SiteTitle != "Updated" || reload.Theme != "" {
		t.Fatalf("reload: %#v err=%v", reload, err)
	}
}

func TestInitSiteAndMedia(t *testing.T) {
	dir := t.TempDir()
	site := &Site{Dir: dir, DataDir: filepath.Join(dir, "data")}
	if err := site.InitSite("My site", "https://example.com/", "en"); err != nil {
		t.Fatal(err)
	}
	if !site.ConfigExists() {
		t.Fatal("config should exist")
	}
	if err := site.InitSite("Again", "", "en"); err == nil {
		t.Fatal("second init should fail")
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	media, err := site.SaveMedia(bytesReader(png), "My Photo.PNG", "")
	if err != nil {
		t.Fatal(err)
	}
	if media.Name != "My-Photo.png" {
		t.Fatalf("sanitized name: %q", media.Name)
	}
	if _, err := site.SaveMedia(bytesReader(png), "evil.svg", ""); err == nil {
		t.Fatal("svg should be rejected")
	}
	files, err := site.ListMedia()
	if err != nil || len(files) != 1 {
		t.Fatalf("media list: %#v err=%v", files, err)
	}
	if err := site.DeleteMedia(media.Path); err != nil {
		t.Fatal(err)
	}
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
