package server

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testDeployKey = "-----BEGIN OPENSSH PRIVATE KEY-----\nsecret-test-key\n-----END OPENSSH PRIVATE KEY-----"
const testKnownHosts = "example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITest"

func deploySettings(site string) map[string]any {
	return map[string]any{
		"hugo_dir":               site,
		"deploy_enabled":         true,
		"deploy_host":            "example.com",
		"deploy_port":            2222,
		"deploy_user":            "deploy",
		"deploy_path":            "/var/www/example.com",
		"deploy_delete":          true,
		"deploy_strict_host_key": true,
		"deploy_ssh_key":         testDeployKey,
		"deploy_known_hosts":     testKnownHosts,
	}
}

func TestDeploySettingsRedactSecrets(t *testing.T) {
	app, csrf := setupApp(t)
	site := t.TempDir()
	res, view := app.request("PUT", "/api/settings", deploySettings(site), map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save deploy settings: %d %#v", res.StatusCode, view)
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "secret-test-key") || strings.Contains(string(encoded), "AAAAC3NzaC1lZDI1NTE5") {
		t.Fatal("settings response leaked deployment secrets")
	}
	deployView, _ := view["deploy"].(map[string]any)
	if deployView == nil {
		t.Fatalf("deploy view missing: %#v", view)
	}
	if deployView["ssh_key_set"] != true || deployView["known_hosts_set"] != true || deployView["configured"] != true {
		t.Fatalf("unexpected deploy view: %#v", deployView)
	}
	if deployView["ssh_key_preview"] != "-----BEGIN" {
		t.Fatalf("ssh_key_preview = %#v, want first 10 characters", deployView["ssh_key_preview"])
	}
	addedAt, _ := deployView["ssh_key_added_at"].(string)
	if _, err := time.Parse(time.RFC3339, addedAt); err != nil {
		t.Fatalf("ssh_key_added_at = %q, want an RFC3339 timestamp", addedAt)
	}
	if deployView["port"] != float64(2222) || deployView["host"] != "example.com" {
		t.Fatalf("unexpected deploy target: %#v", deployView)
	}

	res, fetched := app.request("GET", "/api/settings", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get settings: %d", res.StatusCode)
	}
	encoded, _ = json.Marshal(fetched)
	if strings.Contains(string(encoded), "secret-test-key") || strings.Contains(string(encoded), "AAAAC3NzaC1lZDI1NTE5") {
		t.Fatal("GET /api/settings leaked deployment secrets")
	}
}

func TestDeploySettingsClearSSHKey(t *testing.T) {
	app, csrf := setupApp(t)
	site := t.TempDir()
	res, view := app.request("PUT", "/api/settings", deploySettings(site), map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save deploy settings: %d %#v", res.StatusCode, view)
	}

	res, view = app.request("PUT", "/api/settings", map[string]any{"clear_deploy_ssh_key": true}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("clear ssh key: %d %#v", res.StatusCode, view)
	}
	deployView, _ := view["deploy"].(map[string]any)
	if deployView == nil {
		t.Fatalf("deploy view missing: %#v", view)
	}
	if deployView["ssh_key_set"] != false || deployView["ssh_key_preview"] != "" || deployView["ssh_key_added_at"] != "" {
		t.Fatalf("saved key metadata was not cleared: %#v", deployView)
	}
}

func TestDeploySettingsValidation(t *testing.T) {
	app, csrf := setupApp(t)
	site := t.TempDir()
	for _, body := range []map[string]any{
		{"hugo_dir": site, "deploy_host": "bad host"},
		{"deploy_port": 0},
		{"deploy_port": 70000},
		{"deploy_user": "bad user"},
		{"deploy_path": "bad\npath"},
		{"deploy_source": "../outside"},
		{"deploy_ssh_key": "not-a-private-key"},
		{"deploy_rsync_path": "/usr/bin/rsync --fake"},
		{"deploy_enabled": true},
	} {
		res, view := app.request("PUT", "/api/settings", body, map[string]string{"X-CSRF-Token": csrf})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected %#v to be rejected, got %d %#v", body, res.StatusCode, view)
		}
	}
}

func TestDeployRunWithFakeRsync(t *testing.T) {
	binDir := t.TempDir()
	script := "#!/bin/sh\necho 'deployed via fake rsync'\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "rsync"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	app, csrf := setupApp(t)
	site := t.TempDir()
	if err := os.MkdirAll(filepath.Join(site, "public"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(site, "public", "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, view := app.request("PUT", "/api/settings", deploySettings(site), map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save deploy settings: %d %#v", res.StatusCode, view)
	}

	res, result := app.request("POST", "/api/deploy", map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("deploy status %d: %#v", res.StatusCode, result)
	}
	if result["success"] != true || result["exit_code"] != float64(0) {
		t.Fatalf("unexpected deploy result: %#v", result)
	}
	if !strings.Contains(result["output"].(string), "deployed via fake rsync") {
		t.Fatalf("unexpected deploy output: %#v", result["output"])
	}

	res, fetched := app.request("GET", "/api/deploy", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get deploy: %d", res.StatusCode)
	}
	last, _ := fetched["deploy"].(map[string]any)
	if last == nil || last["success"] != true {
		t.Fatalf("last deploy not persisted: %#v", fetched)
	}
	if fetched["enabled"] != true || fetched["configured"] != true {
		t.Fatalf("unexpected deploy status: %#v", fetched)
	}
}
