package media

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type s3Backend struct {
	cfg      Config
	endpoint *url.URL
	prefix   string
	client   *http.Client
}

func newS3(cfg Config) (*s3Backend, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errf("invalid_request", "S3 endpoint must be an absolute http(s) URL")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errf("invalid_request", "S3 bucket is required")
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	prefix, err := CleanPath(cfg.Prefix)
	if err != nil {
		return nil, err
	}
	return &s3Backend{
		cfg:      cfg,
		endpoint: u,
		prefix:   prefix,
		client:   &http.Client{Timeout: 10 * time.Minute},
	}, nil
}

func (b *s3Backend) fullKey(key string) string {
	key = strings.Trim(key, "/")
	if b.prefix == "" {
		return key
	}
	if key == "" {
		return b.prefix
	}
	return b.prefix + "/" + key
}

// objectPath returns the Host and escaped path for a request. An empty key
// addresses the bucket root.
func (b *s3Backend) objectPath(key string) (host, escaped string) {
	base := strings.TrimSuffix(b.endpoint.EscapedPath(), "/")
	if b.cfg.PathStyle {
		host = b.endpoint.Host
		escaped = base + "/" + b.cfg.Bucket
		if key != "" {
			escaped += "/" + escapePath(key)
		}
		return host, escaped
	}
	host = b.cfg.Bucket + "." + b.endpoint.Host
	escaped = base + "/" + escapePath(key)
	return host, escaped
}

func (b *s3Backend) request(ctx context.Context, method, key string, query url.Values, headers http.Header, body io.Reader, size int64, payloadHash string) (*http.Response, error) {
	host, escaped := b.objectPath(key)
	if escaped == "" {
		escaped = "/"
	}
	qs := canonicalQuery(query)
	raw := b.endpoint.Scheme + "://" + host + escaped
	if qs != "" {
		raw += "?" + qs
	}
	req, err := http.NewRequestWithContext(ctx, method, raw, body)
	if err != nil {
		return nil, errf("server_error", "Could not build the S3 request")
	}
	req.Host = host
	if size >= 0 {
		req.ContentLength = size
	}
	for k, values := range headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}
	b.sign(req, escaped, qs, payloadHash, time.Now().UTC())
	resp, err := b.client.Do(req)
	if err != nil {
		return nil, errf("server_error", "Could not reach the S3 endpoint: %v", err)
	}
	return resp, nil
}

func (b *s3Backend) sign(req *http.Request, canonicalURI, canonicalQuery, payloadHash string, now time.Time) {
	amzDate := now.UTC().Format("20060102T150405Z")
	date := now.UTC().Format("20060102")
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	values := map[string]string{"host": host}
	for key, list := range req.Header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-amz-") || lower == "content-type" || lower == "range" {
			values[lower] = strings.Join(list, ",")
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var canonicalHeaders strings.Builder
	for _, key := range keys {
		canonicalHeaders.WriteString(key)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(strings.TrimSpace(values[key]))
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(keys, ";")

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")
	sum := sha256.Sum256([]byte(canonicalRequest))
	scope := date + "/" + b.cfg.Region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])

	key := hmacSHA256([]byte("AWS4"+b.cfg.SecretKey), date)
	key = hmacSHA256(key, b.cfg.Region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		b.cfg.AccessKey, scope, signedHeaders, signature))
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func canonicalQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		list := append([]string(nil), values[key]...)
		sort.Strings(list)
		for _, value := range list {
			parts = append(parts, escapeSegment(key)+"="+escapeSegment(value))
		}
	}
	return strings.Join(parts, "&")
}

