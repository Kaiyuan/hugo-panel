package media

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:displayname/>
    <d:resourcetype/>
    <d:getcontentlength/>
    <d:getcontenttype/>
    <d:getlastmodified/>
  </d:prop>
</d:propfind>`

type webdavBackend struct {
	base       *url.URL
	prefix     string
	username   string
	password   string
	publicBase string
	client     *http.Client
}

func newWebDAV(cfg Config) (*webdavBackend, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errf("invalid_request", "WebDAV URL must be an absolute http(s) URL")
	}
	prefix, err := CleanPath(cfg.Prefix)
	if err != nil {
		return nil, err
	}
	return &webdavBackend{
		base:       u,
		prefix:     prefix,
		username:   cfg.Username,
		password:   cfg.Password,
		publicBase: strings.TrimRight(cfg.PublicBase, "/"),
		client:     &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (b *webdavBackend) endpointRel(rel string, isDir bool) string {
	u := *b.base
	p := path.Join("/", b.base.Path, rel)
	if p == "" || p == "." {
		p = "/"
	}
	if isDir && !strings.HasSuffix(p, "/") {
		p += "/"
	}
	u.Path = p
	u.RawPath = ""
	return u.String()
}

func (b *webdavBackend) endpoint(key string, isDir bool) string {
	rel := strings.Trim(path.Join(b.prefix, key), "/")
	if rel == "." {
		rel = ""
	}
	return b.endpointRel(rel, isDir)
}

func (b *webdavBackend) request(ctx context.Context, method, target string, body io.Reader, contentLength int64, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, errf("server_error", "Could not build the WebDAV request")
	}
	if contentLength >= 0 {
		req.ContentLength = contentLength
	}
	if b.username != "" || b.password != "" {
		req.SetBasicAuth(b.username, b.password)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, errf("server_error", "Could not reach the WebDAV server: %v", err)
	}
	return resp, nil
}

func davError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	message := strings.TrimSpace(string(data))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	switch resp.StatusCode {
	case http.StatusNotFound:
		return errf("not_found", "File not found")
	case http.StatusUnauthorized, http.StatusForbidden:
		return errf("forbidden", "WebDAV denied the request: %s", message)
	case http.StatusPreconditionFailed:
		return errf("file_exists", "A file already exists at this path")
	case http.StatusConflict:
		return errf("invalid_path", "WebDAV could not create the target folder: %s", message)
	case http.StatusRequestedRangeNotSatisfiable:
		return errf("invalid_request", "Requested range is not satisfiable")
	default:
		return errf("server_error", "WebDAV request failed (%d): %s", resp.StatusCode, message)
	}
}

type davMultistatus struct {
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href      string        `xml:"href"`
	Propstats []davPropstat `xml:"propstat"`
}

type davPropstat struct {
	Status string  `xml:"status"`
	Prop   davProp `xml:"prop"`
}

type davProp struct {
	DisplayName   string          `xml:"displayname"`
	ContentLength string          `xml:"getcontentlength"`
	ContentType   string          `xml:"getcontenttype"`
	LastModified  string          `xml:"getlastmodified"`
	ResourceType  davResourceType `xml:"resourcetype"`
}

type davResourceType struct {
	Collection *struct{} `xml:"collection"`
}

func (p davProp) collection() bool { return p.ResourceType.Collection != nil }

func pickDAVProp(stats []davPropstat) davProp {
	var fallback davProp
	for _, ps := range stats {
		if strings.Contains(ps.Status, "200") {
			return ps.Prop
		}
		if ps.Prop.DisplayName != "" || ps.Prop.ContentLength != "" || ps.Prop.ContentType != "" || ps.Prop.collection() {
			fallback = ps.Prop
		}
	}
	return fallback
}

func parseDAVTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, layout := range []string{http.TimeFormat, time.RFC1123Z, time.RFC1123, time.RFC3339} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func (b *webdavBackend) List(ctx context.Context, dir string) ([]Entry, error) {
	clean, err := CleanPath(dir)
	if err != nil {
		return nil, err
	}
	target := b.endpoint(clean, true)
	headers := map[string]string{
		"Depth":        "1",
		"Content-Type": "application/xml; charset=utf-8",
	}
	resp, err := b.request(ctx, "PROPFIND", target, strings.NewReader(propfindBody), int64(len(propfindBody)), headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return []Entry{}, nil
	}
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		return nil, davError(resp)
	}
	var ms davMultistatus
	decodeErr := xml.NewDecoder(resp.Body).Decode(&ms)
	_ = resp.Body.Close()
	if decodeErr != nil {
		return nil, errf("server_error", "Could not read the WebDAV listing")
	}
	base, _ := url.Parse(target)
	basePath := strings.TrimSuffix(base.Path, "/")
	var out []Entry
	for _, item := range ms.Responses {
		href, err := url.Parse(strings.TrimSpace(item.Href))
		if err != nil {
			continue
		}
		ref := href.Path
		if ref == "" {
			ref = strings.TrimSpace(item.Href)
		}
		ref = strings.TrimSuffix(ref, "/")
		if ref == "" || ref == basePath {
			continue
		}
		if !strings.HasPrefix(ref, basePath+"/") {
			// Some servers return path-less hrefs; fall back to the base name.
			if !strings.Contains(ref, "/") {
				ref = basePath + "/" + ref
			} else {
				continue
			}
		}
		rel := strings.TrimPrefix(ref, basePath+"/")
		rel = strings.Trim(rel, "/")
		if rel == "" || strings.Contains(rel, "/") {
			continue
		}
		prop := pickDAVProp(item.Propstats)
		name := path.Base(rel)
		if prop.DisplayName != "" && !strings.ContainsAny(prop.DisplayName, "/\\") {
			name = prop.DisplayName
		}
		if prop.collection() {
			out = append(out, Entry{Path: joinKey(clean, rel), Name: name, IsDir: true})
			continue
		}
		size, _ := strconv.ParseInt(strings.TrimSpace(prop.ContentLength), 10, 64)
		ct := strings.TrimSpace(prop.ContentType)
		if ct == "" {
			ct = detectContentType(name)
		}
		out = append(out, Entry{
			Path:        joinKey(clean, rel),
			Name:        name,
			Size:        size,
			ModTime:     parseDAVTime(prop.LastModified),
			ContentType: ct,
			Kind:        kindOf(ct),
		})
	}
	sortedEntries(out)
	return out, nil
}

func (b *webdavBackend) propfindOne(ctx context.Context, key string) (Entry, error) {
	target := b.endpoint(key, false)
	headers := map[string]string{
		"Depth":        "0",
		"Content-Type": "application/xml; charset=utf-8",
	}
	resp, err := b.request(ctx, "PROPFIND", target, strings.NewReader(propfindBody), int64(len(propfindBody)), headers)
	if err != nil {
		return Entry{}, err
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return Entry{}, errf("not_found", "File not found")
	}
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		return Entry{}, davError(resp)
	}
	var ms davMultistatus
	decodeErr := xml.NewDecoder(resp.Body).Decode(&ms)
	_ = resp.Body.Close()
	if decodeErr != nil || len(ms.Responses) == 0 {
		return Entry{}, errf("not_found", "File not found")
	}
	prop := pickDAVProp(ms.Responses[0].Propstats)
	name := path.Base(key)
	size, _ := strconv.ParseInt(strings.TrimSpace(prop.ContentLength), 10, 64)
	ct := strings.TrimSpace(prop.ContentType)
	if ct == "" {
		ct = detectContentType(name)
	}
	return Entry{
		Path:        key,
		Name:        name,
		Size:        size,
		ModTime:     parseDAVTime(prop.LastModified),
		ContentType: ct,
		Kind:        kindOf(ct),
	}, nil
}

func (b *webdavBackend) Stat(ctx context.Context, key string) (Entry, error) {
	clean, err := CleanPath(key)
	if err != nil || clean == "" {
		return Entry{}, errf("not_found", "File not found")
	}
	resp, err := b.request(ctx, http.MethodHead, b.endpoint(clean, false), nil, -1, nil)
	if err != nil {
		return Entry{}, err
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return Entry{}, errf("not_found", "File not found")
	}
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		_ = resp.Body.Close()
		return b.propfindOne(ctx, clean)
	}
	if resp.StatusCode >= 400 {
		return Entry{}, davError(resp)
	}
	_ = resp.Body.Close()
	ct := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if ct == "" {
		ct = detectContentType(clean)
	}
	mod := ""
	if t, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		mod = t.UTC().Format(time.RFC3339)
	}
	if resp.ContentLength < 0 {
		return b.propfindOne(ctx, clean)
	}
	name := path.Base(clean)
	return Entry{
		Path:        clean,
		Name:        name,
		Size:        resp.ContentLength,
		ModTime:     mod,
		ContentType: ct,
		Kind:        kindOf(ct),
	}, nil
}

func (b *webdavBackend) Read(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	clean, err := CleanPath(key)
	if err != nil || clean == "" {
		return nil, errf("not_found", "File not found")
	}
	headers := map[string]string{}
	if offset > 0 || length >= 0 {
		end := ""
		if length >= 0 {
			end = strconv.FormatInt(offset+length-1, 10)
		}
		headers["Range"] = fmt.Sprintf("bytes=%d-%s", offset, end)
	}
	resp, err := b.request(ctx, http.MethodGet, b.endpoint(clean, false), nil, -1, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, davError(resp)
	}
	return resp.Body, nil
}

func (b *webdavBackend) ensureParent(ctx context.Context, key string) error {
	parts := strings.Split(strings.Trim(key, "/"), "/")
	if len(parts) < 2 {
		return nil
	}
	return b.Mkdir(ctx, strings.Join(parts[:len(parts)-1], "/"))
}

func (b *webdavBackend) Write(ctx context.Context, key string, r io.ReadSeeker, size int64, contentType string) error {
	clean, err := CleanPath(key)
	if err != nil || clean == "" {
		return errf("invalid_path", "Path is not allowed")
	}
	if err := b.ensureParent(ctx, clean); err != nil {
		return err
	}
	headers := map[string]string{}
	if contentType != "" {
		headers["Content-Type"] = contentType
	}
	resp, err := b.request(ctx, http.MethodPut, b.endpoint(clean, false), r, size, headers)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return davError(resp)
	}
	_ = resp.Body.Close()
	return nil
}

func (b *webdavBackend) Delete(ctx context.Context, key string) error {
	clean, err := CleanPath(key)
	if err != nil || clean == "" {
		return errf("not_found", "File not found")
	}
	resp, err := b.request(ctx, http.MethodDelete, b.endpoint(clean, false), nil, -1, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return errf("not_found", "File not found")
	}
	if resp.StatusCode >= 400 {
		return davError(resp)
	}
	_ = resp.Body.Close()
	return nil
}

func (b *webdavBackend) Copy(ctx context.Context, from, to string) error {
	return b.transfer(ctx, "COPY", from, to)
}

func (b *webdavBackend) Move(ctx context.Context, from, to string) error {
	return b.transfer(ctx, "MOVE", from, to)
}

func (b *webdavBackend) transfer(ctx context.Context, method, from, to string) error {
	fromClean, err := CleanPath(from)
	if err != nil || fromClean == "" {
		return errf("not_found", "File not found")
	}
	toClean, err := CleanPath(to)
	if err != nil || toClean == "" {
		return errf("invalid_path", "Path is not allowed")
	}
	if err := b.ensureParent(ctx, toClean); err != nil {
		return err
	}
	headers := map[string]string{
		"Destination": b.endpoint(toClean, false),
		"Overwrite":   "F",
	}
	resp, err := b.request(ctx, method, b.endpoint(fromClean, false), nil, -1, headers)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return davError(resp)
	}
	_ = resp.Body.Close()
	return nil
}

func (b *webdavBackend) Mkdir(ctx context.Context, dir string) error {
	clean, err := CleanPath(dir)
	if err != nil {
		return err
	}
	full := strings.Trim(path.Join(b.prefix, clean), "/")
	if full == "" || full == "." {
		return nil
	}
	parts := strings.Split(full, "/")
	for i := range parts {
		rel := strings.Join(parts[:i+1], "/")
		resp, err := b.request(ctx, "MKCOL", b.endpointRel(rel, true), nil, -1, nil)
		if err != nil {
			return err
		}
		switch resp.StatusCode {
		case http.StatusCreated, http.StatusOK, http.StatusNoContent, http.StatusMethodNotAllowed, http.StatusMovedPermanently:
			_ = resp.Body.Close()
		case http.StatusConflict:
			// Some servers return 409 when the collection already exists.
			_ = resp.Body.Close()
		default:
			return davError(resp)
		}
	}
	return nil
}

func (b *webdavBackend) Test(ctx context.Context) error {
	headers := map[string]string{
		"Depth":        "0",
		"Content-Type": "application/xml; charset=utf-8",
	}
	resp, err := b.request(ctx, "PROPFIND", b.endpointRel("", true), strings.NewReader(propfindBody), int64(len(propfindBody)), headers)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		return davError(resp)
	}
	_ = resp.Body.Close()
	return nil
}

func (b *webdavBackend) URL(key string) string {
	clean, _ := CleanPath(key)
	if b.publicBase != "" {
		return joinURL(b.publicBase, clean)
	}
	return b.endpoint(clean, false)
}
