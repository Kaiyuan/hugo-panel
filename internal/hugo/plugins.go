package hugo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type ModuleImport struct {
	Path          string `json:"path"`
	Disable       bool   `json:"disable,omitempty"`
	IgnoreConfig  bool   `json:"ignore_config,omitempty"`
	IgnoreImports bool   `json:"ignore_imports,omitempty"`
	NoVendor      bool   `json:"no_vendor,omitempty"`
}

type PluginState struct {
	Modules       []ModuleImport `json:"modules"`
	HugoAvailable bool           `json:"hugo_available"`
	GoAvailable   bool           `json:"go_available"`
	GitAvailable  bool           `json:"git_available"`
	HugoVersion   string         `json:"hugo_version,omitempty"`
	ConfigPath    string         `json:"config_path"`
	Initialized   bool           `json:"initialized"`
}

type PluginResult struct {
	PluginState
	Output  string `json:"output,omitempty"`
	Warning string `json:"warning,omitempty"`
}

var modulePathRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]{0,255}$`)
var moduleVersionRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+~/-]{0,127}$`)

func moduleImports(m map[string]any) []ModuleImport {
	out := []ModuleImport{}
	mod, ok := m["module"].(map[string]any)
	if !ok {
		return out
	}
	imports := AnySlice(mod["imports"])
	for _, raw := range imports {
		switch entry := raw.(type) {
		case string:
			out = append(out, ModuleImport{Path: entry})
		case map[string]any:
			out = append(out, ModuleImport{
				Path:          StringValue(entry["path"]),
				Disable:       BoolValue(entry["disable"]),
				IgnoreConfig:  BoolValue(entry["ignoreConfig"]),
				IgnoreImports: BoolValue(entry["ignoreImports"]),
				NoVendor:      BoolValue(entry["noVendor"]),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (s *Site) PluginState(ctx context.Context) (PluginState, error) {
	rel, _, m, exists, err := s.loadConfigMap()
	if err != nil {
		return PluginState{}, err
	}
	state := PluginState{
		Modules:     moduleImports(m),
		ConfigPath:  filepath.ToSlash(rel),
		Initialized: exists,
	}
	if _, err := os.Stat(s.Binary); err == nil {
		state.HugoAvailable = true
	} else if p, err := exec.LookPath(s.Binary); err == nil && p != "" {
		state.HugoAvailable = true
	}
	if _, err := exec.LookPath("go"); err == nil {
		state.GoAvailable = true
	}
	if _, err := exec.LookPath("git"); err == nil {
		state.GitAvailable = true
	}
	if state.HugoAvailable {
		dir := s.Dir
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			dir = ""
		}
		res := runCommand(ctx, dir, 15*time.Second, nil, s.Binary, "version")
		if res.Err == nil {
			state.HugoVersion = firstLine(res.Output)
		}
	}
	return state, nil
}

func (s *Site) AddPlugin(ctx context.Context, path, version string) (PluginResult, error) {
	path = strings.TrimSpace(path)
	version = strings.TrimSpace(version)
	if !validModulePath(path) {
		return PluginResult{}, errf("invalid_request", "Invalid Hugo module path")
	}
	if version != "" && !moduleVersionRe.MatchString(version) {
		return PluginResult{}, errf("invalid_request", "Invalid module version")
	}
	rel, format, m, exists, err := s.loadConfigMap()
	if err != nil {
		return PluginResult{}, err
	}
	if !exists {
		return PluginResult{}, errf("not_initialized", "Hugo site is not initialized yet")
	}
	for _, mod := range moduleImports(m) {
		if mod.Path == path {
			return PluginResult{}, errf("plugin_exists", "This module is already installed")
		}
	}
	mod, _ := m["module"].(map[string]any)
	if mod == nil {
		mod = map[string]any{}
	}
	imports := AnySlice(mod["imports"])
	imports = append(imports, map[string]any{"path": path})
	mod["imports"] = imports
	m["module"] = mod
	if err := s.saveConfigMap(rel, format, m); err != nil {
		return PluginResult{}, err
	}

	var warning, output string
	if version != "" {
		res := runCommand(ctx, s.Dir, 5*time.Minute, moduleEnv(), s.Binary, "mod", "get", path+"@"+version)
		output = res.Output
		if res.Err != nil {
			warning = commandWarning("hugo mod get", res)
		}
	}
	res := runCommand(ctx, s.Dir, 5*time.Minute, moduleEnv(), s.Binary, "mod", "tidy")
	if res.Output != "" {
		if output != "" {
			output += "\n"
		}
		output += res.Output
	}
	if res.Err != nil {
		warning = commandWarning("hugo mod tidy", res)
	}
	state, err := s.PluginState(ctx)
	if err != nil {
		return PluginResult{}, err
	}
	return PluginResult{PluginState: state, Output: output, Warning: warning}, nil
}

func (s *Site) RemovePlugin(ctx context.Context, path string) (PluginResult, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return PluginResult{}, errf("invalid_request", "Module path is required")
	}
	rel, format, m, exists, err := s.loadConfigMap()
	if err != nil {
		return PluginResult{}, err
	}
	if !exists {
		return PluginResult{}, errf("not_initialized", "Hugo site is not initialized yet")
	}
	mod, _ := m["module"].(map[string]any)
	if mod == nil {
		return PluginResult{}, errf("not_found", "Module is not installed")
	}
	imports := AnySlice(mod["imports"])
	var kept []any
	removed := false
	for _, raw := range imports {
		p := ""
		switch entry := raw.(type) {
		case string:
			p = entry
		case map[string]any:
			p = StringValue(entry["path"])
		}
		if p == path {
			removed = true
			continue
		}
		kept = append(kept, raw)
	}
	if !removed {
		return PluginResult{}, errf("not_found", "Module is not installed")
	}
	mod["imports"] = kept
	m["module"] = mod
	if err := s.saveConfigMap(rel, format, m); err != nil {
		return PluginResult{}, err
	}
	res := runCommand(ctx, s.Dir, 5*time.Minute, moduleEnv(), s.Binary, "mod", "tidy")
	warning := ""
	if res.Err != nil {
		warning = commandWarning("hugo mod tidy", res)
	}
	state, err := s.PluginState(ctx)
	if err != nil {
		return PluginResult{}, err
	}
	return PluginResult{PluginState: state, Output: res.Output, Warning: warning}, nil
}

func (s *Site) TidyPlugins(ctx context.Context) (PluginResult, error) {
	if !s.ConfigExists() {
		return PluginResult{}, errf("not_initialized", "Hugo site is not initialized yet")
	}
	res := runCommand(ctx, s.Dir, 5*time.Minute, moduleEnv(), s.Binary, "mod", "tidy")
	warning := ""
	if res.Err != nil {
		warning = commandWarning("hugo mod tidy", res)
	}
	state, err := s.PluginState(ctx)
	if err != nil {
		return PluginResult{}, err
	}
	return PluginResult{PluginState: state, Output: res.Output, Warning: warning}, nil
}

func (s *Site) saveConfigMap(rel, format string, m map[string]any) error {
	data, err := encodeConfig(m, format)
	if err != nil {
		return err
	}
	full := filepath.Join(s.Dir, rel)
	if old, err := os.ReadFile(full); err == nil {
		s.backup(rel, old)
	}
	return atomicWrite(full, data, 0o644)
}

func validModulePath(path string) bool {
	if !modulePathRe.MatchString(path) {
		return false
	}
	if strings.HasPrefix(path, "-") || strings.Contains(path, "..") || strings.Contains(path, "//") {
		return false
	}
	return true
}

func moduleEnv() []string {
	return []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=/bin/false", "GOFLAGS=-mod=mod"}
}

func commandWarning(name string, res cmdResult) string {
	msg := res.Output
	if msg == "" && res.Err != nil {
		msg = res.Err.Error()
	}
	if len(msg) > 1200 {
		msg = msg[len(msg)-1200:]
	}
	return fmt.Sprintf("%s failed: %s", name, strings.TrimSpace(msg))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
