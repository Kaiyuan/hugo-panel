package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Kaiyuan/hugo-panel/internal/media"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

func (s *Server) handleMediaLibraries(w http.ResponseWriter, r *http.Request, sess store.Session) {
	list, err := s.media.List(r.Context())
	if err != nil {
		writeHugoError(w, err)
		return
	}
	views := make([]media.View, 0, len(list))
	for _, cfg := range list {
		views = append(views, cfg.View())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"libraries":             views,
		"default":               media.DefaultID,
		"allowed_extensions":    media.AllowedExtensions(),
		"default_max_upload_mb": media.DefaultMaxUpload,
	})
}

func (s *Server) handleMediaLibrarySave(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var cfg media.Config
	if !decodeJSON(w, r, &cfg, 64*1024) {
		return
	}
	if err := s.checkMediaRoot(cfg); err != nil {
		writeHugoError(w, err)
		return
	}
	saved, err := s.media.Save(r.Context(), cfg)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"library": saved.View()})
}

// checkMediaRoot prevents a local library from exposing the panel's own data
// directory (SQLite database, backups, sessions, custom locales).
func (s *Server) checkMediaRoot(cfg media.Config) error {
	if cfg.Type != "local" || strings.TrimSpace(cfg.Root) == "" {
		return nil
	}
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return &media.Error{Code: "invalid_path", Message: "Invalid local directory"}
	}
	data, err := filepath.Abs(s.cfg.DataDir)
	if err != nil {
		return nil
	}
	if pathWithin(data, root) || pathWithin(root, data) {
		return &media.Error{Code: "invalid_path", Message: "Storage directory must not overlap the panel data directory"}
	}
	return nil
}

func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (s *Server) handleMediaLibraryDelete(w http.ResponseWriter, r *http.Request, sess store.Session) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Storage id is required")
		return
	}
	if err := s.media.Delete(r.Context(), id); err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMediaLibraryTest(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var cfg media.Config
	if !decodeJSON(w, r, &cfg, 64*1024) {
		return
	}
	if err := s.checkMediaRoot(cfg); err != nil {
		writeHugoError(w, err)
		return
	}
	if err := s.media.Test(r.Context(), cfg); err != nil {
		target := cfg.Endpoint
		if cfg.Type == "webdav" {
			target = cfg.BaseURL
		}
		s.logger.Warn("media storage test failed", "type", cfg.Type, "target", target, "error", err)
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func mediaLibraryID(r *http.Request) string {
	id := strings.TrimSpace(r.URL.Query().Get("library"))
	if id == "" {
		return media.DefaultID
	}
	return id
}

func (s *Server) handleMediaList(w http.ResponseWriter, r *http.Request, sess store.Session) {
	ctx := r.Context()
	id := mediaLibraryID(r)
	clean, err := media.CleanPath(r.URL.Query().Get("path"))
	if err != nil {
		writeHugoError(w, err)
		return
	}
	backend, cfg, err := s.media.Backend(ctx, id)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	entries, err := backend.List(ctx, clean)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	media.ApplyURLs(backend, cfg.ID, entries)
	var stats mediaStats
	for _, entry := range entries {
		if entry.IsDir {
			continue
		}
		stats.Files++
		stats.Bytes += entry.Size
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"library":       cfg.View(),
		"path":          clean,
		"entries":       entries,
		"stats":         stats,
		"max_upload_mb": cfg.MaxUploadMB,
	})
}

type mediaStats struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

type mediaTransferRequest struct {
	FromLibrary string `json:"from_library"`
	From        string `json:"from"`
	ToLibrary   string `json:"to_library"`
	To          string `json:"to"`
}

func (s *Server) handleMediaMove(w http.ResponseWriter, r *http.Request, sess store.Session) {
	s.handleMediaTransfer(w, r, true)
}

func (s *Server) handleMediaCopy(w http.ResponseWriter, r *http.Request, sess store.Session) {
	s.handleMediaTransfer(w, r, false)
}

func (s *Server) handleMediaTransfer(w http.ResponseWriter, r *http.Request, move bool) {
	var req mediaTransferRequest
	if !decodeJSON(w, r, &req, 64*1024) {
		return
	}
	ctx := r.Context()
	fromLib := strings.TrimSpace(req.FromLibrary)
	if fromLib == "" {
		fromLib = media.DefaultID
	}
	toLib := strings.TrimSpace(req.ToLibrary)
	if toLib == "" {
		toLib = fromLib
	}
	from, err := media.CleanPath(req.From)
	if err != nil || from == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Source file is required")
		return
	}
	to, err := media.CleanPath(req.To)
	if err != nil || to == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Destination file is required")
		return
	}
	if from == to && fromLib == toLib {
		writeError(w, http.StatusBadRequest, "invalid_request", "Source and destination are the same")
		return
	}
	fromBackend, fromCfg, err := s.media.Backend(ctx, fromLib)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	toBackend, toCfg, err := s.media.Backend(ctx, toLib)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	source, err := fromBackend.Stat(ctx, from)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	if source.IsDir {
		writeError(w, http.StatusBadRequest, "invalid_request", "Folders cannot be moved or copied here")
		return
	}
	if _, err := toBackend.Stat(ctx, to); err == nil {
		writeError(w, http.StatusConflict, "file_exists", "A file already exists at this path")
		return
	} else if !media.IsCode(err, "not_found") {
		writeHugoError(w, err)
		return
	}
	maxTransfer := media.MaxUploadBytes(toCfg.MaxUploadMB)
	if fromCfg.ID == toCfg.ID {
		if move {
			err = fromBackend.Move(ctx, from, to)
		} else {
			err = fromBackend.Copy(ctx, from, to)
		}
	} else {
		err = s.transferMedia(ctx, fromBackend, toBackend, from, to, source, move, maxTransfer)
	}
	if err != nil {
		writeHugoError(w, err)
		return
	}
	entry, err := toBackend.Stat(ctx, to)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	media.ApplyURLs(toBackend, toCfg.ID, []media.Entry{entry})
	entry.URL = toBackend.URL(to)
	writeJSON(w, http.StatusOK, map[string]any{"entry": entry})
}

