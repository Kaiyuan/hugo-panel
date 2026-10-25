package server

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestNormalizeVersion(t *testing.T) {
	cases := map[string]string{
		"v0.0.1":  "0.0.1",
		" 0.0.1 ": "0.0.1",
		"0.0.1":   "0.0.1",
		"v":       "dev",
		"":        "dev",
		"   ":     "dev",
	}
	for input, want := range cases {
		if got := normalizeVersion(input); got != want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestComputeAssetVersionChangesWithFrontend(t *testing.T) {
	index := []byte("<html>index</html>")
	staticFS := fstest.MapFS{
		"js/app.js": &fstest.MapFile{Data: []byte("console.log('one')")},
	}
	base := computeAssetVersion("0.0.1", index, staticFS)
	if !strings.HasPrefix(base, "0.0.1.") {
		t.Fatalf("asset version %q does not include the panel version", base)
	}
	if got := computeAssetVersion("0.0.1", index, staticFS); got != base {
		t.Fatalf("asset version is not deterministic: %q vs %q", base, got)
	}
	changedAsset := fstest.MapFS{
		"js/app.js": &fstest.MapFile{Data: []byte("console.log('two')")},
	}
	if got := computeAssetVersion("0.0.1", index, changedAsset); got == base {
		t.Fatal("asset version did not change when a static asset changed")
	}
	if got := computeAssetVersion("0.0.1", []byte("<html>changed</html>"), staticFS); got == base {
		t.Fatal("asset version did not change when index.html changed")
	}
}
