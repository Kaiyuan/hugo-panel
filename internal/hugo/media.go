package hugo

import (
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

type MediaFile struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Size        int64  `json:"size"`
	ModTime     string `json:"mod_time"`
	ContentType string `json:"content_type"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

type MediaStats struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

var mediaTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".ico":  "image/x-icon",
}

func (s *Site) MediaRoot() string { return filepath.Join(s.Dir, "static", "images") }

func (s *Site) ListMedia() ([]MediaFile, error) {
	root := s.MediaRoot()
	if info, err := os.Stat(root); errors.Is(err, fs.ErrNotExist) || (err == nil && !info.IsDir()) {
		return []MediaFile{}, nil
	} else if err != nil {
		return nil, err
	}
	var files []MediaFile
	var walk func(dir, prefix string, depth int) error
	walk = func(dir, prefix string, depth int) error {
		if depth > 8 {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			rel := filepath.ToSlash(filepath.Join(prefix, name))
			full := filepath.Join(dir, name)
			if entry.IsDir() {
				if err := walk(full, rel, depth+1); err != nil {
					return err
				}
				continue
			}
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			ext := strings.ToLower(filepath.Ext(name))
			if _, ok := mediaTypes[ext]; !ok {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			m := MediaFile{
				Path:        rel,
				Name:        name,
				URL:         "/images/" + rel,
				Size:        info.Size(),
				ModTime:     info.ModTime().UTC().Format(time.RFC3339),
				ContentType: mediaTypes[ext],
			}
			if f, err := os.Open(full); err == nil {
				if cfg, _, err := image.DecodeConfig(f); err == nil {
					m.Width, m.Height = cfg.Width, cfg.Height
				}
				f.Close()
			}
			files = append(files, m)
		}
		return nil
	}
	if err := walk(root, "", 1); err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ModTime > files[j].ModTime })
	return files, nil
}

func (s *Site) MediaStats() (MediaStats, error) {
	files, err := s.ListMedia()
	if err != nil {
		return MediaStats{}, err
	}
	var stats MediaStats
	stats.Files = len(files)
	for _, f := range files {
		stats.Bytes += f.Size
	}
	return stats, nil
}

// SaveMedia stores an uploaded image under static/images. SVG is intentionally
// rejected because inline SVG can execute scripts on the site origin.
func (s *Site) SaveMedia(r io.Reader, filename, subdir string) (MediaFile, error) {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return MediaFile{}, errf("invalid_request", "File name is required")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if _, ok := mediaTypes[ext]; !ok {
		return MediaFile{}, errf("invalid_type", "Unsupported image type. Use JPG, PNG, GIF, WebP, AVIF or ICO.")
	}
	name := sanitizeFilename(filename)
	root := s.MediaRoot()
	dir := root
	if strings.TrimSpace(subdir) != "" {
		sub, err := safeRelPath(subdir)
		if err != nil {
			return MediaFile{}, err
		}
		dir = filepath.Join(root, sub)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return MediaFile{}, err
	}
	base := strings.TrimSuffix(name, filepath.Ext(name))
	name = base + ext
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, fs.ErrNotExist) {
			break
		}
		if i > 1000 {
			return MediaFile{}, errf("file_exists", "Could not find a free file name")
		}
		name = fmt.Sprintf("%s-%d%s", base, i, ext)
	}
	target := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return MediaFile{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return MediaFile{}, err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return MediaFile{}, err
	}
	if err := tmp.Close(); err != nil {
		return MediaFile{}, err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return MediaFile{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return MediaFile{}, err
	}
	rel, _ := filepath.Rel(root, target)
	m := MediaFile{
		Path:        filepath.ToSlash(rel),
		Name:        name,
		URL:         "/images/" + filepath.ToSlash(rel),
		Size:        info.Size(),
		ModTime:     info.ModTime().UTC().Format(time.RFC3339),
		ContentType: mediaTypes[ext],
	}
	if f, err := os.Open(target); err == nil {
		if cfg, _, err := image.DecodeConfig(f); err == nil {
			m.Width, m.Height = cfg.Width, cfg.Height
		}
		f.Close()
	}
	return m, nil
}

func (s *Site) MediaPath(rel string) (string, string, error) {
	clean, err := safeRelPath(rel)
	if err != nil {
		return "", "", err
	}
	full, err := joinWithin(s.MediaRoot(), clean)
	if err != nil {
		return "", "", err
	}
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return "", "", errf("not_found", "File not found")
	}
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		return "", "", errf("not_found", "File not found")
	}
	ct := mediaTypes[strings.ToLower(filepath.Ext(full))]
	if ct == "" {
		ct = "application/octet-stream"
	}
	return full, ct, nil
}

func (s *Site) DeleteMedia(rel string) error {
	clean, err := safeRelPath(rel)
	if err != nil {
		return err
	}
	full, err := joinWithin(s.MediaRoot(), clean)
	if err != nil {
		return err
	}
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return errf("not_found", "File not found")
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errf("invalid_request", "Not a file")
	}
	return os.Remove(full)
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))
	var b strings.Builder
	lastDash := false
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '.' || r == '_' {
			b.WriteRune(r)
			lastDash = false
		} else if r == '-' || r == ' ' {
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), ".-_")
	out = strings.TrimSpace(out)
	if out == "" {
		out = "image"
	}
	runes := []rune(out)
	if len(runes) > 100 {
		out = string(runes[:100])
	}
	return out + ext
}
