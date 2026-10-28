package hugo

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type Site struct {
	Dir     string
	Binary  string
	DataDir string
}

type ContentItem struct {
	Path        string         `json:"path"`
	Title       string         `json:"title"`
	Slug        string         `json:"slug,omitempty"`
	Date        string         `json:"date,omitempty"`
	Lastmod     string         `json:"lastmod,omitempty"`
	Draft       bool           `json:"draft"`
	Status      string         `json:"status"`
	Section     string         `json:"section"`
	Type        string         `json:"type,omitempty"`
	Layout      string         `json:"layout,omitempty"`
	Description string         `json:"description,omitempty"`
	Tags        []string       `json:"tags"`
	Categories  []string       `json:"categories"`
	Weight      int            `json:"weight"`
	WordCount   int            `json:"word_count"`
	Bundle      bool           `json:"bundle"`
	Format      string         `json:"format"`
	ModTime     string         `json:"mod_time"`
	Size        int64          `json:"size"`
	Body        string         `json:"body,omitempty"`
	FrontMatter map[string]any `json:"front_matter,omitempty"`
	Error       string         `json:"error,omitempty"`
}

type ListOptions struct {
	Section string
	Status  string
	Query   string
	Page    int
	Limit   int
	Sort    string
}

type ListResult struct {
	Items []ContentItem `json:"items"`
	Total int           `json:"total"`
	Page  int           `json:"page"`
	Limit int           `json:"limit"`
}

type SaveInput struct {
	Path        string         `json:"path"`
	NewPath     string         `json:"new_path"`
	Format      string         `json:"format"`
	FrontMatter map[string]any `json:"front_matter"`
	Body        string         `json:"body"`
}

type SectionInfo struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type ContentMeta struct {
	Sections    []SectionInfo `json:"sections"`
	Directories []string      `json:"directories"`
	Tags        []string      `json:"tags"`
	Categories  []string      `json:"categories"`
	Exists      bool          `json:"exists"`
	Initialized bool          `json:"initialized"`
}

var contentExts = map[string]bool{".md": true, ".markdown": true, ".mdown": true}

func (s *Site) ContentDir() string { return filepath.Join(s.Dir, "content") }

func safeRelPath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", errf("invalid_path", "Path is required")
	}
	if strings.ContainsRune(rel, '\x00') || strings.Contains(rel, "\\") {
		return "", errf("invalid_path", "Path contains invalid characters")
	}
	if strings.HasPrefix(rel, "/") {
		return "", errf("invalid_path", "Path must be relative")
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
		return "", errf("invalid_path", "Path must stay inside the Hugo site")
	}
	return clean, nil
}

func joinWithin(base, rel string) (string, error) {
	clean, err := safeRelPath(rel)
	if err != nil {
		return "", err
	}
	full := filepath.Join(base, clean)
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	relCheck, err := filepath.Rel(baseAbs, fullAbs)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", errf("invalid_path", "Path must stay inside the Hugo site")
	}
	return full, nil
}

func validContentPath(rel string) error {
	if !contentExts[strings.ToLower(filepath.Ext(rel))] {
		return errf("invalid_path", "Content files must use .md or .markdown")
	}
	base := strings.ToLower(filepath.Base(rel))
	if base == "" || strings.HasPrefix(base, ".") {
		return errf("invalid_path", "Invalid content file name")
	}
	return nil
}

func (s *Site) ScanContent() ([]ContentItem, error) {
	root := s.ContentDir()
	var items []ContentItem
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			if path != root {
				name := d.Name()
				if strings.HasPrefix(name, ".") || name == "node_modules" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !contentExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		item, err := readContentFile(s, path, filepath.ToSlash(rel), false)
		if err != nil {
			return nil
		}
		items = append(items, item)
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []ContentItem{}, nil
		}
		return nil, fmt.Errorf("scan content: %w", err)
	}
	return items, nil
}

