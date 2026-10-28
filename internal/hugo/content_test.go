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

func TestCreateContentDirectory(t *testing.T) {
	root := t.TempDir()
	site := &Site{Dir: root}
	dir, err := site.CreateContentDirectory("/posts/2026/")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "posts/2026" {
		t.Fatalf("dir = %q", dir)
	}
	info, err := os.Stat(filepath.Join(root, "content/posts/2026"))
	if err != nil || !info.IsDir() {
		t.Fatalf("directory was not created: %v", err)
	}
	again, err := site.CreateContentDirectory("posts/2026")
	if err != nil || again != dir {
		t.Fatalf("existing directory: %q %v", again, err)
	}
	files, err := os.ReadDir(filepath.Join(root, "content"))
	if err != nil || len(files) != 1 || files[0].Name() != "posts" {
		t.Fatalf("unexpected content root: %#v %v", files, err)
	}
}

func TestCreateContentDirectoryRejectsInvalidNames(t *testing.T) {
	site := &Site{Dir: t.TempDir()}
	for _, name := range []string{"", "/", "../escape", "posts/../../escape", "posts\\windows", ".hidden", "posts/.hidden", "node_modules", "posts/node_modules"} {
		if _, err := site.CreateContentDirectory(name); err == nil {
			t.Errorf("expected %q to be rejected", name)
		}
	}
	file := filepath.Join(site.ContentDir(), "posts")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := site.CreateContentDirectory("posts"); err == nil {
		t.Fatal("expected a file at the path to be rejected")
	}
}