func hashReader(r io.ReadSeeker) (string, error) {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "", errf("invalid_request", "Could not read the upload")
	}
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", errf("invalid_request", "Could not read the upload")
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return "", errf("invalid_request", "Could not read the upload")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func s3Error(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	_ = resp.Body.Close()
	message := strings.TrimSpace(string(data))
	code := ""
	var apiErr struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if xml.Unmarshal(data, &apiErr) == nil && apiErr.Message != "" {
		code = apiErr.Code
		message = apiErr.Message
	}
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	switch resp.StatusCode {
	case http.StatusNotFound:
		return errf("not_found", "File not found")
	case http.StatusForbidden, http.StatusUnauthorized:
		return errf("forbidden", "S3 denied the request: %s", message)
	case http.StatusConflict:
		return errf("file_exists", "A file already exists at this path")
	case http.StatusRequestedRangeNotSatisfiable:
		return errf("invalid_request", "Requested range is not satisfiable")
	default:
		if code != "" {
			return errf("server_error", "S3 %s: %s", code, message)
		}
		return errf("server_error", "S3 request failed (%d): %s", resp.StatusCode, message)
	}
}

type s3ListResult struct {
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
	Contents              []struct {
		Key          string `xml:"Key"`
		LastModified string `xml:"LastModified"`
		Size         int64  `xml:"Size"`
	} `xml:"Contents"`
	CommonPrefixes []struct {
		Prefix string `xml:"Prefix"`
	} `xml:"CommonPrefixes"`
}

func (b *s3Backend) List(ctx context.Context, dir string) ([]Entry, error) {
	clean, err := CleanPath(dir)
	if err != nil {
		return nil, err
	}
	base := b.fullKey(clean)
	prefix := base
	if prefix != "" {
		prefix += "/"
	}
	var out []Entry
	token := ""
	for {
		query := url.Values{}
		query.Set("list-type", "2")
		query.Set("delimiter", "/")
		query.Set("prefix", prefix)
		query.Set("max-keys", "1000")
		if token != "" {
			query.Set("continuation-token", token)
		}
		resp, err := b.request(ctx, http.MethodGet, "", query, nil, nil, 0, emptySHA256)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 400 {
			return nil, s3Error(resp)
		}
		var page s3ListResult
		decodeErr := xml.NewDecoder(resp.Body).Decode(&page)
		_ = resp.Body.Close()
		if decodeErr != nil {
			return nil, errf("server_error", "Could not read the S3 listing")
		}
		for _, cp := range page.CommonPrefixes {
			rel := strings.TrimPrefix(cp.Prefix, prefix)
			rel = strings.Trim(rel, "/")
			if rel == "" || strings.Contains(rel, "/") {
				continue
			}
			out = append(out, Entry{Path: joinKey(clean, rel), Name: rel, IsDir: true})
		}
		for _, obj := range page.Contents {
			if !strings.HasPrefix(obj.Key, prefix) {
				continue
			}
			rel := strings.TrimPrefix(obj.Key, prefix)
			if rel == "" || strings.HasSuffix(rel, "/") || strings.Contains(rel, "/") {
				continue
			}
			ct := detectContentType(rel)
			mod := ""
			if t, err := time.Parse(time.RFC3339, obj.LastModified); err == nil {
				mod = t.UTC().Format(time.RFC3339)
			}
			out = append(out, Entry{
				Path:        joinKey(clean, rel),
				Name:        rel,
				Size:        obj.Size,
				ModTime:     mod,
				ContentType: ct,
				Kind:        kindOf(ct),
			})
		}
		if !page.IsTruncated || page.NextContinuationToken == "" {
			break
		}
		token = page.NextContinuationToken
	}
	sortedEntries(out)
	return out, nil
}

func (b *s3Backend) Stat(ctx context.Context, key string) (Entry, error) {
	clean, err := CleanPath(key)
	if err != nil || clean == "" {
		return Entry{}, errf("not_found", "File not found")
	}
	full := b.fullKey(clean)
	resp, err := b.request(ctx, http.MethodHead, full, nil, nil, nil, -1, emptySHA256)
	if err != nil {
		return Entry{}, err
	}
	if resp.StatusCode >= 400 {
		return Entry{}, s3Error(resp)
	}
	_ = resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = detectContentType(clean)
	}
	mod := ""
	if t, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		mod = t.UTC().Format(time.RFC3339)
	}
	name := clean
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return Entry{
		Path:        clean,
		Name:        name,
		Size:        resp.ContentLength,
		ModTime:     mod,
		ContentType: ct,
		Kind:        kindOf(ct),
	}, nil
}

