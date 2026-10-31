package hugo

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const (
	defaultHugoDownloadBase = "https://github.com/gohugoio/hugo/releases/download"
	maxHugoDownloadBytes    = 256 << 20
	maxHugoBinaryBytes      = 200 << 20
	hugoDownloadTimeout     = 15 * time.Minute
)

var exactHugoVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// hugoDownloadBase returns the release download base. HUGO_DOWNLOAD_BASE can
// point at a mirror (and is used by tests with a local server).
func hugoDownloadBase() string {
	if value, ok := os.LookupEnv("HUGO_DOWNLOAD_BASE"); ok {
		if base := strings.TrimRight(strings.TrimSpace(value), "/"); base != "" {
			return base
		}
	}
	return defaultHugoDownloadBase
}

func hugoAssetName(version, goos, goarch string) (string, error) {
	arch := ""
	switch goos {
	case "linux":
		switch goarch {
		case "amd64", "arm64", "386", "arm":
			arch = goarch
		}
	case "darwin":
		// Hugo publishes a universal binary for macOS.
		arch = "universal"
	case "windows":
		switch goarch {
		case "amd64", "arm64", "386":
			arch = goarch
		}
	}
	if arch == "" {
		return "", fmt.Errorf("unsupported platform for Hugo upgrades: %s/%s", goos, goarch)
	}
	extension := ".tar.gz"
	if goos == "windows" {
		extension = ".zip"
	}
	return fmt.Sprintf("hugo_extended_%s_%s-%s%s", version, goos, arch, extension), nil
}

func hugoDownloadURL(version string) (string, error) {
	asset, err := hugoAssetName(version, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return "", err
	}
	raw := fmt.Sprintf("%s/v%s/%s", hugoDownloadBase(), version, asset)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid Hugo download URL: %s", raw)
	}
	return raw, nil
}

// InstallHugoVersion downloads an official extended Hugo release into the
// panel data directory and returns the executable path. Versioned directories
// keep an existing binary usable if a later upgrade is rejected or rolled back.
func (svc *Service) InstallHugoVersion(ctx context.Context, dataDir, version string) (string, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if !exactHugoVersionRe.MatchString(version) {
		return "", errf("invalid_request", "Hugo version must look like 0.169.0")
	}
	if strings.TrimSpace(dataDir) == "" {
		return "", errf("invalid_request", "Data directory is not configured")
	}
	dir := filepath.Join(dataDir, "hugo-bin", version)
	binaryName := "hugo"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	target := filepath.Join(dir, binaryName)
	if got, err := binaryVersion(ctx, target); err == nil && got == version {
		return target, nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create Hugo binary directory: %w", err)
	}
	downloadURL, err := hugoDownloadURL(version)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "HugoPanel")
	client := &http.Client{Timeout: hugoDownloadTimeout}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download Hugo %s: %w", version, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download Hugo %s: unexpected HTTP status %s", version, res.Status)
	}
	if res.ContentLength > maxHugoDownloadBytes {
		return "", fmt.Errorf("download Hugo %s: release archive is too large", version)
	}

	archive, err := os.CreateTemp(dir, ".hugo-download-*")
	if err != nil {
		return "", fmt.Errorf("create temporary download: %w", err)
	}
	archivePath := archive.Name()
	defer os.Remove(archivePath)
	written, copyErr := io.Copy(archive, io.LimitReader(res.Body, maxHugoDownloadBytes+1))
	closeErr := archive.Close()
	if copyErr != nil {
		return "", fmt.Errorf("download Hugo %s: %w", version, copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("save Hugo %s: %w", version, closeErr)
	}
	if written > maxHugoDownloadBytes {
		return "", fmt.Errorf("download Hugo %s: release archive is too large", version)
	}

	tmpBinary := filepath.Join(dir, ".hugo-"+version+"-tmp")
	_ = os.Remove(tmpBinary)
	if err := extractHugoBinary(archivePath, tmpBinary); err != nil {
		return "", err
	}
	if err := os.Chmod(tmpBinary, 0o755); err != nil {
		_ = os.Remove(tmpBinary)
		return "", fmt.Errorf("make Hugo executable: %w", err)
	}
	got, err := binaryVersion(ctx, tmpBinary)
	if err != nil {
		_ = os.Remove(tmpBinary)
		return "", fmt.Errorf("verify downloaded Hugo binary: %w", err)
	}
	if got != version {
		_ = os.Remove(tmpBinary)
		return "", fmt.Errorf("downloaded Hugo reported version %s, expected %s", got, version)
	}
	_ = os.Remove(target)
	if err := os.Rename(tmpBinary, target); err != nil {
		_ = os.Remove(tmpBinary)
		return "", fmt.Errorf("install Hugo binary: %w", err)
	}
	return target, nil
}

func extractHugoBinary(archivePath, target string) error {
	// The temporary file has no meaningful extension, so detect the archive
	// type from its magic bytes instead of the name.
	head := make([]byte, 4)
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open Hugo archive: %w", err)
	}
	_, _ = io.ReadFull(f, head)
	f.Close()
	if head[0] == 'P' && head[1] == 'K' {
		return extractHugoFromZip(archivePath, target)
	}
	return extractHugoFromTarGz(archivePath, target)
}

func extractHugoFromTarGz(archivePath, target string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open Hugo archive: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("read Hugo archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read Hugo archive: %w", err)
		}
		name := filepath.Base(hdr.Name)
		if hdr.Typeflag != tar.TypeReg || (name != "hugo" && name != "hugo.exe") {
			continue
		}
		return writeHugoBinary(target, tr)
	}
	return fmt.Errorf("Hugo binary not found in archive")
}

func extractHugoFromZip(archivePath, target string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open Hugo archive: %w", err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		name := filepath.Base(f.Name)
		if f.FileInfo().IsDir() || (name != "hugo" && name != "hugo.exe") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("read Hugo archive: %w", err)
		}
		err = writeHugoBinary(target, rc)
		rc.Close()
		return err
	}
	return fmt.Errorf("Hugo binary not found in archive")
}

func writeHugoBinary(target string, src io.Reader) error {
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create Hugo binary: %w", err)
	}
	written, copyErr := io.Copy(out, io.LimitReader(src, maxHugoBinaryBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(target)
		return fmt.Errorf("extract Hugo binary: %w", copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(target)
		return fmt.Errorf("extract Hugo binary: %w", closeErr)
	}
	if written > maxHugoBinaryBytes {
		_ = os.Remove(target)
		return fmt.Errorf("Hugo binary is too large")
	}
	return nil
}
