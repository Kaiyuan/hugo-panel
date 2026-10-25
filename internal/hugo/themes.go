package hugo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Theme struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	License     string   `json:"license"`
	Homepage    string   `json:"homepage"`
	Author      string   `json:"author"`
	Tags        []string `json:"tags"`
	MinVersion  string   `json:"min_version"`
	Active      bool     `json:"active"`
	HasGit      bool     `json:"has_git"`
	Screenshot  bool     `json:"screenshot"`
}

var installNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

func (s *Site) ThemesDir() string { return filepath.Join(s.Dir, "themes") }

func (s *Site) ListThemes() ([]Theme, error) {
	cfg, err := s.LoadConfig()
	if err != nil {
		return nil, err
	}
	active := map[string]bool{}
	for _, name := range cfg.Themes {
		active[name] = true
	}
	root := s.ThemesDir()
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return []Theme{}, nil
	}
	if err != nil {
		return nil, err
	}
	themes := make([]Theme, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		theme := Theme{Name: name, Title: name, Tags: []string{}, Active: active[name]}
		dir := filepath.Join(root, name)
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			theme.HasGit = true
		}
		for _, metaName := range []string{"theme.toml", "theme.yaml", "theme.yml", "theme.json"} {
			metaPath := filepath.Join(dir, metaName)
			data, err := os.ReadFile(metaPath)
			if err != nil {
				continue
			}
			m, err := decodeConfig(data, formatFromExt(metaName))
			if err != nil {
				continue
			}
			if v := StringValue(m["name"]); v != "" {
				theme.Title = v
			}
			theme.Description = StringValue(m["description"])
			theme.License = StringValue(m["license"])
			theme.Homepage = safeHTTPURL(StringValue(m["homepage"]))
			theme.MinVersion = StringValue(m["min_version"])
			if theme.MinVersion == "" {
				theme.MinVersion = StringValue(m["minVersion"])
			}
			switch a := m["author"].(type) {
			case string:
				theme.Author = a
			case map[string]any:
				theme.Author = StringValue(a["name"])
			}
			theme.Tags = StringSlice(m["tags"])
			if theme.Tags == nil {
				theme.Tags = []string{}
			}
			break
		}
		theme.Screenshot = s.themeScreenshot(name) != ""
		themes = append(themes, theme)
	}
	sort.Slice(themes, func(i, j int) bool {
		if themes[i].Active != themes[j].Active {
			return themes[i].Active
		}
		return strings.ToLower(themes[i].Name) < strings.ToLower(themes[j].Name)
	})
	return themes, nil
}

func (s *Site) themeScreenshot(name string) string {
	if !installNameRe.MatchString(name) {
		return ""
	}
	candidates := []string{
		filepath.Join("images", "screenshot.png"), filepath.Join("images", "screenshot.jpg"), filepath.Join("images", "screenshot.jpeg"),
		filepath.Join("images", "tn.png"), filepath.Join("images", "tn.jpg"),
		"screenshot.png", "screenshot.jpg", "preview.png",
	}
	for _, rel := range candidates {
		full := filepath.Join(s.ThemesDir(), name, rel)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return full
		}
	}
	return ""
}

func (s *Site) ThemeScreenshotFile(name string) (string, error) {
	full := s.themeScreenshot(name)
	if full == "" {
		return "", errf("not_found", "Screenshot not found")
	}
	return full, nil
}

func (s *Site) ActivateTheme(name string) error {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "..") || !installNameRe.MatchString(name) {
		return errf("invalid_request", "Invalid theme name")
	}
	if info, err := os.Stat(filepath.Join(s.ThemesDir(), name)); err != nil || !info.IsDir() {
		return errf("theme_not_found", "Theme is not installed")
	}
	if !s.ConfigExists() {
		return errf("not_initialized", "Hugo site is not initialized yet")
	}
	_, err := s.SaveConfig(ConfigPatch{Theme: &name})
	return err
}

func (s *Site) InstallTheme(ctx context.Context, repoURL, name string) (Theme, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return Theme{}, errf("invalid_request", "Repository URL is required")
	}
	if err := validateGitURL(repoURL); err != nil {
		return Theme{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = deriveRepoName(repoURL)
	}
	if !installNameRe.MatchString(name) {
		return Theme{}, errf("invalid_request", "Invalid theme folder name")
	}
	if err := os.MkdirAll(s.ThemesDir(), 0o755); err != nil {
		return Theme{}, err
	}
	target := filepath.Join(s.ThemesDir(), name)
	if _, err := os.Stat(target); err == nil {
		return Theme{}, errf("file_exists", "A theme with this name already exists")
	}
	res := runCommand(ctx, "", 4*time.Minute, []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false"}, "git", "clone", "--depth", "1", "--recurse-submodules", "--", repoURL, target)
	if res.Err != nil {
		_ = os.RemoveAll(target)
		msg := res.Output
		if msg == "" {
			msg = res.Err.Error()
		}
		return Theme{}, errf("git_failed", "Could not clone theme: "+msg)
	}
	for _, theme := range mustListThemes(s) {
		if theme.Name == name {
			return theme, nil
		}
	}
	return Theme{Name: name, Title: name, Tags: []string{}}, nil
}

func (s *Site) UpdateTheme(ctx context.Context, name string) (string, error) {
	if !installNameRe.MatchString(name) {
		return "", errf("invalid_request", "Invalid theme name")
	}
	dir := filepath.Join(s.ThemesDir(), name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return "", errf("theme_not_found", "Theme is not installed")
	}
	if info, err := os.Stat(filepath.Join(dir, ".git")); err != nil || !info.IsDir() {
		return "", errf("git_failed", "This theme is not a Git repository")
	}
	res := runCommand(ctx, dir, 2*time.Minute, []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false"}, "git", "pull", "--ff-only")
	if res.Err != nil {
		msg := res.Output
		if msg == "" {
			msg = res.Err.Error()
		}
		return msg, errf("git_failed", "Could not update theme: "+msg)
	}
	if res.Output == "" {
		res.Output = "Theme is already up to date."
	}
	return res.Output, nil
}

func (s *Site) RemoveTheme(name string) error {
	if !installNameRe.MatchString(name) {
		return errf("invalid_request", "Invalid theme name")
	}
	cfg, err := s.LoadConfig()
	if err != nil {
		return err
	}
	for _, active := range cfg.Themes {
		if active == name {
			return errf("theme_active", "Deactivate the theme before removing it")
		}
	}
	dir := filepath.Join(s.ThemesDir(), name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return errf("theme_not_found", "Theme is not installed")
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove theme: %w", err)
	}
	return nil
}

func safeHTTPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return raw
}

func validateGitURL(raw string) error {
	if strings.HasPrefix(raw, "git@") {
		if strings.ContainsAny(raw, " \r\n\t") {
			return errf("invalid_request", "Invalid repository URL")
		}
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errf("invalid_request", "Invalid repository URL")
	}
	switch u.Scheme {
	case "https", "http", "ssh", "git":
		return nil
	default:
		return errf("invalid_request", "Only http(s), ssh and git repositories are supported")
	}
}

func deriveRepoName(raw string) string {
	raw = strings.TrimSuffix(raw, "/")
	raw = strings.TrimSuffix(raw, ".git")
	if i := strings.LastIndexAny(raw, "/:"); i >= 0 {
		raw = raw[i+1:]
	}
	if !installNameRe.MatchString(raw) {
		return "theme"
	}
	return raw
}

func mustListThemes(s *Site) []Theme {
	themes, err := s.ListThemes()
	if err != nil {
		return nil
	}
	return themes
}
