package hugo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Compatibility is the result of a theme or module compatibility check.
// Status is one of "compatible", "incompatible" or "unknown". Reason and
// Output are advisory details for the UI; Compatible is the decision used by
// the upgrade flow.
type Compatibility struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	HugoVersion string `json:"hugo_version"`
	MinVersion  string `json:"min_version,omitempty"`
	Compatible  bool   `json:"compatible"`
	Status      string `json:"status"`
	Code        string `json:"code,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Output      string `json:"output,omitempty"`
	CheckedAt   string `json:"checked_at"`
}

// UpgradeCompatibility aggregates the active theme and module checks that run
// before a Hugo binary is switched.
type UpgradeCompatibility struct {
	HugoVersion string          `json:"hugo_version"`
	Compatible  bool            `json:"compatible"`
	Status      string          `json:"status"`
	Reason      string          `json:"reason,omitempty"`
	Output      string          `json:"output,omitempty"`
	Themes      []Compatibility `json:"themes"`
	Plugins     Compatibility   `json:"plugins"`
}

func compatibilityNow() string { return time.Now().UTC().Format(time.RFC3339) }

func limitCompatibilityOutput(raw string) string {
	raw = strings.TrimSpace(raw)
	runes := []rune(raw)
	if len(runes) > 8000 {
		raw = string(runes[len(runes)-8000:])
	}
	return raw
}

// binaryVersion runs an arbitrary Hugo binary and returns its normalized
// version. It is used to test a newly downloaded binary before switching.
func binaryVersion(ctx context.Context, binary string) (string, error) {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		return "", errf("hugo_not_found", "Hugo binary is not configured")
	}
	if _, err := exec.LookPath(binary); err != nil {
		if info, statErr := os.Stat(binary); statErr != nil || info.IsDir() {
			return "", errf("hugo_not_found", "Hugo binary not found: "+binary)
		}
	}
	res := runCommand(ctx, "", 30*time.Second, nil, binary, "version")
	if res.Err != nil {
		msg := strings.TrimSpace(res.Output)
		if msg == "" {
			msg = res.Err.Error()
		}
		return "", errf("hugo_not_found", "Could not run the Hugo binary: "+msg)
	}
	version, ok := ParseHugoVersion(res.Output)
	if !ok {
		return "", errf("hugo_not_found", "Could not detect the Hugo version from: "+firstLine(res.Output))
	}
	return version, nil
}

// ThemeMinVersion reads min_version (or minVersion) from the theme metadata.
func (s *Site) ThemeMinVersion(name string) string {
	if !installNameRe.MatchString(name) {
		return ""
	}
	dir := filepath.Join(s.ThemesDir(), name)
	for _, metaName := range []string{"theme.toml", "theme.yaml", "theme.yml", "theme.json"} {
		data, err := os.ReadFile(filepath.Join(dir, metaName))
		if err != nil {
			continue
		}
		m, err := decodeConfig(data, formatFromExt(metaName))
		if err != nil {
			continue
		}
		if v := StringValue(m["min_version"]); v != "" {
			return v
		}
		if v := StringValue(m["minVersion"]); v != "" {
			return v
		}
		return ""
	}
	return ""
}

// CheckThemeCompatibility verifies a theme against the configured Hugo binary.
// It first compares the declared min_version, then runs a real build with the
// theme selected so template/API incompatibilities are surfaced too.
func (s *Site) CheckThemeCompatibility(ctx context.Context, name string) (Compatibility, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "..") || !installNameRe.MatchString(name) {
		return Compatibility{}, errf("invalid_request", "Invalid theme name")
	}
	if info, err := os.Stat(filepath.Join(s.ThemesDir(), name)); err != nil || !info.IsDir() {
		return Compatibility{}, errf("theme_not_found", "Theme is not installed")
	}
	return s.checkThemeCompatibility(ctx, s.Binary, name), nil
}

func (s *Site) checkThemeCompatibility(ctx context.Context, binary, name string) Compatibility {
	result := Compatibility{
		Kind:      "theme",
		Name:      name,
		Status:    "unknown",
		CheckedAt: compatibilityNow(),
	}
	version, err := binaryVersion(ctx, binary)
	if err != nil {
		result.Code = "hugo_missing"
		result.Reason = err.Error()
		return result
	}
	result.HugoVersion = version
	result.MinVersion = s.ThemeMinVersion(name)
	if result.MinVersion != "" && !HugoVersionAtLeast(version, result.MinVersion) {
		result.Code = "min_version"
		result.Status = "incompatible"
		result.Reason = fmt.Sprintf("This theme requires Hugo %s or newer, but %s is installed.", result.MinVersion, version)
		return result
	}
	if !s.ConfigExists() {
		result.Code = "not_initialized"
		result.Reason = "Hugo site is not initialized yet."
		return result
	}
	res := runCommand(ctx, s.Dir, 5*time.Minute, append([]string{"HUGO_ENV=production"}, moduleEnv()...), binary, "--theme", name, "--renderToMemory")
	result.Output = limitCompatibilityOutput(res.Output)
	if res.Err != nil {
		result.Code = "build_failed"
		result.Status = "incompatible"
		result.Reason = "Hugo could not build the site with this theme."
		return result
	}
	result.Code = "ok"
	result.Status = "compatible"
	result.Compatible = true
	result.Reason = fmt.Sprintf("The theme built successfully with Hugo %s.", version)
	return result
}

// CheckPluginsCompatibility verifies that the configured Hugo modules can be
// resolved and used by the current Hugo binary. A real build is the most
// reliable check because modules can contribute templates, shortcodes and
// assets that Hugo only validates while rendering.
func (s *Site) CheckPluginsCompatibility(ctx context.Context) (Compatibility, error) {
	return s.checkPluginsCompatibility(ctx, s.Binary), nil
}

func (s *Site) checkPluginsCompatibility(ctx context.Context, binary string) Compatibility {
	result := Compatibility{
		Kind:      "plugins",
		Name:      "modules",
		Status:    "unknown",
		CheckedAt: compatibilityNow(),
	}
	version, err := binaryVersion(ctx, binary)
	if err != nil {
		result.Code = "hugo_missing"
		result.Reason = err.Error()
		return result
	}
	result.HugoVersion = version
	cfg, err := s.LoadConfig()
	if err != nil {
		result.Code = "config_error"
		result.Reason = err.Error()
		return result
	}
	if !cfg.Exists {
		result.Code = "not_initialized"
		result.Reason = "Hugo site is not initialized yet."
		return result
	}
	if len(cfg.Modules) == 0 {
		result.Code = "no_modules"
		result.Status = "compatible"
		result.Compatible = true
		result.Reason = "No Hugo modules are installed."
		return result
	}
	res := runCommand(ctx, s.Dir, 5*time.Minute, moduleEnv(), binary, "--renderToMemory")
	result.Output = limitCompatibilityOutput(res.Output)
	if res.Err != nil {
		result.Code = "build_failed"
		result.Status = "incompatible"
		result.Reason = "Hugo could not build the site with the installed modules."
		return result
	}
	result.Code = "ok"
	result.Status = "compatible"
	result.Compatible = true
	result.Reason = fmt.Sprintf("The installed modules built successfully with Hugo %s.", version)
	return result
}

// CheckUpgradeCompatibility tests a new Hugo binary against the active themes
// and modules before the panel switches to it. It performs one build for all
// active themes/modules after checking each declared min_version.
func (s *Site) CheckUpgradeCompatibility(ctx context.Context, binary string) (UpgradeCompatibility, error) {
	version, err := binaryVersion(ctx, binary)
	if err != nil {
		return UpgradeCompatibility{}, err
	}
	out := UpgradeCompatibility{
		HugoVersion: version,
		Compatible:  true,
		Status:      "compatible",
		Themes:      []Compatibility{},
		Plugins: Compatibility{
			Kind:        "plugins",
			Name:        "modules",
			HugoVersion: version,
			Compatible:  true,
			Status:      "compatible",
			CheckedAt:   compatibilityNow(),
		},
	}
	cfg, err := s.LoadConfig()
	if err != nil {
		return UpgradeCompatibility{}, err
	}
	if !cfg.Exists {
		out.Status = "unknown"
		out.Reason = "Hugo site is not initialized yet; there are no themes or modules to test."
		return out, nil
	}
	for _, name := range cfg.Themes {
		item := Compatibility{
			Kind:        "theme",
			Name:        name,
			HugoVersion: version,
			MinVersion:  s.ThemeMinVersion(name),
			Compatible:  true,
			Status:      "compatible",
			Code:        "ok",
			CheckedAt:   compatibilityNow(),
		}
		if item.MinVersion != "" && !HugoVersionAtLeast(version, item.MinVersion) {
			item.Code = "min_version"
			item.Status = "incompatible"
			item.Compatible = false
			item.Reason = fmt.Sprintf("This theme requires Hugo %s or newer, but %s would be installed.", item.MinVersion, version)
			out.Compatible = false
			out.Status = "incompatible"
			out.Reason = item.Reason
		}
		out.Themes = append(out.Themes, item)
	}
	if len(cfg.Modules) == 0 {
		out.Plugins.Code = "no_modules"
		out.Plugins.Reason = "No Hugo modules are installed."
	} else {
		out.Plugins.Code = "ok"
		out.Plugins.Reason = fmt.Sprintf("The installed modules are expected to work with Hugo %s.", version)
	}
	if len(cfg.Themes) == 0 && len(cfg.Modules) == 0 {
		out.Reason = "No active themes or modules need to be checked."
		return out, nil
	}
	res := runCommand(ctx, s.Dir, 5*time.Minute, moduleEnv(), binary, "--renderToMemory")
	out.Output = limitCompatibilityOutput(res.Output)
	if res.Err != nil {
		out.Compatible = false
		out.Status = "incompatible"
		out.Reason = "Hugo could not build the site with the active themes and modules."
		if len(cfg.Modules) > 0 {
			out.Plugins.Code = "build_failed"
			out.Plugins.Status = "incompatible"
			out.Plugins.Compatible = false
			out.Plugins.Reason = "The installed modules could not be used by the new Hugo binary."
		}
		return out, nil
	}
	if out.Compatible {
		out.Reason = fmt.Sprintf("Active themes and modules build successfully with Hugo %s.", version)
	}
	return out, nil
}