func (s *Server) transferMedia(ctx context.Context, from, to media.Backend, fromKey, toKey string, source media.Entry, move bool, maxSize int64) error {
	if maxSize > 0 && source.Size > maxSize {
		return &media.Error{Code: "file_too_large", Message: "File is larger than the destination upload limit"}
	}
	reader, err := from.Read(ctx, fromKey, 0, -1)
	if err != nil {
		return err
	}
	defer reader.Close()
	dir := path.Join(s.cfg.DataDir, "tmp")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".media-transfer-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := io.Copy(tmp, reader); err != nil {
		tmp.Close()
		return err
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		tmp.Close()
		return err
	}
	if err := to.Write(ctx, toKey, tmp, size, source.ContentType); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if move {
		_ = from.Delete(ctx, fromKey)
	}
	return nil
}

func (s *Server) handleMediaFolder(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req struct {
		Library string `json:"library"`
		Path    string `json:"path"`
	}
	if !decodeJSON(w, r, &req, 32*1024) {
		return
	}
	lib := strings.TrimSpace(req.Library)
	if lib == "" {
		lib = media.DefaultID
	}
	clean, err := media.CleanPath(req.Path)
	if err != nil || clean == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Folder name is required")
		return
	}
	backend, _, err := s.media.Backend(r.Context(), lib)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	if err := backend.Mkdir(r.Context(), clean); err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true, "path": clean})
}

func (s *Server) handleMediaUpload(w http.ResponseWriter, r *http.Request, sess store.Session) {
	ctx := r.Context()
	id := mediaLibraryID(r)
	dir, err := media.CleanPath(r.URL.Query().Get("path"))
	if err != nil {
		writeHugoError(w, err)
		return
	}
	backend, cfg, err := s.media.Backend(ctx, id)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	max := media.MaxUploadBytes(cfg.MaxUploadMB)
	r.Body = http.MaxBytesReader(w, r.Body, max+2*1024*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Upload must be multipart form data")
		return
	}
	tmpDir := path.Join(s.cfg.DataDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o750); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not prepare the upload")
		return
	}
	var uploaded []media.Entry
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusBadRequest, "file_too_large", fmt.Sprintf("Upload is too large (maximum %d MB)", cfg.MaxUploadMB))
			} else {
				writeError(w, http.StatusBadRequest, "invalid_request", "Could not read the upload")
			}
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			_, _ = io.Copy(io.Discard, io.LimitReader(part, 1<<20))
			part.Close()
			continue
		}
		name := media.SanitizeFilename(part.FileName())
		if !media.SupportedUpload(name) {
			part.Close()
			writeError(w, http.StatusBadRequest, "invalid_type", "That file type is not supported")
			return
		}
		tmp, err := os.CreateTemp(tmpDir, ".media-upload-*")
		if err != nil {
			part.Close()
			writeError(w, http.StatusInternalServerError, "server_error", "Could not prepare the upload")
			return
		}
		size, err := io.Copy(tmp, io.LimitReader(part, max+1))
		part.Close()
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			writeError(w, http.StatusBadRequest, "invalid_request", "Could not read the upload")
			return
		}
		if size > max {
			tmp.Close()
			os.Remove(tmp.Name())
			writeError(w, http.StatusBadRequest, "file_too_large", fmt.Sprintf("Upload is too large (maximum %d MB)", cfg.MaxUploadMB))
			return
		}
		if _, err := tmp.Seek(0, io.SeekStart); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			writeError(w, http.StatusInternalServerError, "server_error", "Could not read the upload")
			return
		}
		key, finalName, err := uniqueMediaKey(ctx, backend, dir, name)
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			writeHugoError(w, err)
			return
		}
		contentType := media.ContentTypeFor(finalName)
		if err := backend.Write(ctx, key, tmp, size, contentType); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			writeHugoError(w, err)
			return
		}
		tmp.Close()
		os.Remove(tmp.Name())
		entry, err := backend.Stat(ctx, key)
		if err != nil {
			writeHugoError(w, err)
			return
		}
		media.ApplyURLs(backend, cfg.ID, []media.Entry{entry})
		entry.URL = backend.URL(key)
		uploaded = append(uploaded, entry)
	}
	if len(uploaded) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "Choose a file to upload")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"file": uploaded[0], "files": uploaded})
}