func readContentFile(s *Site, fullPath, rel string, withBody bool) (ContentItem, error) {
	info, err := os.Stat(fullPath)
	if err != nil {
		return ContentItem{}, err
	}
	if info.Size() > 16*1024*1024 {
		return ContentItem{Path: rel, Title: filepath.Base(rel), Size: info.Size(), Status: "error", Error: "file is larger than 16 MB", ModTime: info.ModTime().UTC().Format(time.RFC3339)}, nil
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return ContentItem{}, err
	}
	format, fm, body, parseErr := SplitFrontMatter(data)
	item := ContentItem{
		Path:        rel,
		Format:      format,
		Body:        body,
		FrontMatter: fm,
		ModTime:     info.ModTime().UTC().Format(time.RFC3339),
		Size:        info.Size(),
		Bundle:      strings.EqualFold(filepath.Base(rel), "index.md") || strings.EqualFold(filepath.Base(rel), "_index.md"),
		WordCount:   wordCount(body),
	}
	if parseErr != nil {
		item.Error = parseErr.Error()
		item.Status = "error"
		item.Title = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
		item.Section = sectionFor(rel)
		if !withBody {
			item.Body = ""
			item.FrontMatter = nil
		}
		return item, nil
	}
	if item.FrontMatter == nil {
		item.FrontMatter = map[string]any{}
	}
	if !withBody {
		item.Body = ""
		item.FrontMatter = nil
	}
	item.Title = strings.TrimSpace(StringValue(fm["title"]))
	if item.Title == "" {
		item.Title = prettyFilename(rel)
	}
	item.Slug = StringValue(fm["slug"])
	item.Type = StringValue(fm["type"])
	item.Layout = StringValue(fm["layout"])
	item.Description = StringValue(fm["description"])
	item.Weight = IntValue(fm["weight"])
	item.Tags = StringSlice(fm["tags"])
	if item.Tags == nil {
		item.Tags = []string{}
	}
	item.Categories = StringSlice(fm["categories"])
	if item.Categories == nil {
		item.Categories = []string{}
	}
	item.Section = sectionFor(rel)
	item.Lastmod = StringValue(fm["lastmod"])
	dateValue := fm["date"]
	if dateValue == nil {
		dateValue = fm["publishDate"]
	}
	if t, ok := TimeValue(dateValue); ok {
		item.Date = t.Format(time.RFC3339)
	} else {
		item.Date = strings.TrimSpace(StringValue(dateValue))
	}
	item.Draft = BoolValue(fm["draft"])
	switch {
	case item.Draft:
		item.Status = "draft"
	case item.Date != "":
		if t, ok := TimeValue(item.Date); ok && t.After(time.Now().Add(1*time.Minute)) {
			item.Status = "scheduled"
		} else {
			item.Status = "published"
		}
	default:
		item.Status = "published"
	}
	return item, nil
}

func prettyFilename(rel string) string {
	name := filepath.Base(rel)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	if name == "index" || name == "_index" {
		dir := sectionFor(rel)
		if dir != "" {
			name = dir
		}
	}
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.TrimSpace(name)
	if name == "" {
		return rel
	}
	r := []rune(name)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func sectionFor(rel string) string {
	rel = filepath.ToSlash(rel)
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." || dir == "" {
		return ""
	}
	return strings.Split(dir, "/")[0]
}