func (b *s3Backend) Read(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	clean, err := CleanPath(key)
	if err != nil || clean == "" {
		return nil, errf("not_found", "File not found")
	}
	headers := http.Header{}
	if offset > 0 || length >= 0 {
		end := ""
		if length >= 0 {
			end = strconv.FormatInt(offset+length-1, 10)
		}
		headers.Set("Range", fmt.Sprintf("bytes=%d-%s", offset, end))
	}
	resp, err := b.request(ctx, http.MethodGet, b.fullKey(clean), nil, headers, nil, -1, emptySHA256)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, s3Error(resp)
	}
	return resp.Body, nil
}

func (b *s3Backend) Write(ctx context.Context, key string, r io.ReadSeeker, size int64, contentType string) error {
	clean, err := CleanPath(key)
	if err != nil || clean == "" {
		return errf("invalid_path", "Path is not allowed")
	}
	hash, err := hashReader(r)
	if err != nil {
		return err
	}
	headers := http.Header{}
	if contentType != "" {
		headers.Set("Content-Type", contentType)
	}
	resp, err := b.request(ctx, http.MethodPut, b.fullKey(clean), nil, headers, r, size, hash)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return s3Error(resp)
	}
	_ = resp.Body.Close()
	return nil
}

func (b *s3Backend) Delete(ctx context.Context, key string) error {
	clean, err := CleanPath(key)
	if err != nil || clean == "" {
		return errf("not_found", "File not found")
	}
	resp, err := b.request(ctx, http.MethodDelete, b.fullKey(clean), nil, nil, nil, -1, emptySHA256)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		_ = resp.Body.Close()
		return nil
	}
	if resp.StatusCode >= 400 {
		return s3Error(resp)
	}
	_ = resp.Body.Close()
	return nil
}

func (b *s3Backend) Copy(ctx context.Context, from, to string) error {
	fromClean, err := CleanPath(from)
	if err != nil || fromClean == "" {
		return errf("not_found", "File not found")
	}
	toClean, err := CleanPath(to)
	if err != nil || toClean == "" {
		return errf("invalid_path", "Path is not allowed")
	}
	headers := http.Header{}
	headers.Set("x-amz-copy-source", "/"+b.cfg.Bucket+"/"+escapePath(b.fullKey(fromClean)))
	resp, err := b.request(ctx, http.MethodPut, b.fullKey(toClean), nil, headers, nil, 0, emptySHA256)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return s3Error(resp)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	_ = resp.Body.Close()
	// S3 can return 200 with an <Error> document for CopyObject.
	if len(body) > 0 {
		var apiErr struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		}
		if xml.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
			return errf("server_error", "S3 %s: %s", apiErr.Code, apiErr.Message)
		}
	}
	return nil
}

func (b *s3Backend) Move(ctx context.Context, from, to string) error {
	if err := b.Copy(ctx, from, to); err != nil {
		return err
	}
	return b.Delete(ctx, from)
}

func (b *s3Backend) Mkdir(ctx context.Context, dir string) error {
	clean, err := CleanPath(dir)
	if err != nil || clean == "" {
		return nil
	}
	headers := http.Header{}
	headers.Set("Content-Type", "application/x-directory")
	resp, err := b.request(ctx, http.MethodPut, b.fullKey(clean)+"/", nil, headers, strings.NewReader(""), 0, emptySHA256)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return s3Error(resp)
	}
	_ = resp.Body.Close()
	return nil
}

func (b *s3Backend) Test(ctx context.Context) error {
	query := url.Values{}
	query.Set("list-type", "2")
	query.Set("max-keys", "1")
	if b.prefix != "" {
		query.Set("prefix", b.prefix+"/")
	}
	resp, err := b.request(ctx, http.MethodGet, "", query, nil, nil, 0, emptySHA256)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return s3Error(resp)
	}
	_ = resp.Body.Close()
	return nil
}

func (b *s3Backend) URL(key string) string {
	clean, _ := CleanPath(key)
	full := b.fullKey(clean)
	if b.cfg.PublicBase != "" {
		return joinURL(b.cfg.PublicBase, clean)
	}
	host, escaped := b.objectPath(full)
	return b.endpoint.Scheme + "://" + host + escaped
}
