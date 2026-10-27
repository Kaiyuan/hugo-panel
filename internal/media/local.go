package media

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type localBackend struct {
	root       string
	publicBase string
	id         string
	builtin    bool
}

func newLocal(cfg Config) (*localBackend, error) {
	root := strings.TrimSpace(cfg.Root)
	if root == "" {
		return nil, errf("invalid_request", "Local directory is required")
	}
	if !filepath.IsAbs(root) {
		return nil, errf("invalid_request", "Local directory must be an absolute path")
	}
	return &localBackend{
		root:       filepath.Clean(root),
		publicBase: strings.TrimRight(cfg.PublicBase, "/"),
		id:         cfg.ID,
		builtin:    cfg.Builtin,
	}, nil
}

func (b *localBackend) resolve(key string) (string, error) {
	clean, err := CleanPath(key)
	if err != nil {
		return "", err
	}
	full := b.root
	if clean != "" {
		full = filepath.Join(b.root, filepath.FromSlash(clean))
	}
	if !inside(b.root, full) {
		return "", errf("invalid_path", "Path is not allowed")
	}
	// Refuse to follow symlinks that escape the configured root. If the root
	// does not exist yet, the lexical check above is sufficient; uploads will
	// create it.
	realRoot, err := filepath.EvalSymlinks(b.root)
	if err != nil {
		return full, nil
	}
	check := full
	for {
		if real, err := filepath.EvalSymlinks(check); err == nil {
			if !inside(realRoot, real) {
				return "", errf("invalid_path", "Path is not allowed")
			}
			break
		}
		parent := filepath.Dir(check)
		if parent == check {
			break
		}
		check = parent
	}
	return full, nil
}

func inside(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func (b *localBackend) List(ctx context.Context, dir string) ([]Entry, error) {
	full, err := b.resolve(dir)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(full)
	if errors.Is(err, fs.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, errf("server_error", "Could not list the folder: %v", err)
	}
	out := make([]Entry, 0, len(items))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := item.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if item.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		if info.IsDir() {
			out = append(out, Entry{Path: joinKey(dir, name), Name: name, IsDir: true})
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		ct := detectContentType(name)
		out = append(out, Entry{
			Path:        joinKey(dir, name),
			Name:        name,
			Size:        info.Size(),
			ModTime:     info.ModTime().UTC().Format(time.RFC3339),
			ContentType: ct,
			Kind:        kindOf(ct),
		})
	}
	sortedEntries(out)
	return out, nil
}

func (b *localBackend) Stat(ctx context.Context, key string) (Entry, error) {
	full, err := b.resolve(key)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return Entry{}, errf("not_found", "File not found")
	}
	if err != nil {
		return Entry{}, errf("server_error", "Could not read the file: %v", err)
	}
	if info.IsDir() {
		return Entry{}, errf("not_found", "File not found")
	}
	ct := detectContentType(info.Name())
	return Entry{
		Path:        key,
		Name:        info.Name(),
		Size:        info.Size(),
		ModTime:     info.ModTime().UTC().Format(time.RFC3339),
		ContentType: ct,
		Kind:        kindOf(ct),
	}, nil
}

func (b *localBackend) Read(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error) {
	full, err := b.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errf("not_found", "File not found")
	}
	if err != nil {
		return nil, errf("server_error", "Could not open the file: %v", err)
	}
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			f.Close()
			return nil, errf("server_error", "Could not read the file")
		}
	}
	if length >= 0 {
		return &limitedReadCloser{Reader: io.LimitReader(f, length), closer: f}, nil
	}
	return f, nil
}

type limitedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (l *limitedReadCloser) Close() error { return l.closer.Close() }

func (b *localBackend) Write(ctx context.Context, key string, r io.ReadSeeker, size int64, contentType string) error {
	full, err := b.resolve(key)
	if err != nil {
		return err
	}
	dir := filepath.Dir(full)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errf("server_error", "Could not create the folder: %v", err)
	}
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return errf("server_error", "Could not create the file: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return errf("server_error", "Could not save the file: %v", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return errf("server_error", "Could not save the file: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return errf("server_error", "Could not save the file: %v", err)
	}
	if err := os.Rename(tmpName, full); err != nil {
		return errf("server_error", "Could not save the file: %v", err)
	}
	return nil
}

func (b *localBackend) Delete(ctx context.Context, key string) error {
	full, err := b.resolve(key)
	if err != nil {
		return err
	}
	info, err := os.Lstat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return errf("not_found", "File not found")
	}
	if err != nil {
		return errf("server_error", "Could not read the file: %v", err)
	}
	if info.IsDir() {
		return errf("invalid_request", "Folders cannot be deleted here")
	}
	if err := os.Remove(full); err != nil {
		return errf("server_error", "Could not delete the file: %v", err)
	}
	return nil
}

func (b *localBackend) Move(ctx context.Context, from, to string) error {
	src, err := b.resolve(from)
	if err != nil {
		return err
	}
	dst, err := b.resolve(to)
	if err != nil {
		return err
	}
	if info, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) || (err == nil && info.IsDir()) {
		return errf("not_found", "File not found")
	}
	if _, err := os.Lstat(dst); err == nil {
		return errf("file_exists", "A file already exists at this path")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return errf("server_error", "Could not create the folder: %v", err)
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Different filesystem: fall back to copy + delete.
	if err := copyFile(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func (b *localBackend) Copy(ctx context.Context, from, to string) error {
	src, err := b.resolve(from)
	if err != nil {
		return err
	}
	dst, err := b.resolve(to)
	if err != nil {
		return err
	}
	if info, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) || (err == nil && info.IsDir()) {
		return errf("not_found", "File not found")
	}
	if _, err := os.Lstat(dst); err == nil {
		return errf("file_exists", "A file already exists at this path")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return errf("server_error", "Could not create the folder: %v", err)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return errf("server_error", "Could not open the source file: %v", err)
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".copy-*")
	if err != nil {
		return errf("server_error", "Could not create the target file: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return errf("server_error", "Could not copy the file: %v", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return errf("server_error", "Could not copy the file: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return errf("server_error", "Could not copy the file: %v", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return errf("server_error", "Could not copy the file: %v", err)
	}
	return nil
}

func (b *localBackend) Mkdir(ctx context.Context, dir string) error {
	full, err := b.resolve(dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(full, 0o755); err != nil {
		return errf("server_error", "Could not create the folder: %v", err)
	}
	return nil
}

func (b *localBackend) Test(ctx context.Context) error {
	if err := os.MkdirAll(b.root, 0o755); err != nil {
		return errf("invalid_path", "Could not access %s: %v", b.root, err)
	}
	f, err := os.CreateTemp(b.root, ".hugo-panel-test-*")
	if err != nil {
		return errf("invalid_path", "Could not write to %s: %v", b.root, err)
	}
	name := f.Name()
	if _, err := f.WriteString("ok"); err != nil {
		f.Close()
		os.Remove(name)
		return errf("invalid_path", "Could not write to %s: %v", b.root, err)
	}
	f.Close()
	os.Remove(name)
	return nil
}

func (b *localBackend) URL(key string) string {
	clean, _ := CleanPath(key)
	if b.publicBase != "" {
		return joinURL(b.publicBase, clean)
	}
	if b.builtin {
		return "/images/" + escapePath(clean)
	}
	return previewURL(b.id, clean)
}