func wordCount(s string) int {
	count := 0
	inWord := false
	for _, r := range s {
		if isCJK(r) {
			count++
			inWord = false
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\'' || r == '-' {
			if !inWord {
				count++
				inWord = true
			}
		} else {
			inWord = false
		}
	}
	return count
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

func (s *Site) ListContent(opts ListOptions) (ListResult, error) {
	items, err := s.ScanContent()
	if err != nil {
		return ListResult{}, err
	}
	filtered := make([]ContentItem, 0, len(items))
	q := strings.ToLower(strings.TrimSpace(opts.Query))
	for _, item := range items {
		if opts.Section == "root" {
			if item.Section != "" {
				continue
			}
		} else if opts.Section != "" && item.Section != opts.Section {
			continue
		}
		if opts.Status != "" && item.Status != opts.Status {
			continue
		}
		if q != "" {
			hay := strings.ToLower(item.Title + " " + item.Path + " " + item.Description + " " + strings.Join(item.Tags, " ") + " " + strings.Join(item.Categories, " "))
			if !strings.Contains(hay, q) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		at, aok := TimeValue(a.Date)
		bt, bok := TimeValue(b.Date)
		if aok && bok && !at.Equal(bt) {
			return at.After(bt)
		}
		if aok != bok {
			return aok
		}
		if a.ModTime != b.ModTime {
			return a.ModTime > b.ModTime
		}
		return a.Path < b.Path
	})
	page, limit := opts.Page, opts.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	total := len(filtered)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return ListResult{Items: filtered[start:end], Total: total, Page: page, Limit: limit}, nil
}

func (s *Site) ContentMeta() (ContentMeta, error) {
	items, err := s.ScanContent()
	if err != nil {
		return ContentMeta{}, err
	}
	_, statErr := os.Stat(s.ContentDir())
	meta := ContentMeta{Exists: statErr == nil, Initialized: s.ConfigExists(), Sections: []SectionInfo{}, Directories: []string{}, Tags: []string{}, Categories: []string{}}
	sectionCounts := map[string]int{}
	tagSet := map[string]bool{}
	catSet := map[string]bool{}
	for _, item := range items {
		name := item.Section
		if name == "" {
			name = "root"
		}
		sectionCounts[name]++
		for _, t := range item.Tags {
			tagSet[t] = true
		}
		for _, c := range item.Categories {
			catSet[c] = true
		}
	}
	for name, n := range sectionCounts {
		meta.Sections = append(meta.Sections, SectionInfo{Name: name, Count: n})
	}
	sort.Slice(meta.Sections, func(i, j int) bool {
		if meta.Sections[i].Name == "root" {
			return true
		}
		if meta.Sections[j].Name == "root" {
			return false
		}
		return meta.Sections[i].Name < meta.Sections[j].Name
	})
	for t := range tagSet {
		meta.Tags = append(meta.Tags, t)
	}
	for c := range catSet {
		meta.Categories = append(meta.Categories, c)
	}
	sort.Strings(meta.Tags)
	sort.Strings(meta.Categories)
	meta.Directories = s.contentDirectories()
	return meta, nil
}

// contentDirectories lists every directory inside content/ using slash
// separators. Hidden folders and dependency folders are skipped, matching the
// content scanner.
func (s *Site) contentDirectories() []string {
	out := []string{}
	root := s.ContentDir()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if !d.IsDir() {
			return nil
		}
		if path != root {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" {
				return filepath.SkipDir
			}
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return out
	}
	sort.Strings(out)
	return out
}

func (s *Site) ReadContent(rel string) (ContentItem, error) {
	clean, err := safeRelPath(rel)
	if err != nil {
		return ContentItem{}, err
	}
	if err := validContentPath(clean); err != nil {
		return ContentItem{}, err
	}
	full, err := joinWithin(s.ContentDir(), clean)
	if err != nil {
		return ContentItem{}, err
	}
	info, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return ContentItem{}, errf("not_found", "Content file not found")
	}
	if err != nil {
		return ContentItem{}, err
	}
	if info.IsDir() {
		return ContentItem{}, errf("not_found", "Content file not found")
	}
	item, err := readContentFile(s, full, filepath.ToSlash(clean), true)
	if err != nil {
		return ContentItem{}, err
	}
	if item.Error != "" {
		return ContentItem{}, errf("invalid_front_matter", "The front matter in this file could not be parsed: "+item.Error)
	}
	return item, nil
}

