package hugo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContentMetaListsDirectories(t *testing.T) {
	root := t.TempDir()
	site := &Site{Dir: root}
	for _, dir := range []string{"content/posts/2026", "content/notes", "content/.hidden", "content/node_modules/pkg"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "content/posts/2026/hello.md"), []byte("---\ntitle: Hello\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, err := site.ContentMeta()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"notes", "posts", "posts/2026"}
	if len(meta.Directories) != len(want) {
		t.Fatalf("directories = %#v", meta.Directories)
	}
	for i, dir := range want {
		if meta.Directories[i] != dir {
			t.Fatalf("directories = %#v, want %#v", meta.Directories, want)
		}
	}
}

func TestContentMetaWithoutContentDir(t *testing.T) {
	meta, err := (&Site{Dir: t.TempDir()}).ContentMeta()
	if err != nil {
		t.Fatal(err)
	}
	if meta.Exists {
		t.Fatal("expected content to be missing")
	}
	if len(meta.Directories) != 0 {
		t.Fatalf("directories = %#v", meta.Directories)
	}
}
