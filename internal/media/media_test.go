package media

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type memStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemStore() *memStore { return &memStore{data: map[string]string{}} }

func (m *memStore) GetSetting(ctx context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[key]
	return v, ok, nil
}

func (m *memStore) SetSetting(ctx context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func TestLocalBackendLifecycle(t *testing.T) {
	root := t.TempDir()
	cfg := Config{ID: "local-test", Name: "Local test", Type: "local", Root: root, PublicBase: "https://cdn.example.com/media", MaxUploadMB: 10}
	if err := normalizeConfig(&cfg); err != nil {
		t.Fatal(err)
	}
	b, err := newLocal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := b.Mkdir(ctx, "posts/2026"); err != nil {
		t.Fatal(err)
	}
	body := []byte("hello media")
	if err := b.Write(ctx, "posts/2026/hello.txt", bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatal(err)
	}
	entries, err := b.List(ctx, "posts/2026")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "hello.txt" || entries[0].Kind != "text" {
		t.Fatalf("unexpected entries: %#v", entries)
	}
	if got := b.URL("posts/2026/hello.txt"); got != "https://cdn.example.com/media/posts/2026/hello.txt" {
		t.Fatalf("url = %q", got)
	}
	stat, err := b.Stat(ctx, "posts/2026/hello.txt")
	if err != nil || stat.Size != int64(len(body)) {
		t.Fatalf("stat: %#v %v", stat, err)
	}
	rc, err := b.Read(ctx, "posts/2026/hello.txt", 6, 5)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "media" {
		t.Fatalf("range = %q", got)
	}
	if err := b.Copy(ctx, "posts/2026/hello.txt", "posts/copy.txt"); err != nil {
		t.Fatal(err)
	}
	if err := b.Move(ctx, "posts/copy.txt", "posts/moved.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stat(ctx, "posts/moved.txt"); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, "posts/moved.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stat(ctx, "posts/moved.txt"); err == nil {
		t.Fatal("expected deleted file to be gone")
	}
	if err := b.Test(ctx); err != nil {
		t.Fatal(err)
	}
	// Path traversal is rejected.
	if _, err := b.resolve("../etc/passwd"); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestLocalBackendRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	b, err := newLocal(Config{ID: "local", Name: "Local", Type: "local", Root: root, MaxUploadMB: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Read(context.Background(), "link/secret.txt", 0, -1); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestManagerSaveListAndSecretMasking(t *testing.T) {
	st := newMemStore()
	hugoDir := t.TempDir()
	m := NewManager(st, func(context.Context) string { return hugoDir })
	ctx := context.Background()

	list, err := m.List(ctx)
	if err != nil || len(list) != 1 || !list[0].Builtin || list[0].ID != DefaultID {
		t.Fatalf("builtin list: %#v %v", list, err)
	}
	if list[0].Root != filepath.Join(hugoDir, "static", "images") {
		t.Fatalf("builtin root = %q", list[0].Root)
	}

	saved, err := m.Save(ctx, Config{
		Name: "CDN", Type: "s3", Endpoint: "https://s3.example.com", Bucket: "media",
		AccessKey: "AK", SecretKey: "SECRET", Region: "eu-central-1", Prefix: "assets/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != "cdn" || saved.Prefix != "assets" {
		t.Fatalf("saved: %#v", saved)
	}
	view := saved.View()
	if view.HasSecret != true || view.AccessKey != "AK" {
		t.Fatalf("view: %#v", view)
	}
	if strings.Contains(fmt.Sprintf("%#v", view), "SECRET") {
		t.Fatal("view leaked secret")
	}

	// Updating without a secret keeps the stored one.
	updated, err := m.Save(ctx, Config{ID: saved.ID, Name: "CDN 2", Type: "s3", Endpoint: "https://s3.example.com", Bucket: "media2", AccessKey: "AK", Region: "eu-central-1"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.SecretKey != "SECRET" {
		t.Fatalf("secret not preserved: %#v", updated)
	}
	got, err := m.Get(ctx, saved.ID)
	if err != nil || got.SecretKey != "SECRET" {
		t.Fatalf("stored secret: %#v %v", got, err)
	}
	if err := m.Delete(ctx, saved.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, saved.ID); err == nil {
		t.Fatal("expected library to be deleted")
	}
}

func TestConfigValidation(t *testing.T) {
	base := Config{Name: "x", Type: "s3", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "a", SecretKey: "s"}
	cases := []struct {
		name string
		cfg  Config
	}{
		{"no name", Config{Type: "s3"}},
		{"bad type", Config{Name: "x", Type: "ftp"}},
		{"relative local", Config{Name: "x", Type: "local", Root: "relative"}},
		{"bad endpoint", Config{Name: "x", Type: "s3", Endpoint: "nope", Bucket: "b", AccessKey: "a", SecretKey: "s"}},
		{"no bucket", Config{Name: "x", Type: "s3", Endpoint: "https://s3.example.com", AccessKey: "a", SecretKey: "s"}},
		{"no secret", Config{Name: "x", Type: "s3", Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "a"}},
		{"bad webdav", Config{Name: "x", Type: "webdav", BaseURL: "ftp://x"}},
		{"bad public base", Config{Name: "x", Type: "local", Root: "/tmp", PublicBase: "javascript:alert(1)"}},
	}
	for _, tc := range cases {
		cfg := tc.cfg
		if err := normalizeConfig(&cfg); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}
	if err := normalizeConfig(&base); err != nil {
		t.Fatal(err)
	}
}

func TestS3SigningMatchesAWSExample(t *testing.T) {
	b := &s3Backend{cfg: Config{
		Region: "us-east-1", AccessKey: "AKIAIOSFODNN7EXAMPLE",
		SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}}
	req, err := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "examplebucket.s3.amazonaws.com"
	req.Header.Set("Range", "bytes=0-9")
	b.sign(req, "/test.txt", "", emptySHA256, time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("authorization mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestS3URLGeneration(t *testing.T) {
	ctxBackend, err := newS3(Config{
		ID: "s3", Name: "s3", Type: "s3", Endpoint: "https://minio.local:9000",
		Bucket: "media", Prefix: "assets", PathStyle: true, AccessKey: "a", SecretKey: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ctxBackend.URL("posts/a b.png"), "https://minio.local:9000/media/assets/posts/a%20b.png"; got != want {
		t.Fatalf("path-style url = %q, want %q", got, want)
	}
	virtual, err := newS3(Config{
		ID: "s3", Name: "s3", Type: "s3", Endpoint: "https://s3.eu-central-1.amazonaws.com",
		Bucket: "media", Prefix: "assets", AccessKey: "a", SecretKey: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := virtual.URL("x/y.png"), "https://media.s3.eu-central-1.amazonaws.com/assets/x/y.png"; got != want {
		t.Fatalf("virtual url = %q, want %q", got, want)
	}
	cdn, err := newS3(Config{
		ID: "s3", Name: "s3", Type: "s3", Endpoint: "https://s3.example.com",
		Bucket: "media", Prefix: "assets", PublicBase: "https://cdn.example.com", AccessKey: "a", SecretKey: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cdn.URL("x/y.png"), "https://cdn.example.com/x/y.png"; got != want {
		t.Fatalf("cdn url = %q, want %q", got, want)
	}
}

// fakeS3 implements just enough of the S3 API for the backend tests.
type fakeS3 struct {
	mu      sync.Mutex
	objects map[string][]byte
	types   map[string]string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
		http.Error(w, "missing signature", http.StatusForbidden)
		return
	}
	if r.Header.Get("x-amz-content-sha256") == "" || r.Header.Get("x-amz-date") == "" {
		http.Error(w, "missing amz headers", http.StatusBadRequest)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/bucket/")
	key = strings.TrimPrefix(key, "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("list-type") == "2" {
			f.list(w, r)
			return
		}
		body, ok := f.objects[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", f.types[key])
		serveRange(w, r, body)
	case http.MethodHead:
		body, ok := f.objects[key]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", f.types[key])
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Last-Modified", "Mon, 02 Jan 2006 15:04:05 GMT")
	case http.MethodPut:
		if src := r.Header.Get("x-amz-copy-source"); src != "" {
			srcKey := strings.TrimPrefix(src, "/bucket/")
			body, ok := f.objects[srcKey]
			if !ok {
				http.NotFound(w, r)
				return
			}
			f.objects[key] = append([]byte(nil), body...)
			f.types[key] = f.types[srcKey]
			io.WriteString(w, `<CopyObjectResult></CopyObjectResult>`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.objects[key] = body
		f.types[key] = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "unsupported", http.StatusMethodNotAllowed)
	}
}

func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	var res s3ListResult
	seenDirs := map[string]bool{}
	for key := range f.objects {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		rel := strings.TrimPrefix(key, prefix)
		if rel == "" {
			continue
		}
		if delimiter == "/" && strings.Contains(rel, "/") {
			dir := prefix + strings.SplitN(rel, "/", 2)[0] + "/"
			if !seenDirs[dir] {
				seenDirs[dir] = true
				res.CommonPrefixes = append(res.CommonPrefixes, struct {
					Prefix string `xml:"Prefix"`
				}{Prefix: dir})
			}
			continue
		}
		res.Contents = append(res.Contents, struct {
			Key          string `xml:"Key"`
			LastModified string `xml:"LastModified"`
			Size         int64  `xml:"Size"`
		}{Key: key, LastModified: "2006-01-02T15:04:05Z", Size: int64(len(f.objects[key]))})
	}
	w.Header().Set("Content-Type", "application/xml")
	xml.NewEncoder(w).Encode(res)
}

func serveRange(w http.ResponseWriter, r *http.Request, body []byte) {
	rangeHeader := r.Header.Get("Range")
	if rangeHeader == "" {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
		return
	}
	var start, end int
	if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end); err != nil {
		if _, err := fmt.Sscanf(rangeHeader, "bytes=%d-", &start); err != nil {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		end = len(body) - 1
	}
	if start < 0 || start >= len(body) || end >= len(body) || end < start {
		http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
	w.WriteHeader(http.StatusPartialContent)
	w.Write(body[start : end+1])
}

func TestS3BackendLifecycle(t *testing.T) {
	fake := &fakeS3{objects: map[string][]byte{}, types: map[string]string{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b, err := newS3(Config{
		ID: "s3", Name: "s3", Type: "s3", Endpoint: srv.URL,
		Bucket: "bucket", Prefix: "media", PathStyle: true, AccessKey: "AK", SecretKey: "SK",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := b.Test(ctx); err != nil {
		t.Fatal(err)
	}
	body := []byte("0123456789")
	if err := b.Write(ctx, "posts/a.txt", bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatal(err)
	}
	entries, err := b.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || !entries[0].IsDir || entries[0].Name != "posts" {
		t.Fatalf("root list: %#v", entries)
	}
	entries, err = b.List(ctx, "posts")
	if err != nil || len(entries) != 1 || entries[0].Name != "a.txt" {
		t.Fatalf("posts list: %#v %v", entries, err)
	}
	rc, err := b.Read(ctx, "posts/a.txt", 2, 4)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "2345" {
		t.Fatalf("range = %q", got)
	}
	if err := b.Copy(ctx, "posts/a.txt", "posts/b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := b.Move(ctx, "posts/b.txt", "posts/c.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stat(ctx, "posts/c.txt"); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, "posts/c.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stat(ctx, "posts/c.txt"); err == nil {
		t.Fatal("expected deleted object to be gone")
	}
	if err := b.Mkdir(ctx, "new/folder"); err != nil {
		t.Fatal(err)
	}
	entries, err = b.List(ctx, "new")
	if err != nil || len(entries) != 1 || !entries[0].IsDir || entries[0].Name != "folder" {
		t.Fatalf("mkdir list: %#v %v", entries, err)
	}
}

// fakeDAV implements the WebDAV methods used by the backend.
type fakeDAV struct {
	mu    sync.Mutex
	files map[string][]byte
	dirs  map[string]bool
}

func newFakeDAV() *fakeDAV {
	return &fakeDAV{files: map[string][]byte{}, dirs: map[string]bool{"/dav": true}}
}

func (f *fakeDAV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := strings.TrimSuffix(r.URL.Path, "/")
	if p == "" {
		p = "/"
	}
	switch r.Method {
	case "PROPFIND":
		f.propfind(w, r, p)
	case http.MethodHead, http.MethodGet:
		body, ok := f.files[p]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		serveRange(w, r, body)
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		f.files[p] = body
		if parent := pathDir(p); parent != "" {
			f.dirs[parent] = true
		}
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		if _, ok := f.files[p]; !ok {
			http.NotFound(w, r)
			return
		}
		delete(f.files, p)
		w.WriteHeader(http.StatusNoContent)
	case "MKCOL":
		if f.dirs[p] {
			http.Error(w, "exists", http.StatusMethodNotAllowed)
			return
		}
		f.dirs[p] = true
		w.WriteHeader(http.StatusCreated)
	case "COPY", "MOVE":
		dest := r.Header.Get("Destination")
		u, err := url.Parse(dest)
		if err != nil {
			http.Error(w, "bad destination", http.StatusBadRequest)
			return
		}
		dp := strings.TrimSuffix(u.Path, "/")
		body, ok := f.files[p]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if _, exists := f.files[dp]; exists && r.Header.Get("Overwrite") == "F" {
			http.Error(w, "exists", http.StatusPreconditionFailed)
			return
		}
		f.files[dp] = append([]byte(nil), body...)
		if r.Method == "MOVE" {
			delete(f.files, p)
		}
		w.WriteHeader(http.StatusCreated)
	default:
		http.Error(w, "unsupported", http.StatusMethodNotAllowed)
	}
}

func pathDir(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx <= 0 {
		return "/"
	}
	return p[:idx]
}

type davTestMultistatus struct {
	XMLName   xml.Name `xml:"d:multistatus"`
	XmlnsD    string   `xml:"xmlns:d,attr"`
	Responses []struct {
		Href     string `xml:"d:href"`
		Propstat struct {
			Prop struct {
				ResourceType struct {
					Collection *struct{} `xml:"d:collection"`
				} `xml:"d:resourcetype"`
				ContentLength string `xml:"d:getcontentlength"`
				ContentType   string `xml:"d:getcontenttype"`
				LastModified  string `xml:"d:getlastmodified"`
			} `xml:"d:prop"`
		} `xml:"d:propstat"`
	} `xml:"d:response"`
}

func (f *fakeDAV) propfind(w http.ResponseWriter, r *http.Request, p string) {
	ms := davTestMultistatus{XmlnsD: "DAV:"}
	add := func(href string, isDir bool, body []byte) {
		var item struct {
			Href     string `xml:"d:href"`
			Propstat struct {
				Prop struct {
					ResourceType struct {
						Collection *struct{} `xml:"d:collection"`
					} `xml:"d:resourcetype"`
					ContentLength string `xml:"d:getcontentlength"`
					ContentType   string `xml:"d:getcontenttype"`
					LastModified  string `xml:"d:getlastmodified"`
				} `xml:"d:prop"`
			} `xml:"d:propstat"`
		}
		item.Href = href
		if isDir {
			item.Propstat.Prop.ResourceType.Collection = &struct{}{}
		} else {
			item.Propstat.Prop.ContentLength = strconv.Itoa(len(body))
			item.Propstat.Prop.ContentType = "text/plain"
			item.Propstat.Prop.LastModified = "Mon, 02 Jan 2006 15:04:05 GMT"
		}
		ms.Responses = append(ms.Responses, item)
	}
	if f.dirs[p] {
		add(p+"/", true, nil)
	} else if body, ok := f.files[p]; ok {
		add(p, false, body)
	} else {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Depth") != "0" {
		prefix := strings.TrimSuffix(p, "/") + "/"
		for dir := range f.dirs {
			if dir != p && pathDir(dir) == p {
				add(dir+"/", true, nil)
			}
		}
		for file, body := range f.files {
			if pathDir(file) == p {
				add(file, false, body)
			}
		}
		_ = prefix
	}
	out, _ := xml.Marshal(ms)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(xml.Header))
	w.Write(out)
}

func TestWebDAVBackendLifecycle(t *testing.T) {
	fake := newFakeDAV()
	srv := httptest.NewServer(fake)
	defer srv.Close()

	b, err := newWebDAV(Config{
		ID: "dav", Name: "dav", Type: "webdav", BaseURL: srv.URL + "/dav",
		Prefix: "media", Username: "u", Password: "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := b.Test(ctx); err != nil {
		t.Fatalf("test: %v", err)
	}
	body := []byte("0123456789")
	if err := b.Write(ctx, "posts/a.txt", bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatal(err)
	}
	entries, err := b.List(ctx, "posts")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "a.txt" || entries[0].Size != int64(len(body)) {
		t.Fatalf("list: %#v", entries)
	}
	rc, err := b.Read(ctx, "posts/a.txt", 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != "3456" {
		t.Fatalf("range = %q", got)
	}
	if err := b.Copy(ctx, "posts/a.txt", "posts/b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := b.Move(ctx, "posts/b.txt", "posts/c.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stat(ctx, "posts/c.txt"); err != nil {
		t.Fatal(err)
	}
	if err := b.Delete(ctx, "posts/c.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Stat(ctx, "posts/c.txt"); err == nil {
		t.Fatal("expected deleted file to be gone")
	}
	if err := b.Mkdir(ctx, "new/folder"); err != nil {
		t.Fatal(err)
	}
	entries, err = b.List(ctx, "new")
	if err != nil || len(entries) != 1 || !entries[0].IsDir || entries[0].Name != "folder" {
		t.Fatalf("mkdir list: %#v %v", entries, err)
	}
}

func TestSafeContentType(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"a.html", "text/html", "application/octet-stream"},
		{"a.svg", "image/svg+xml", "application/octet-stream"},
		{"a.js", "text/javascript", "application/octet-stream"},
		{"a.txt", "text/html", "application/octet-stream"},
		{"a.png", "", "image/png"},
		{"a.mp4", "video/mp4", "video/mp4"},
	}
	for _, tc := range cases {
		got, _ := SafeContentType(tc.name, tc.in)
		if got != tc.want {
			t.Errorf("SafeContentType(%q, %q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
	if SupportedUpload("a.svg") || SupportedUpload("a.html") {
		t.Fatal("script-capable file types must not be uploadable")
	}
	if !SupportedUpload("a.mp4") || !SupportedUpload("a.pdf") || !SupportedUpload("a.docx") {
		t.Fatal("expected video/document upload types to be supported")
	}
}

func TestCleanPathAndSanitize(t *testing.T) {
	for _, bad := range []string{"../x", "a/../../b", "/etc/passwd", "a\x00b", "a\rb"} {
		if _, err := CleanPath(bad); err == nil {
			t.Errorf("CleanPath(%q) should fail", bad)
		}
	}
	for _, good := range []string{"", "a", "a/b", "2026/01/photo.png", "über/ß.txt"} {
		if _, err := CleanPath(good); err != nil {
			t.Errorf("CleanPath(%q): %v", good, err)
		}
	}
	if got := SanitizeFilename("../../evil name.PNG"); got != "evil-name.png" {
		t.Fatalf("sanitize = %q", got)
	}
}