func uniqueMediaKey(ctx context.Context, backend media.Backend, dir, name string) (string, string, error) {
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	candidate := name
	for i := 2; i <= 1000; i++ {
		key := path.Join(dir, candidate)
		_, err := backend.Stat(ctx, key)
		if err != nil {
			if media.IsCode(err, "not_found") {
				return key, candidate, nil
			}
			return "", "", err
		}
		candidate = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
	return "", "", &media.Error{Code: "file_exists", Message: "Could not find a free file name"}
}

func (s *Server) handleMediaDelete(w http.ResponseWriter, r *http.Request, sess store.Session) {
	id := mediaLibraryID(r)
	clean, err := media.CleanPath(r.URL.Query().Get("path"))
	if err != nil || clean == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "File path is required")
		return
	}
	backend, _, err := s.media.Backend(r.Context(), id)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	if err := backend.Delete(r.Context(), clean); err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMediaFile(w http.ResponseWriter, r *http.Request, sess store.Session) {
	id := mediaLibraryID(r)
	clean, err := media.CleanPath(r.URL.Query().Get("path"))
	if err != nil || clean == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "File path is required")
		return
	}
	backend, _, err := s.media.Backend(r.Context(), id)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	entry, err := backend.Stat(r.Context(), clean)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	ctype, kind := media.SafeContentType(entry.Name, entry.ContentType)
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	ctype = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, ctype)
	inline := media.Previewable(kind)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Disposition", contentDisposition(entry.Name, inline))
	// The panel denies framing globally; media previews (notably PDF) need to
	// be embeddable by the panel itself, but never by another origin.
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'")
	size := entry.Size
	if size < 0 {
		size = 0
	}
	start, end, ranged, unsatisfiable := parseByteRange(r.Header.Get("Range"), size)
	if unsatisfiable {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if ranged {
		length := end - start + 1
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method == http.MethodHead {
			return
		}
		reader, err := backend.Read(r.Context(), clean, start, length)
		if err != nil {
			return
		}
		defer reader.Close()
		_, _ = io.Copy(w, reader)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if r.Method == http.MethodHead {
		return
	}
	reader, err := backend.Read(r.Context(), clean, 0, -1)
	if err != nil {
		return
	}
	defer reader.Close()
	_, _ = io.Copy(w, reader)
}

// parseByteRange handles a single range. ok=false with unsatisfiable=false
// means the header should be ignored (serve the whole file).
func parseByteRange(header string, size int64) (start, end int64, ok, unsatisfiable bool) {
	header = strings.TrimSpace(header)
	if header == "" || !strings.HasPrefix(header, "bytes=") {
		return 0, 0, false, false
	}
	spec := strings.TrimSpace(strings.TrimPrefix(header, "bytes="))
	if spec == "" || strings.Contains(spec, ",") {
		return 0, 0, false, false
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false, false
	}
	if parts[0] == "" {
		if parts[1] == "" {
			return 0, 0, false, false
		}
		n, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true, false
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false, false
	}
	if start >= size {
		return 0, 0, false, true
	}
	end = size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < start {
			return 0, 0, false, false
		}
		if end >= size {
			end = size - 1
		}
	}
	return start, end, true, false
}

func contentDisposition(name string, inline bool) string {
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return '-'
		}
		return r
	}, name)
	if ascii == "" {
		ascii = "file"
	}
	return fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`, disposition, ascii, url.PathEscape(name))
}