func (s *Site) CreateContent(in SaveInput) (ContentItem, error) {
	clean, err := safeRelPath(in.Path)
	if err != nil {
		return ContentItem{}, err
	}
	if err := validContentPath(clean); err != nil {
		return ContentItem{}, err
	}
	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" {
		format = FormatYAML
	}
	if !validFormat(format) {
		return ContentItem{}, errf("invalid_request", "Unsupported front matter format")
	}
	full, err := joinWithin(s.ContentDir(), clean)
	if err != nil {
		return ContentItem{}, err
	}
	if _, err := os.Stat(full); err == nil {
		return ContentItem{}, errf("file_exists", "A content file already exists at this path")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return ContentItem{}, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return ContentItem{}, fmt.Errorf("create content directory: %w", err)
	}
	if in.FrontMatter == nil {
		in.FrontMatter = map[string]any{}
	}
	if strings.TrimSpace(StringValue(in.FrontMatter["title"])) == "" {
		in.FrontMatter["title"] = prettyFilename(clean)
	}
	if _, ok := in.FrontMatter["date"]; !ok {
		in.FrontMatter["date"] = time.Now().Format("2006-01-02")
	}
	if _, ok := in.FrontMatter["draft"]; !ok {
		in.FrontMatter["draft"] = false
	}
	data, err := EncodeFrontMatter(format, in.FrontMatter, in.Body)
	if err != nil {
		return ContentItem{}, err
	}
	if len(data) > 16*1024*1024 {
		return ContentItem{}, errf("file_too_large", "Content file is larger than 16 MB")
	}
	if err := atomicWrite(full, data, 0o644); err != nil {
		return ContentItem{}, err
	}
	return s.ReadContent(filepath.ToSlash(clean))
}

func (s *Site) UpdateContent(in SaveInput) (ContentItem, error) {
	clean, err := safeRelPath(in.Path)
	if err != nil {
		return ContentItem{}, err
	}
	if err := validContentPath(clean); err != nil {
		return ContentItem{}, err
	}
	full, err := joinWithin(s.ContentDir(), clean)
	if err != nil {
		return ContentItem{}, err
	}
	if _, err := os.Stat(full); errors.Is(err, fs.ErrNotExist) {
		return ContentItem{}, errf("not_found", "Content file not found")
	} else if err != nil {
		return ContentItem{}, err
	}
	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" {
		data, _ := os.ReadFile(full)
		detected, _, _, _ := SplitFrontMatter(data)
		format = detected
	}
	if !validFormat(format) {
		format = FormatYAML
	}
	if in.FrontMatter == nil {
		in.FrontMatter = map[string]any{}
	}
	data, err := EncodeFrontMatter(format, in.FrontMatter, in.Body)
	if err != nil {
		return ContentItem{}, err
	}
	if len(data) > 16*1024*1024 {
		return ContentItem{}, errf("file_too_large", "Content file is larger than 16 MB")
	}
	target := full
	targetRel := clean
	if strings.TrimSpace(in.NewPath) != "" {
		newClean, err := safeRelPath(in.NewPath)
		if err != nil {
			return ContentItem{}, err
		}
		if newClean != clean {
			if err := validContentPath(newClean); err != nil {
				return ContentItem{}, err
			}
			target, err = joinWithin(s.ContentDir(), newClean)
			if err != nil {
				return ContentItem{}, err
			}
			if _, err := os.Stat(target); err == nil {
				return ContentItem{}, errf("file_exists", "A content file already exists at this path")
			} else if !errors.Is(err, fs.ErrNotExist) {
				return ContentItem{}, err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return ContentItem{}, err
			}
			targetRel = newClean
		}
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(full); err == nil {
		mode = info.Mode().Perm()
	}
	if err := atomicWrite(target, data, mode); err != nil {
		return ContentItem{}, err
	}
	if target != full {
		if err := os.Remove(full); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return ContentItem{}, fmt.Errorf("remove old content file: %w", err)
		}
	}
	return s.ReadContent(filepath.ToSlash(targetRel))
}

func (s *Site) DeleteContent(rel string) error {
	clean, err := safeRelPath(rel)
	if err != nil {
		return err
	}
	if err := validContentPath(clean); err != nil {
		return err
	}
	full, err := joinWithin(s.ContentDir(), clean)
	if err != nil {
		return err
	}
	if err := os.Remove(full); errors.Is(err, fs.ErrNotExist) {
		return errf("not_found", "Content file not found")
	} else if err != nil {
		return err
	}
	return nil
}

func validFormat(format string) bool {
	return format == FormatYAML || format == FormatTOML || format == FormatJSON
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".hugo-panel-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

var slugCleaner = regexp.MustCompile(`[^a-z0-9]+`)

func Slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugCleaner.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}
