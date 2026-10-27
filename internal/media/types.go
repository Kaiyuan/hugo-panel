package media

import "strings"

// contentTypes is also the upload allowlist. HTML, SVG, XML and scripts are
// deliberately absent: they would execute on the panel or site origin.
var contentTypes = map[string]string{
	// Images
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
	".ico":  "image/x-icon",
	".bmp":  "image/bmp",
	".tif":  "image/tiff",
	".tiff": "image/tiff",

	// Video
	".mp4":  "video/mp4",
	".m4v":  "video/x-m4v",
	".webm": "video/webm",
	".ogv":  "video/ogg",
	".mov":  "video/quicktime",
	".avi":  "video/x-msvideo",
	".mkv":  "video/x-matroska",

	// Audio
	".mp3":  "audio/mpeg",
	".wav":  "audio/wav",
	".ogg":  "audio/ogg",
	".oga":  "audio/ogg",
	".flac": "audio/flac",
	".m4a":  "audio/mp4",

	// Documents
	".pdf":      "application/pdf",
	".doc":      "application/msword",
	".docx":     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xls":      "application/vnd.ms-excel",
	".xlsx":     "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".ppt":      "application/vnd.ms-powerpoint",
	".pptx":     "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":      "application/vnd.oasis.opendocument.text",
	".ods":      "application/vnd.oasis.opendocument.spreadsheet",
	".odp":      "application/vnd.oasis.opendocument.presentation",
	".txt":      "text/plain; charset=utf-8",
	".md":       "text/markdown; charset=utf-8",
	".markdown": "text/markdown; charset=utf-8",
	".csv":      "text/csv; charset=utf-8",
	".json":     "application/json",
	".rtf":      "application/rtf",
	".epub":     "application/epub+zip",

	// Archives
	".zip": "application/zip",
	".7z":  "application/x-7z-compressed",
	".tar": "application/x-tar",
	".gz":  "application/gzip",
}

// ContentTypeFor resolves a file name to its declared MIME type.
func ContentTypeFor(name string) string { return detectContentType(name) }

// KindFor classifies a MIME type for the preview UI.
func KindFor(contentType string) string { return kindOf(contentType) }

// Previewable reports whether the panel can render the file inline.
func Previewable(kind string) bool {
	switch kind {
	case "image", "video", "audio", "pdf", "text":
		return true
	default:
		return false
	}
}

// InlineContentType strips parameters for response headers that need a bare type.
func InlineContentType(contentType string) string {
	return strings.TrimSpace(strings.Split(contentType, ";")[0])
}

// SafeContentType normalises a content type for inline delivery. Types that can
// execute script on the panel origin are always downgraded to a download.
func SafeContentType(name, contentType string) (string, string) {
	base := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if base == "" {
		base = detectContentType(name)
	}
	switch base {
	case "text/html", "application/xhtml+xml", "image/svg+xml",
		"text/xml", "application/xml", "application/javascript",
		"text/javascript", "application/x-javascript":
		return "application/octet-stream", "file"
	}
	if contentType == "" {
		contentType = base
	}
	return contentType, kindOf(base)
}

// MaxUploadBytes converts the configured MB limit to bytes.
func MaxUploadBytes(mb int) int64 {
	if mb <= 0 {
		mb = DefaultMaxUpload
	}
	return int64(mb) * 1024 * 1024
}
