package hugo

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var hugoVersionRe = regexp.MustCompile(`\bv?(\d+)\.(\d+)\.(\d+)`)

// ParseHugoVersion extracts the numeric version from `hugo version` output,
// for example "hugo v0.169.0+extended linux/amd64 BuildDate=..." -> "0.169.0".
func ParseHugoVersion(output string) (string, bool) {
	m := hugoVersionRe.FindStringSubmatch(output)
	if m == nil {
		return "", false
	}
	return m[1] + "." + m[2] + "." + m[3], true
}

func versionNumbers(v string) ([3]int, bool) {
	m := hugoVersionRe.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var parts [3]int
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return [3]int{}, false
		}
		parts[i] = n
	}
	return parts, true
}

// HugoVersionAtLeast reports whether version is greater than or equal to
// minimum. Unparsable versions are treated as older.
func HugoVersionAtLeast(version, minimum string) bool {
	cur, ok := versionNumbers(version)
	if !ok {
		return false
	}
	min, ok := versionNumbers(minimum)
	if !ok {
		return true
	}
	for i := 0; i < 3; i++ {
		if cur[i] != min[i] {
			return cur[i] > min[i]
		}
	}
	return true
}

// HugoVersion runs the configured Hugo binary and returns its normalized
// version. It is used by the init dialog and the settings page to compare the
// installed binary with the latest upstream release.
func (s *Site) HugoVersion(ctx context.Context) (string, error) {
	if _, err := exec.LookPath(s.Binary); err != nil {
		if info, statErr := os.Stat(s.Binary); statErr != nil || info.IsDir() {
			return "", errf("hugo_not_found", "Hugo binary not found: "+s.Binary)
		}
	}
	res := runCommand(ctx, "", 30*time.Second, nil, s.Binary, "version")
	if res.Err != nil {
		msg := strings.TrimSpace(res.Output)
		if msg == "" {
			msg = res.Err.Error()
		}
		return "", errf("hugo_not_found", "Could not run the Hugo binary: "+msg)
	}
	version, ok := ParseHugoVersion(res.Output)
	if !ok {
		return "", errf("hugo_not_found", "Could not detect the Hugo version from: "+firstLine(res.Output))
	}
	return version, nil
}
