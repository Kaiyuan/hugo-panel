package hugo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

const (
	FormatYAML = "yaml"
	FormatTOML = "toml"
	FormatJSON = "json"
)

// SplitFrontMatter parses Hugo supported YAML, TOML and JSON front matter.
func SplitFrontMatter(data []byte) (format string, fm map[string]any, body string, err error) {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	text := string(data)
	if strings.HasPrefix(text, "---") && (len(text) == 3 || text[3] == '\n' || text[3] == '\r') {
		rest := text[3:]
		rest = strings.TrimPrefix(rest, "\r")
		rest = strings.TrimPrefix(rest, "\n")
		idx, endLen := findDelimiter(rest, "---")
		altIdx, altLen := findDelimiter(rest, "...")
		if altIdx >= 0 && (idx < 0 || altIdx < idx) {
			idx, endLen = altIdx, altLen
		}
		if idx < 0 {
			return "", nil, "", fmt.Errorf("unterminated YAML front matter")
		}
		fm = map[string]any{}
		if err := yaml.Unmarshal([]byte(rest[:idx]), &fm); err != nil {
			return "", nil, "", fmt.Errorf("parse YAML front matter: %w", err)
		}
		if fm == nil {
			fm = map[string]any{}
		}
		body = rest[idx+endLen:]
		body = strings.TrimLeft(body, "\r\n")
		return FormatYAML, normalizeMap(fm), body, nil
	}
	if strings.HasPrefix(text, "+++") && (len(text) == 3 || text[3] == '\n' || text[3] == '\r') {
		rest := text[3:]
		rest = strings.TrimPrefix(rest, "\r")
		rest = strings.TrimPrefix(rest, "\n")
		idx, endLen := findDelimiter(rest, "+++")
		if idx < 0 {
			return "", nil, "", fmt.Errorf("unterminated TOML front matter")
		}
		fm = map[string]any{}
		md, err := toml.Decode(rest[:idx], &fm)
		if err != nil {
			return "", nil, "", fmt.Errorf("parse TOML front matter: %w", err)
		}
		_ = md
		if fm == nil {
			fm = map[string]any{}
		}
		body = rest[idx+endLen:]
		body = strings.TrimLeft(body, "\r\n")
		return FormatTOML, normalizeMap(fm), body, nil
	}
	if strings.HasPrefix(strings.TrimLeft(text, " \r\n\t"), "{") {
		dec := json.NewDecoder(bytes.NewReader(data))
		var raw map[string]any
		if err := dec.Decode(&raw); err != nil {
			return "", nil, "", fmt.Errorf("parse JSON front matter: %w", err)
		}
		offset := dec.InputOffset()
		body = string(data[offset:])
		body = strings.TrimLeft(body, "\r\n")
		if raw == nil {
			raw = map[string]any{}
		}
		return FormatJSON, normalizeMap(raw), body, nil
	}
	return FormatYAML, map[string]any{}, text, nil
}

func findDelimiter(s, delim string) (int, int) {
	for start := 0; start <= len(s); {
		lineEnd := strings.IndexByte(s[start:], '\n')
		var line string
		next := len(s)
		if lineEnd < 0 {
			line = s[start:]
		} else {
			line = s[start : start+lineEnd]
			next = start + lineEnd + 1
		}
		if strings.TrimSpace(strings.TrimSuffix(line, "\r")) == delim {
			consumed := next - start
			return start, consumed
		}
		if lineEnd < 0 {
			break
		}
		start = next
	}
	return -1, 0
}

func EncodeFrontMatter(format string, fm map[string]any, body string) ([]byte, error) {
	if fm == nil {
		fm = map[string]any{}
	}
	switch format {
	case FormatTOML:
		var buf bytes.Buffer
		buf.WriteString("+++\n")
		enc := toml.NewEncoder(&buf)
		if err := enc.Encode(normalizeMap(fm)); err != nil {
			return nil, fmt.Errorf("encode TOML front matter: %w", err)
		}
		buf.WriteString("+++\n")
		buf.WriteString(body)
		return buf.Bytes(), nil
	case FormatJSON:
		b, err := json.MarshalIndent(normalizeMap(fm), "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode JSON front matter: %w", err)
		}
		var out bytes.Buffer
		out.Write(b)
		out.WriteString("\n")
		out.WriteString(body)
		return out.Bytes(), nil
	default:
		b, err := yaml.Marshal(normalizeMap(fm))
		if err != nil {
			return nil, fmt.Errorf("encode YAML front matter: %w", err)
		}
		var out bytes.Buffer
		out.WriteString("---\n")
		out.Write(b)
		out.WriteString("---\n")
		out.WriteString(body)
		return out.Bytes(), nil
	}
}

func normalizeMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeValue(v any) any {
	switch x := v.(type) {
	case time.Time:
		if x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0 && x.Nanosecond() == 0 {
			return x.Format("2006-01-02")
		}
		return x.Format(time.RFC3339)
	case map[string]any:
		return normalizeMap(x)
	case map[any]any:
		m := map[string]any{}
		for k, val := range x {
			m[fmt.Sprint(k)] = normalizeValue(val)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = normalizeValue(val)
		}
		return out
	case []string:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = val
		}
		return out
	case float64:
		if x == float64(int64(x)) {
			return int64(x)
		}
		return x
	default:
		return v
	}
}

func StringValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case time.Time:
		return x.Format(time.RFC3339)
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			if s := StringValue(item); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ",")
	case map[string]any:
		b, _ := json.Marshal(x)
		return string(b)
	default:
		return fmt.Sprint(x)
	}
}

func StringSlice(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
		parts := strings.Split(x, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s := strings.TrimSpace(StringValue(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		if s := strings.TrimSpace(StringValue(x)); s != "" {
			return []string{s}
		}
		return nil
	}
}

func BoolValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		b, _ := strconv.ParseBool(strings.TrimSpace(x))
		return b
	case int64:
		return x != 0
	case int:
		return x != 0
	case float64:
		return x != 0
	default:
		return false
	}
}

func AnySlice(v any) []any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

func IntValue(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func TimeValue(v any) (time.Time, bool) {
	switch x := v.(type) {
	case time.Time:
		return x, true
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return time.Time{}, false
		}
		layouts := []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02", time.RFC1123Z, time.RFC822Z}
		for _, layout := range layouts {
			if t, err := time.Parse(layout, s); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}
