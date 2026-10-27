package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupAdmin(t *testing.T, app *testApp) string {
	t.Helper()
	res, state := app.request("POST", "/api/setup", map[string]any{
		"username": "admin", "password": "password123", "email": "admin@example.com", "language": "en",
	}, nil)
	if res.StatusCode != 200 {
		t.Fatalf("setup status %d", res.StatusCode)
	}
	csrf, _ := state["csrf"].(string)
	if csrf == "" {
		t.Fatal("missing csrf token")
	}
	return csrf
}

func (a *testApp) upload(path string, csrf string, filename string, content []byte) (*http.Response, map[string]any) {
	a.t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		a.t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		a.t.Fatal(err)
	}
	writer.Close()
	req, err := http.NewRequest("POST", a.server.URL+path, &body)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-CSRF-Token", csrf)
	res, err := a.client.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	var decoded map[string]any
	_ = json.NewDecoder(res.Body).Decode(&decoded)
	res.Body.Close()
	return res, decoded
}

func TestMediaLocalLibraryFlow(t *testing.T) {
	app := newTestApp(t)
	csrf := setupAdmin(t, app)

	res, libs := app.request("GET", "/api/media/libraries", nil, nil)
	if res.StatusCode != 200 {
		t.Fatalf("libraries status %d", res.StatusCode)
	}
	list, _ := libs["libraries"].([]any)
	if len(list) != 1 {
		t.Fatalf("expected builtin library, got %#v", libs)
	}
	builtin, _ := list[0].(map[string]any)
	if builtin["id"] != "local" || builtin["builtin"] != true {
		t.Fatalf("builtin = %#v", builtin)
	}

	root := filepath.Join(t.TempDir(), "media")
	res, saved := app.request("POST", "/api/media/libraries", map[string]any{
		"name": "Archive", "type": "local", "root": root,
		"public_base": "https://cdn.example.com/archive", "max_upload_mb": 5,
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != 200 {
		t.Fatalf("save library: %d %#v", res.StatusCode, saved)
	}
	view, _ := saved["library"].(map[string]any)
	libID, _ := view["id"].(string)
	if libID == "" || view["type"] != "local" {
		t.Fatalf("saved view = %#v", view)
	}

	// Upload creates the folder and derives a public URL from the folder path.
	res, upload := app.upload("/api/media?library="+libID+"&path=posts/2026", csrf, "My Photo.PNG", []byte("png-bytes"))
	if res.StatusCode != 201 {
		t.Fatalf("upload: %d %#v", res.StatusCode, upload)
	}
	file, _ := upload["file"].(map[string]any)
	path, _ := file["path"].(string)
	if path != "posts/2026/My-Photo.png" {
		t.Fatalf("uploaded path = %q", path)
	}
	if file["url"] != "https://cdn.example.com/archive/posts/2026/My-Photo.png" {
		t.Fatalf("uploaded url = %q", file["url"])
	}

	res, listing := app.request("GET", "/api/media?library="+libID+"&path=posts/2026", nil, nil)
	if res.StatusCode != 200 {
		t.Fatalf("list: %d %#v", res.StatusCode, listing)
	}
	entries, _ := listing["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	stats, _ := listing["stats"].(map[string]any)
	if stats["files"].(float64) != 1 || stats["bytes"].(float64) != 9 {
		t.Fatalf("stats = %#v", stats)
	}

	// The proxy serves the bytes and honours Range for media players.
	res, _ = app.request("GET", "/api/media/file?library="+libID+"&path="+path, nil, nil)
	if res.StatusCode != 200 {
		t.Fatalf("file: %d", res.StatusCode)
	}
	if res.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatalf("media frame header = %q", res.Header.Get("X-Frame-Options"))
	}
	req, _ := http.NewRequest("GET", app.server.URL+"/api/media/file?library="+libID+"&path="+path, nil)
	req.Header.Set("Range", "bytes=2-4")
	rangeRes, err := app.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rangeBody, _ := io.ReadAll(rangeRes.Body)
	rangeRes.Body.Close()
	if rangeRes.StatusCode != http.StatusPartialContent || string(rangeBody) != "g-b" {
		t.Fatalf("range status %d body %q", rangeRes.StatusCode, rangeBody)
	}

	res, copied := app.request("POST", "/api/media/copy", map[string]any{
		"from_library": libID, "from": path, "to_library": libID, "to": "posts/2026/copy.png",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != 200 {
		t.Fatalf("copy: %d %#v", res.StatusCode, copied)
	}
	res, moved := app.request("POST", "/api/media/move", map[string]any{
		"from_library": libID, "from": "posts/2026/copy.png", "to_library": libID, "to": "moved.png",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != 200 {
		t.Fatalf("move: %d %#v", res.StatusCode, moved)
	}
	entry, _ := moved["entry"].(map[string]any)
	if entry["path"] != "moved.png" {
		t.Fatalf("moved entry = %#v", entry)
	}

	res, _ = app.request("POST", "/api/media/folder", map[string]any{"library": libID, "path": "empty/folder"}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != 201 {
		t.Fatalf("folder: %d", res.StatusCode)
	}

	if res, _ = app.request("DELETE", "/api/media?library="+libID+"&path=moved.png", nil, map[string]string{"X-CSRF-Token": csrf}); res.StatusCode != 200 {
		t.Fatalf("delete: %d", res.StatusCode)
	}

	// Traversal must be rejected before touching the filesystem.
	res, _ = app.request("GET", "/api/media/file?library="+libID+"&path=../panel.db", nil, nil)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("traversal status = %d", res.StatusCode)
	}

	// A local library must not expose the panel's own data directory.
	_, system := app.request("GET", "/api/system", nil, nil)
	dataDir, _ := system["data_dir"].(string)
	res, _ = app.request("POST", "/api/media/libraries", map[string]any{
		"name": "Unsafe", "type": "local", "root": filepath.Join(dataDir, "media"),
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("data dir library status = %d", res.StatusCode)
	}

	// Files that can execute script are never served inline, even when they
	// exist in a local folder.
	if err := os.WriteFile(filepath.Join(root, "evil.html"), []byte("<script>alert(1)</script>"), 0o644); err != nil {
		t.Fatal(err)
	}
	evil, err := app.client.Get(app.server.URL + "/api/media/file?library=" + libID + "&path=evil.html")
	if err != nil {
		t.Fatal(err)
	}
	evil.Body.Close()
	if evil.StatusCode != 200 || evil.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("html file: status %d type %q", evil.StatusCode, evil.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(evil.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("html disposition = %q", evil.Header.Get("Content-Disposition"))
	}
}

func TestMediaS3LibraryHidesSecret(t *testing.T) {
	app := newTestApp(t)
	csrf := setupAdmin(t, app)
	res, saved := app.request("POST", "/api/media/libraries", map[string]any{
		"name": "Object storage", "type": "s3", "endpoint": "https://s3.example.com",
		"region": "eu-central-1", "bucket": "media", "access_key": "AK",
		"secret_key": "SUPERSECRET", "prefix": "assets", "public_base": "https://cdn.example.com",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != 200 {
		t.Fatalf("save s3: %d %#v", res.StatusCode, saved)
	}
	view, _ := saved["library"].(map[string]any)
	if view["has_secret"] != true {
		t.Fatalf("has_secret = %#v", view["has_secret"])
	}
	blob, _ := json.Marshal(view)
	if strings.Contains(string(blob), "SUPERSECRET") {
		t.Fatal("secret leaked in API response")
	}
	if view["access_key"] != "AK" || view["prefix"] != "assets" {
		t.Fatalf("view = %#v", view)
	}
}

func TestMediaUploadRejectsUnsupportedType(t *testing.T) {
	app := newTestApp(t)
	csrf := setupAdmin(t, app)
	res, _ := app.upload("/api/media", csrf, "evil.svg", []byte("<svg/>"))
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected unsupported type, got %d", res.StatusCode)
	}
}
